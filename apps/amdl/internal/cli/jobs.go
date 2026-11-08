package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"strings"
	"time"

	"amdl/apps/amdl/internal/jobview"
	"amdl/internal/capability"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
	"amdl/internal/progress"
)

var summary = jobview.Summary

func jobsGetCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	view := fs.String("view", "summary", "summary (the job, its album progress and short tracks) or full (the API's DownloadDetail)")
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl jobs get <job_id>")
		}
		if *view != "summary" && *view != "full" {
			return usageErr("--view must be summary or full")
		}
		d, err := e.c.Download(e.ctx, args[0])
		if err != nil {
			return err
		}
		if *view == "full" {
			return e.out.result(d)
		}
		return e.out.result(summary(d))
	}
}

// jobsDelete deletes a job as the desktop app and the console do: with
// record=true, a record's run deletes its record.
func jobsDelete(e *env, args []string) error {
	if len(args) != 1 {
		return usageErr("usage: amdl jobs delete <job_id>")
	}
	req, err := capability.Lookup("deleteDownload").Build(map[string]any{"job_id": args[0], "record": true})
	if err != nil {
		return err
	}
	return e.send(req, nil)
}

func downloadCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	codecs := fs.String("codec", "", "codec order for this download, such as alac,aac (download.quality_priority)")
	var fallback *bool
	fs.BoolFunc("codec-fallback", "fall back to the next codec when one is unavailable (download.codec_alternative)", func(s string) error {
		b := s == "true"
		fallback = &b
		return nil
	})
	sampleRate := fs.Int("max-sample-rate", 0, "highest ALAC sample rate in Hz (download.alac_max_sample_rate)")
	bitDepth := fs.Int("max-bit-depth", 0, "highest ALAC bit depth (download.alac_max_bit_depth)")
	wait := fs.Bool("wait", false, "wait until every job finishes and print them")
	watch := fs.Bool("watch", false, "stream the jobs' events as NDJSON until they finish")
	timeout := fs.Duration("timeout", 2*time.Hour, "how long --wait or --watch waits for all the jobs together, as a duration such as 90s, 20m or 2h (default 2h); 0 only looks once")
	interval := fs.Duration("progress-interval", 2*time.Second, "with --watch, the least time between two item_progress lines of one track; 0 prints all")
	return func(e *env, args []string) error {
		urls, err := readURLs(e, args)
		if err != nil {
			return err
		}
		overrides := map[string]any{}
		if *codecs != "" {
			overrides["quality_priority"] = splitList(*codecs)
		}
		if fallback != nil {
			overrides["codec_alternative"] = *fallback
		}
		if *sampleRate > 0 {
			overrides["alac_max_sample_rate"] = *sampleRate
		}
		if *bitDepth > 0 {
			overrides["alac_max_bit_depth"] = *bitDepth
		}
		body := map[string]any{"urls": urls}
		if len(overrides) > 0 {
			body["overrides"] = overrides
		}
		req := capability.Request{Method: http.MethodPost, Path: "/api/v1/downloads", Body: body}
		if e.dry {
			return e.send(req, nil)
		}
		raw, err := e.c.Call(e.ctx, req.Method, req.Path, nil, body)
		var batch domain.BatchSubmitResponse
		if err != nil {
			var api *client.APIError
			if !errors.As(err, &api) || api.Code != "no_urls_accepted" || json.Unmarshal(api.Details, &batch) != nil || !batch.AllDuplicates() {
				return err
			}
			// Every link is already downloading: the request's goal holds.
			raw = api.Details
		} else if err := json.Unmarshal(raw, &batch); err != nil {
			return err
		}
		if hint := alreadyHaveHint(batch, *codecs); hint != "" {
			e.note("%s", hint)
			if raw, err = withHint(raw, hint); err != nil {
				return err
			}
		}
		ids := jobIDs(batch)
		if e.running != nil && !*wait && !*watch {
			// The backend lives only as long as this command.
			e.note("waiting: the embedded backend stops when amdl exits")
			*wait = true
		}
		if !*wait && !*watch {
			return e.out.result(raw)
		}
		e.note("%d accepted, %d rejected", batch.Accepted, batch.Rejected)
		if pending := pendingURLs(batch); len(pending) > 0 {
			e.note("not waiting for %d link(s) read into records first, with no job yet (amdl library pending list): %s",
				len(pending), strings.Join(pending, " "))
			if e.running != nil {
				e.note("the embedded backend reads them on its next start")
			}
		}
		if len(ids) == 0 {
			// No link has a job to follow: the answer says what each got.
			return e.out.result(raw)
		}
		ctx, cancel := context.WithTimeout(e.ctx, *timeout)
		defer cancel()
		var finals []map[string]any
		var failed []client.DownloadDetail
		for _, id := range ids {
			d, err := e.follow(ctx, id, *watch, watchOpts{interval: *interval, stopAtTerminal: !*watch})
			if err != nil {
				return e.waitErr(err, id, d, finals)
			}
			s := summary(d)
			if *watch {
				if err := e.out.line(map[string]any{"type": "job_snapshot", "job": s}); err != nil {
					return err
				}
			} else {
				finals = append(finals, s)
			}
			if d.Job.Status != domain.JobCompleted {
				failed = append(failed, d)
			}
		}
		if !*watch {
			if len(finals) == 1 {
				if err := e.out.result(finals[0]); err != nil {
					return err
				}
			} else if err := e.out.result(finals); err != nil {
				return err
			}
		}
		if len(failed) > 0 {
			hint := "amdl jobs list --status failed,cancelled"
			if len(failed) == 1 {
				_, hint = e.endedHint(failed[0])
			}
			return &cliError{Code: "job_failed", exit: exitJobFailed,
				Message: fmt.Sprintf("%d of %d jobs did not complete", len(failed), len(ids)), Hint: hint}
		}
		return nil
	}
}

