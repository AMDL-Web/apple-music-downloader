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

	"amdl/internal/capability"
	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/format"
	"amdl/internal/progress"
)

// track is one item in the summary view of a job.
type track struct {
	Index      int    `json:"index"`
	Title      string `json:"title,omitempty"`
	Status     string `json:"status"`
	Quality    string `json:"quality,omitempty"`
	FileSize   int64  `json:"file_size,omitempty"`
	OutputPath string `json:"output_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// summary is the default view of a job: the Job itself plus its album bar and
// one short entry per track, about a quarter of the full detail's size.
func summary(d client.DownloadDetail) map[string]any {
	raw, _ := json.Marshal(d.Job) // a domain.Job always marshals
	var out map[string]any
	_ = json.Unmarshal(raw, &out) // and decodes back into a map
	tracks := make([]track, 0, len(d.Items))
	for _, it := range d.Items {
		tracks = append(tracks, track{
			Index: it.Index, Title: it.Title, Status: string(it.Status),
			Quality:  format.Quality(it.Codec, it.BitDepth, it.SampleRate, it.Bitrate),
			FileSize: it.FileSize, OutputPath: it.OutputPath, Error: it.Error,
		})
	}
	for k := range out {
		// Palettes are for drawing covers; the full view keeps them.
		if strings.Contains(k, "_bg_color") || strings.Contains(k, "_text_color") {
			delete(out, k)
		}
	}
	out["album_progress"] = progress.AlbumProgress(d.Job.Status, d.Items)
	out["tracks"] = tracks
	return out
}

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
	overwrite := fs.Bool("overwrite", false, "overwrite files already on disk instead of skipping them (destructive)")
	wait := fs.Bool("wait", false, "wait until every job finishes and print them")
	watch := fs.Bool("watch", false, "stream the jobs' events as NDJSON until they finish")
	timeout := fs.Duration("timeout", 2*time.Hour, "how long --wait or --watch waits")
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
		if *overwrite {
			overrides["force_overwrite"] = true
		}
		body := map[string]any{"urls": urls}
		if len(overrides) > 0 {
			body["overrides"] = overrides
		}
		req := capability.Request{Method: http.MethodPost, Path: "/api/v1/downloads", Body: body}
		if e.dry {
			return e.send(req, nil)
		}
		if *overwrite {
			if err := e.confirm("--overwrite replaces files already on disk"); err != nil {
				return err
			}
		}
		raw, err := e.c.Call(e.ctx, req.Method, req.Path, nil, body)
		var batch domain.BatchSubmitResponse
		if err != nil {
			var api *client.APIError
			if !errors.As(err, &api) || api.Code != "no_urls_accepted" || json.Unmarshal(api.Details, &batch) != nil || !allDuplicates(batch) {
				return err
			}
			// Every link is already downloading: the request's goal holds.
			raw = api.Details
		} else if err := json.Unmarshal(raw, &batch); err != nil {
			return err
		}
		ids := jobIDs(batch)
		if !*wait && !*watch {
			return e.out.result(raw)
		}
		e.note("%d accepted, %d rejected", batch.Accepted, batch.Rejected)
		ctx, cancel := context.WithTimeout(e.ctx, *timeout)
		defer cancel()
		var finals []map[string]any
		failed := 0
		for _, id := range ids {
			d, err := e.follow(ctx, id, *watch, watchOpts{interval: 2 * time.Second})
			if err != nil {
				return e.waitErr(err, id, finals)
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
				failed++
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
		if failed > 0 {
			return &cliError{Code: "job_failed", exit: exitJobFailed,
				Message: fmt.Sprintf("%d of %d jobs did not complete", failed, len(ids)), Hint: "amdl jobs list --status failed,cancelled"}
		}
		return nil
	}
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
		return nil, usageErr("no link given; usage: amdl download <url>… (or - to read them from stdin)")
	}
	return args, nil
}

func allDuplicates(b domain.BatchSubmitResponse) bool {
	if len(b.Results) == 0 {
		return false
	}
	for _, r := range b.Results {
		if r.ExistingJobID == "" {
			return false
		}
	}
	return true
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
}

// follow follows one job until it is finished and returns its final detail. It
// prints the job's events as NDJSON when lines is set, and a progress line on a
// terminal's stderr otherwise.
func (e *env) follow(ctx context.Context, id string, lines bool, o watchOpts) (client.DownloadDetail, error) {
	var d client.DownloadDetail
	items := map[string]int{}
	lastProgress := map[string]time.Time{}
	var rate progress.Rate
	var lineErr error
	h := client.JobHandlers{
		Detail: func(dd client.DownloadDetail) {
			d = dd
			items = map[string]int{}
			for i, it := range d.Items {
				items[it.ID] = i
			}
		},
		Event: func(ev domain.Event) {
			applyEvent(&d, items, ev)
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
	if err == nil && !client.Terminal(d.Job.Status) {
		d, err = e.c.Download(e.ctx, id)
	}
	return d, err
}

// applyEvent merges an event's payload into the detail: lifecycle events carry
// the Job's fields, item events the JobItem's.
func applyEvent(d *client.DownloadDetail, items map[string]int, ev domain.Event) {
	if len(ev.Payload) == 0 {
		return
	}
	if ev.ItemID == "" {
		if strings.HasPrefix(string(ev.Type), "job_") {
			_ = json.Unmarshal(ev.Payload, &d.Job) // a malformed payload leaves the last snapshot
		}
		return
	}
	i, ok := items[ev.ItemID]
	if !ok {
		var it domain.JobItem
		if json.Unmarshal(ev.Payload, &it) != nil || it.ID == "" {
			return
		}
		items[it.ID] = len(d.Items)
		d.Items = append(d.Items, it)
		return
	}
	_ = json.Unmarshal(ev.Payload, &d.Items[i]) // as above
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
	line := fmt.Sprintf("%s  %d/%d  %d%%", title, a.Lit, a.Total, a.Percent)
	if left != "" {
		line += "  " + left
	}
	fmt.Fprintf(e.Stderr, "\r\x1b[K%s", line)
}

// waitErr turns the end of a wait that did not see the job finish into the
// right error, printing what is known so far first.
func (e *env) waitErr(err error, id string, done []map[string]any) error {
	switch {
	case errors.Is(err, client.ErrJobGone):
		return &cliError{Code: "job_not_found", exit: exitNotFound, Message: "job " + id + " no longer exists"}
	case errors.Is(err, context.DeadlineExceeded):
		if d, derr := e.c.Download(e.ctx, id); derr == nil {
			_ = e.out.result(append(done, summary(d))) // the error below is the outcome either way
		}
		return &cliError{Code: "wait_timeout", exit: exitWaitTimeout,
			Message: "job " + id + " is still running", Hint: "amdl jobs wait " + id}
	}
	return err
}

func jobsWaitCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	timeout := fs.Duration("timeout", 2*time.Hour, "how long to wait")
	return func(e *env, args []string) error {
		if len(args) != 1 {
			return usageErr("usage: amdl jobs wait <job_id>")
		}
		ctx, cancel := context.WithTimeout(e.ctx, *timeout)
		defer cancel()
		d, err := e.follow(ctx, args[0], false, watchOpts{})
		if err != nil {
			return e.waitErr(err, args[0], nil)
		}
		if err := e.out.result(summary(d)); err != nil {
			return err
		}
		if d.Job.Status != domain.JobCompleted {
			return &cliError{Code: "job_failed", exit: exitJobFailed,
				Message: fmt.Sprintf("job %s ended %s", d.Job.ID, d.Job.Status), Hint: "amdl jobs retry " + d.Job.ID}
		}
		return nil
	}
}

func jobsWatchCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	types := fs.String("types", "", "comma-separated event types to print, such as item_completed,item_failed (default all)")
	interval := fs.Duration("progress-interval", 2*time.Second, "least time between two item_progress lines of one track; 0 prints all")
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
			return e.watchFeed(o)
		case 1:
		default:
			return usageErr("usage: amdl jobs watch [<job_id>]")
		}
		d, err := e.follow(e.ctx, args[0], true, o)
		if err != nil {
			return e.waitErr(err, args[0], nil)
		}
		return e.out.line(map[string]any{"type": "job_snapshot", "job": summary(d)})
	}
}

// watchFeed prints the overview feed's messages until interrupted.
func (e *env) watchFeed(o watchOpts) error {
	var lineErr error
	ctx, cancel := context.WithCancel(e.ctx)
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

func libraryListCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	kind := fs.String("kind", "", "album, playlist, song, station or artist (default all)")
	q := fs.String("q", "", "text to find in the title, artist or link")
	limit := fs.Int("limit", 50, "how many per page, at most 200")
	offset := fs.Int("offset", 0, "where the page starts")
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl library list [--kind album] [--q text]")
		}
		values := map[string]any{"status": []string{"completed"}, "limit": *limit, "q": *q, "type": *kind, "sort": "updated_at"}
		if *offset > 0 {
			values["offset"] = *offset
		}
		req, err := capability.Lookup("listDownloads").Build(values)
		if err != nil {
			return usageErr("%v", err)
		}
		return e.send(req, nil)
	}
}

func logsTailCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	op := capability.Lookup("streamLogs")
	values := specFlags(fs, op, nil)
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
		var lineErr error
		_, err = e.c.Stream(e.ctx, path, "", func() { e.note("live") }, func(ev client.Event) {
			if lineErr == nil {
				lineErr = e.out.line(json.RawMessage(ev.Data))
			}
		})
		if lineErr != nil {
			return lineErr
		}
		return err
	}
}
