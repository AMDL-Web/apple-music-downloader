package wrapperhost

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// A ranged download keeps its bytes in a directory of part files beside the
// target, named "<start>-<end>" for the bytes [start, end). A part is written
// as "<start>-<end>.tmp" and renamed once it holds all its bytes, so a name
// without the suffix is whole, whatever stopped the download. A .tmp file is
// appended to in order, so the next attempt continues after what it holds.
// Only a whole download is checked against the pin's sha256, which is also
// what catches a part a power loss left torn.
//
// These are variables so that tests can shrink them.
var (
	// chunkSize is the most one request asks for: small enough that a cancel
	// or a failure costs little and that the last requests do not leave
	// connections idle, large enough that a request's setup is noise.
	chunkSize int64 = 1 << 20
	// chunkAttempts is how often one chunk is tried before the download fails.
	chunkAttempts = 5
	// chunkBackoff is the wait after a first failure; it doubles up to
	// maxBackoff.
	chunkBackoff = time.Second
	maxBackoff   = 15 * time.Second
	// chunkTimeout bounds one request.
	chunkTimeout = 5 * time.Minute
)

// span is the bytes [start, end).
type span struct{ start, end int64 }

func (s span) len() int64     { return s.end - s.start }
func (s span) name() string   { return fmt.Sprintf("%d-%d", s.start, s.end) }
func (s span) String() string { return s.name() }

func parseSpan(name string, size int64) (span, bool) {
	var s span
	if _, err := fmt.Sscanf(name, "%d-%d", &s.start, &s.end); err != nil || s.name() != name {
		return span{}, false
	}
	return s, s.start >= 0 && s.start < s.end && s.end <= size
}

// retry runs fn up to chunkAttempts times, waiting between attempts, and
// stops at once for a permanent error or a canceled ctx.
func retry(ctx context.Context, fn func() error) error {
	var err error
	wait := chunkBackoff
	for a := 0; a < chunkAttempts; a++ {
		if err = fn(); err == nil {
			return nil
		}
		var perm permanent
		if ctx.Err() != nil || errors.As(err, &perm) || a == chunkAttempts-1 {
			return err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
		wait = min(wait*2, maxBackoff)
	}
	return err
}

// progress hands the callers' report one byte count however many workers
// write: calls are serialized and Done never goes back.
type progress struct {
	mu          sync.Mutex
	done, total int64
	report      func(Progress)
}

func (p *progress) add(n int64) {
	p.mu.Lock()
	p.done += n
	p.report(Progress{Downloading, p.done, p.total})
	p.mu.Unlock()
}

type partWriter struct {
	w io.Writer
	p *progress
}

func (c partWriter) Write(b []byte) (int, error) {
	n, err := c.w.Write(b)
	c.p.add(int64(n))
	return n, err
}

// parts is the state of one ranged download.
type parts struct {
	dir  string
	size int64
}

// scan lists the whole parts, sorted, dropping files that are neither whole
// nor a .tmp a later attempt could continue.
func (d parts) scan() ([]span, error) {
	entries, err := os.ReadDir(d.dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var whole []span
	for _, e := range entries {
		s, ok := parseSpan(e.Name(), d.size)
		info, ierr := e.Info()
		if !ok || ierr != nil || info.Size() != s.len() {
			if !strings.HasSuffix(e.Name(), ".tmp") {
				_ = os.Remove(filepath.Join(d.dir, e.Name())) // not a part of ours, or cut short
			}
			continue
		}
		whole = append(whole, s)
	}
	sort.Slice(whole, func(i, j int) bool {
		if whole[i].start != whole[j].start {
			return whole[i].start < whole[j].start
		}
		return whole[i].end > whole[j].end
	})
	return whole, nil
}

// missing is what the whole parts leave of [0, size), cut into chunks.
func (d parts) missing(whole []span) []span {
	var out []span
	pos := int64(0)
	gap := func(to int64) {
		for pos < to {
			e := min(pos+chunkSize, to)
			out = append(out, span{pos, e})
			pos = e
		}
	}
	for _, s := range whole {
		if s.start > pos {
			gap(s.start)
		}
		pos = max(pos, s.end)
	}
	gap(d.size)
	return out
}

// prepare creates the directory, keeps the tmp files of the chunks still
// missing and returns how many bytes the parts already hold.
func (d parts) prepare(plan []span, whole []span) (int64, error) {
	var have int64
	pos := int64(0)
	for _, s := range whole {
		have += max(0, s.end-max(pos, s.start))
		pos = max(pos, s.end)
	}
	want := map[string]span{}
	for _, c := range plan {
		want[c.name()+".tmp"] = c
	}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		path := filepath.Join(d.dir, e.Name())
		c, ok := want[e.Name()]
		info, ierr := e.Info()
		switch {
		case !ok || ierr != nil || info.Size() > c.len():
			_ = os.Remove(path) // a chunk no longer planned, or too long to be ours
		default:
			have += info.Size()
		}
	}
	return have, nil
}

// fetch runs the plan on src's connections. It returns when every worker has.
func (d parts) fetch(ctx context.Context, src RangeSource, plan []span, prog *progress) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	work := make(chan span)
	for range min(src.Conns(), len(plan)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range work {
				if err := retry(ctx, func() error { return d.chunk(ctx, src, c, prog) }); err != nil {
					mu.Lock()
					if first == nil {
						first = err
					}
					mu.Unlock()
					cancel()
					return
				}
			}
		}()
	}
	fed := true