// recordArgs are the kind and id arguments of the library commands for the
// record a link or job belongs to, such as "albums 1463409338".
func recordArgs(kind domain.ResourceKind, id string) string {
	return string(kind) + "s " + id
}

// alreadyHaveHint says how to get another quality when --codec was given and
// a link answered already_downloaded: download ignores the codec then, and
// library download adds it as a second file. "" when nothing applies.
func alreadyHaveHint(b domain.BatchSubmitResponse, codecs string) string {
	order := splitList(codecs)
	if len(order) == 0 {
		return ""
	}
	var cmds []string
	for _, r := range b.Results {
		if r.Status == domain.SubmitAlreadyDownloaded && r.Resource != nil {
			cmds = append(cmds, fmt.Sprintf("amdl library download %s --codec %s", recordArgs(r.Resource.Kind, r.Resource.ID), order[0]))
		}
	}
	if len(cmds) == 0 {
		return ""
	}
	return "already downloaded, so --codec changed nothing; to add that quality as another file: " + strings.Join(cmds, "; ")
}

// withHint adds a top-level hint to a JSON object.
func withHint(raw json.RawMessage, hint string) (json.RawMessage, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	obj["hint"] = hint
	return json.Marshal(obj)
}

// endedHint says what to do about a job that ended without completing: why
// in a few words (or "") and the command to run next. A run its record's
// pause cancelled is continued by resuming the record, a retry would not
// help a link the catalog does not have, and any other job is retried.
func (e *env) endedHint(d client.DownloadDetail) (why, hint string) {
	j := d.Job
	retry := "amdl jobs retry " + j.ID
	switch {
	case j.Status == domain.JobCancelled && j.Error == "paused" && j.ResourceID != "":
		return " (its record is paused)", "amdl library resume " + recordArgs(j.ResourceKind, j.ResourceID)
	case notInStorefront(j.Error) && j.ResourceID != "":
		return " (the catalog does not have it)", "the link is not in this account's storefront, so a retry fails again: check the link's storefront with amdl lookup " +
			j.Input + ", or drop the record with amdl library delete " + recordArgs(j.ResourceKind, j.ResourceID)
	}
	return "", retry
}

// notInStorefront reports whether a job's error says the catalog has no such
// resource: the run's own error text ("Apple Music has no album 1 in
// storefront cn (404)"), or the reason a claimed run of a record already
// marked that way gives.
func notInStorefront(msg string) bool {
	return msg == string(domain.CatalogNotInStorefront) || msg == string(domain.CatalogGone) ||
		strings.Contains(msg, "has no ") && strings.Contains(msg, "in storefront")
}

// readURLs takes the links from the arguments, or one or more per line from
// stdin when the only argument is "-".
func readURLs(e *env, args []string) ([]string, error) {
	if len(args) == 1 && args[0] == "-" {
		var urls []string
		sc := bufio.NewScanner(e.Stdin)
		for sc.Scan() {
			urls = append(urls, strings.Fields(sc.Text())...)
		}
		if err := sc.Err(); err != nil {
			return nil, err
		}
		args = urls
	}
	if len(args) == 0 {
		return nil, usageErr("no link given; usage: amdl download <url>... (or - to read them from stdin)")
	}
	return args, nil
}

