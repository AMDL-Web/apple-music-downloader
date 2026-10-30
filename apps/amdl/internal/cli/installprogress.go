package cli

import (
	"fmt"
	"io"
	"time"

	"amdl/internal/wrapperhost"
)

// Progress cadence: a terminal redraws its line at most every ttyEvery; any
// other stderr (a log file) gets a line at each 10% and at least one every
// logEvery.
const (
	ttyEvery = 200 * time.Millisecond
	logEvery = 15 * time.Second
)

// progressf writes a process line to stderr unless --quiet: unlike note it
// also writes when stderr is a file, where a long step would otherwise leave
// the log empty.
func (e *env) progressf(format string, a ...any) {
	if !e.quiet {
		fmt.Fprintf(e.Stderr, format+"\n", a...)
	}
}

// installReport turns wrapperhost's install progress into stderr lines: one
// line rewritten in place on a terminal, a line per 10% or per logEvery
// elsewhere. stdout stays the final JSON.
type installReport struct {
	w     io.Writer
	tty   bool
	now   func() time.Time
	phase wrapperhost.Phase
	// open is true while a terminal line is unfinished.
	open    bool
	last    time.Time
	decile  int64
	started bool
}

func (e *env) installReport() *installReport {
	if e.quiet {
		return &installReport{w: io.Discard, now: time.Now}
	}
	return &installReport{w: e.Stderr, tty: e.ErrTTY, now: time.Now}
}

func mb(n int64) int64 { return n >> 20 }

// report is the wrapperhost.Progress callback.
func (r *installReport) report(p wrapperhost.Progress) {
	if p.Phase != r.phase {
		r.finish()
		r.phase = p.Phase
		switch p.Phase {
		case wrapperhost.Verifying:
			fmt.Fprintln(r.w, "checking the download's sha256")
			return
		case wrapperhost.Unpacking:
			fmt.Fprintln(r.w, "unpacking the guest")
			return
		}
	}
	if p.Phase != wrapperhost.Downloading || p.Total <= 0 {
		return
	}
	now := r.now()
	pct := p.Done * 100 / p.Total
	line := fmt.Sprintf("downloading: %d of %d MB (%d%%)", mb(p.Done), mb(p.Total), pct)
	if r.tty {
		if p.Done < p.Total && r.started && now.Sub(r.last) < ttyEvery {
			return
		}
		fmt.Fprintf(r.w, "\r%s", line)
		r.open, r.started, r.last = true, true, now
		return
	}
	if d := pct / 10; r.started && d <= r.decile && now.Sub(r.last) < logEvery {
		return
	}
	fmt.Fprintln(r.w, line)
	r.decile, r.started, r.last = pct/10, true, now
}

// finish ends an unfinished terminal line, so what is printed next, an error
// included, starts on its own line.
func (r *installReport) finish() {
	if r.open {
		fmt.Fprintln(r.w)
		r.open = false
	}
}