feed:
	for _, c := range plan {
		select {
		case work <- c:
		case <-ctx.Done():
			fed = false
			break feed
		}
	}
	close(work)
	wg.Wait()
	switch {
	case first != nil:
		return first
	case !fed:
		return ctx.Err()
	}
	return nil
}

// chunk fetches the rest of one part and makes it whole.
func (d parts) chunk(ctx context.Context, src RangeSource, c span, prog *progress) error {
	tmp := filepath.Join(d.dir, c.name()+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if have := st.Size(); have < c.len() {
		rctx, cancel := context.WithTimeout(ctx, chunkTimeout)
		err = src.Range(rctx, c.start+have, c.end, partWriter{f, prog})
		cancel()
		if err != nil {
			return err
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(d.dir, c.name()))
}

// assemble joins the whole parts into path, as far as they are contiguous
// from byte 0, and returns how many bytes that is.
func (d parts) assemble(whole []span, path string) (int64, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	pos := int64(0)
	for _, s := range whole {
		if s.end <= pos {
			continue
		}
		if s.start > pos {
			break
		}
		if err := d.copyPart(out, s, pos); err != nil {
			out.Close()
			return pos, err
		}
		pos = s.end
	}
	return pos, out.Close()
}

func (d parts) copyPart(out io.Writer, s span, from int64) error {
	f, err := os.Open(filepath.Join(d.dir, s.name()))
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Seek(from-s.start, io.SeekStart); err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	return err
}

// downloadRanges fetches p into path over several connections. It reports
// false, with no error, when the single stream should do it: the fetcher
// cannot serve ranges, the file is small, or the server ignores Range.
func downloadRanges(ctx context.Context, f Fetcher, p Pin, path string, report func(Progress)) (bool, error) {
	rf, ok := f.(RangeFetcher)
	if !ok || p.Size < 2*chunkSize {
		return false, nil
	}
	if st, err := os.Stat(path); err == nil && st.Size() == p.Size {
		return false, nil // already whole; Download only checks it
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	d := parts{dir: path + ".parts", size: p.Size}
	if err := os.MkdirAll(d.dir, 0o700); err != nil {
		return false, err
	}
	// A partial file the single stream left is a valid prefix: adopt it.
	if st, err := os.Stat(path); err == nil && st.Size() > 0 && st.Size() < p.Size {
		if err := os.Rename(path, filepath.Join(d.dir, span{0, st.Size()}.name())); err != nil {
			return false, err
		}
	}
	whole, err := d.scan()
	if err != nil {
		return false, err
	}
	plan := d.missing(whole)
	have, err := d.prepare(plan, whole)
	if err != nil {
		return false, err
	}
	prog := &progress{done: have, total: p.Size, report: report}
	report(Progress{Downloading, have, p.Size})
	if len(plan) > 0 {
		src, err := rf.Ranges(ctx, p)
		if err != nil {
			return false, err
		}
		defer src.Close()
		if err := d.fetch(ctx, src, plan, prog); err != nil {
			if errors.Is(err, ErrNoRange) {
				// Keep what a stream can continue from: the parts from byte 0.
				_, _ = d.assemble(whole, path) // best effort; the stream starts over without it
				return false, nil
			}
			return false, err
		}
		if whole, err = d.scan(); err != nil {
			return false, err
		}
	}
	n, err := d.assemble(whole, path)
	if err == nil && n != p.Size {
		err = fmt.Errorf("the parts hold %d of %d bytes", n, p.Size)
	}
	return err == nil, err
}