// pendingURLs are the links a submission answered pending: read into records
// first, they have no job to follow yet.
func pendingURLs(b domain.BatchSubmitResponse) []string {
	var urls []string
	for _, r := range b.Results {
		if r.Status == domain.SubmitPending {
			urls = append(urls, r.URL)
		}
	}
	return urls
}

// jobIDs are the jobs a submission created or found already running, in
// submission order.
func jobIDs(b domain.BatchSubmitResponse) []string {
	var ids []string
	for _, r := range b.Results {
		switch {
		case r.Job != nil:
			ids = append(ids, r.Job.ID)
		case r.ExistingJobID != "":
			ids = append(ids, r.ExistingJobID)
		}
	}
	return ids
}

type watchOpts struct {
	types    map[string]bool
	interval time.Duration
	// stopAtTerminal returns once the job is finished instead of waiting for
	// its post-download hooks.
	stopAtTerminal bool
}

// follow follows one job until it is finished and returns its final detail. It
// prints the job's events as NDJSON when lines is set, and a progress line on a
// terminal's stderr otherwise. A job that is finished when ctx ends is not a
// timeout, and a ctx that is over from the start makes follow a single look at
// the job. On a timeout it returns the job's fresh detail with the error.
func (e *env) follow(ctx context.Context, id string, lines bool, o watchOpts) (client.DownloadDetail, error) {
	var d client.DownloadDetail
	items := map[string]int{}
	lastProgress := map[string]time.Time{}
	var rate progress.Rate
	var lineErr error
	h := client.JobHandlers{
		StopAtTerminal: o.stopAtTerminal,
		Detail: func(dd client.DownloadDetail) {
			d = dd
			items = jobview.Index(d)
		},
		Event: func(ev domain.Event) {
			jobview.Apply(&d, items, ev)
			if lines {
				if len(o.types) > 0 && !o.types[string(ev.Type)] {
					return
				}
				if ev.Type == domain.EventItemProgress && o.interval > 0 {
					if time.Since(lastProgress[ev.ItemID]) < o.interval {
						return
					}
					lastProgress[ev.ItemID] = time.Now()
				}
				if err := e.out.line(ev); err != nil && lineErr == nil {
					lineErr = err
				}
				return
			}
			e.progressLine(d, &rate)
		},
	}
	err := e.c.FollowJob(ctx, id, h)
	if e.ErrTTY && !lines && !e.quiet {
		fmt.Fprint(e.Stderr, "\r\x1b[K")
	}
	if lineErr != nil {
		return d, lineErr
	}
	if errors.Is(err, context.DeadlineExceeded) && e.ctx.Err() == nil {
		if fresh, ferr := e.c.Download(e.ctx, id); ferr == nil {
			d = fresh
			if client.Terminal(d.Job.Status) {
				err = nil
				if d.HooksRunning() && !o.stopAtTerminal {
					e.note("post-download hooks are still running")
				}
			}
		}
	}
	if err == nil && !client.Terminal(d.Job.Status) {
		d, err = e.c.Download(e.ctx, id)
	}
	return d, err
}

// progressLine redraws one line on a terminal's stderr: title, tracks done,
// percent and time left.
func (e *env) progressLine(d client.DownloadDetail, rate *progress.Rate) {
	if !e.ErrTTY || e.quiet {
		return
	}
	a := progress.AlbumProgress(d.Job.Status, d.Items)
	rate.Add(time.Now(), float64(a.Percent)/100)
	title := d.Job.Title
	if title == "" {
		title = d.Job.ID
	}
	left := format.Remaining(rate.Remaining())
	line := fmt.Sprintf("%s  %d/%d  %d%%", title, a.OnDisk, a.Total, a.Percent)
	if left != "" {
		line += "  " + left
	}
	fmt.Fprintf(e.Stderr, "\r\x1b[K%s", line)
}

// waitErr turns the end of a wait that did not see the job finish into the
// right error, printing what is known so far first: d is the job's latest
// detail and done holds the jobs of the same command that finished before it.
func (e *env) waitErr(err error, id string, d client.DownloadDetail, done []map[string]any) error {
	switch {
	case errors.Is(err, client.ErrJobGone):
		return &cliError{Code: "job_not_found", exit: exitNotFound, Message: "job " + id + " no longer exists"}
	case errors.Is(err, context.DeadlineExceeded):
		if d.Job.ID != "" {
			// One job is an object, as when it finishes; several are an array.
			var out any = summary(d)
			if len(done) > 0 {
				out = append(done, summary(d))
			}
			_ = e.out.result(out) // the error below is the outcome either way
		}
		return &cliError{Code: "wait_timeout", exit: exitWaitTimeout,
			Message: "job " + id + " is still running", Hint: "amdl jobs wait " + id}
	}
	return err
}

func jobsWaitCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	timeout := fs.Duration("timeout", 2*time.Hour, "how long to wait, as a duration such as 90s, 20m or 2h (default 2h); 0 only looks once")
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl jobs wait <job_id>")
		}
		ctx, cancel := context.WithTimeout(e.ctx, *timeout)
		defer cancel()
		d, err := e.follow(ctx, args[0], false, watchOpts{stopAtTerminal: true})
		if err != nil {
			return e.waitErr(err, args[0], d, nil)
		}
		if err := e.out.result(summary(d)); err != nil {
			return err
		}
		if d.Job.Status != domain.JobCompleted {
			why, hint := e.endedHint(d)
			return &cliError{Code: "job_failed", exit: exitJobFailed,
				Message: fmt.Sprintf("job %s ended %s%s", d.Job.ID, d.Job.Status, why), Hint: hint}
		}
		return nil
	}
}

func jobsWatchCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	types := fs.String("types", "", "comma-separated event types to print, such as item_completed,item_failed (default all)")
	interval := fs.Duration("progress-interval", 2*time.Second, "least time between two item_progress lines of one track; 0 prints all")
	timeout := fs.Duration("timeout", 0, "stop watching after this long, as a duration such as 90s, 20m or 2h (default 0: no limit)")
	return func(e *env, args []string) error {
		o := watchOpts{interval: *interval}
		if *types != "" {
			o.types = map[string]bool{}
			for _, t := range splitList(*types) {
				o.types[t] = true
			}
		}
		switch len(args) {
		case 0:
			return e.watchFeed(o, *timeout)
		case 1:
		default:
			return usageErr("usage: amdl jobs watch [<job_id>]")
		}
		ctx := e.ctx
		if *timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(e.ctx, *timeout)
			defer cancel()
		}
		d, err := e.follow(ctx, args[0], true, o)
		if err != nil {
			return e.waitErr(err, args[0], d, nil)
		}
		return e.out.line(map[string]any{"type": "job_snapshot", "job": summary(d)})
	}
}

// watchFeed prints the overview feed's messages until interrupted, or for
// timeout when it is positive; running out of time is a normal end.
func (e *env) watchFeed(o watchOpts, timeout time.Duration) error {
	var lineErr error
	ctx, cancel := withOptionalTimeout(e.ctx, timeout)
	defer cancel()
	e.c.FollowFeed(ctx, client.FeedHandlers{
		Message: func(m domain.DownloadFeedMessage) {
			if len(o.types) > 0 && !o.types[m.Type] {
				return
			}
			if err := e.out.line(m); err != nil {
				lineErr = err
				cancel()
			}
		},
		State: func(s client.ConnState) {
			switch s.Phase {
			case client.Live:
				e.note("live")
			case client.Offline:
				e.note("offline (%v); retrying at %s", s.Err, format.Seconds(s.RetryAt))
			}
		},
	})
	if lineErr != nil {
		return lineErr
	}
	return e.ctx.Err()
}

// withOptionalTimeout bounds ctx by timeout when it is positive.
func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func logsTailCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("streamLogs")
	values := specFlags(fs, op, nil)
	timeout := fs.Duration("timeout", 0, "stop after this long, as a duration such as 90s, 20m or 2h (default 0: no limit)")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl logs tail [--job-id id] [--level error]")
		}
		req, err := op.Build(values)
		if err != nil {
			return usageErr("%v", err)
		}
		path := req.Path
		if len(req.Query) > 0 {
			path += "?" + req.Query.Encode()
		}
		ctx, cancel := withOptionalTimeout(e.ctx, *timeout)
		defer cancel()
		var lineErr error
		_, err = e.c.Stream(ctx, path, "", func() { e.note("live") }, func(ev client.Event) {
			if lineErr == nil {
				lineErr = e.out.line(json.RawMessage(ev.Data))
			}
		})
		if lineErr != nil {
			return lineErr
		}
		if e.ctx.Err() == nil && ctx.Err() != nil {
			return nil // the --timeout ran out
		}
		return err
	}
}
