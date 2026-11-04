// Package artwork fetches and decodes album covers for the views: off the UI
// thread, a few at a time, the ones the window shows now before any it showed
// before, sized for where they show, and kept decoded until they take more
// memory than keepBytes, the least lately shown released first.
package artwork

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

// keepBytes bounds the decoded covers kept: past it, the covers shown
// longest ago are released, never one shown in the last shownLately, nor
// one the window's last frame drew (lastFrame), however long ago that
// was: an idle window draws no frames, and a cover it shows must not go
// when a fetch lands meanwhile. A screen left and opened again finds its
// covers decoded, which a fetch, even from the disk, would show only some
// frames late, as placeholders first. failedRelease is how long a cover
// that failed and is no longer asked for is remembered, and sweepEvery how
// often the cache looks. maxSide is the longest side a cover is decoded
// at: a URL without a size, as a private playlist's, is the picture as
// uploaded, up to 4000 pixels, where no view shows more than 1200.
const (
	keepBytes     = 384 << 20
	shownLately   = 5 * time.Second
	lastFrame     = 250 * time.Millisecond
	failedRelease = 2 * time.Minute
	sweepEvery    = time.Minute
	maxSide       = 1200
)

// retryFirst is how long a cover that failed to load waits before it is
// tried again, doubling with each failure up to retryMost: a view asks for
// it every frame, so without a retry one dropped request would leave its
// placeholder for as long as it shows.
const (
	retryFirst = 5 * time.Second
	retryMost  = 5 * time.Minute
)

// userAgent and referer make a cover request look like the web player's.
// Apple's image CDN answers 403 to Go's default User-Agent for a size it has
// not rendered before, so without them some covers never load.
const (
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	referer   = "https://music.apple.com/"
)

// slots is how many covers load at once. Past it, a cover waits in the
// queue: the one asked for last goes first, those one frame asked for
// within sameFrame of each other in the order asked, which is the screen's
// from the top; one not asked for in the window's last frame (lastFrame)
// leaves the queue, and one loading that the window no longer shows gives
// its slot to one waiting (pump). A frame slower than lastFrame takes the
// covers it asked for first as no longer shown: they lose a load or their
// place, and are asked for again the next frame.
const (
	slots     = 6
	sameFrame = 4 * time.Millisecond
)

// A download gives up once no byte came for stallAfter, response headers
// included, and once it took downloadMost in all. Not a deadline on the
// whole picture: a private playlist's presigned picture, served from
// Apple's blobstore at its uploaded size and no other, takes 25 to 35 s on
// a slow link, more with others loading beside it. What a download that
// gave up had received stays, up to partsMost in all, and its next try asks
// for the rest (download).
const (
	stallAfter   = 15 * time.Second
	downloadMost = 5 * time.Minute
	partsMost    = 64 << 20
	pictureMost  = 16 << 20
)

// errStalled ends a download that received nothing for stallAfter.
var errStalled = errors.New("artwork download stalled")

// fallbackPx are the smaller sizes a cover is asked for when its size is
// refused: an Apple-generated playlist cover serves 512 pixels to nobody.
var fallbackPx = []int{256, 128}

// Cache holds decoded covers. Use Get on the UI thread.
type Cache struct {
	post func(func())
	http *http.Client
	// disk, when set, keeps fetched covers across launches.
	disk    *diskCache
	entries map[coverKey]*entry
	// queue are the covers waiting for a slot, in the order asked for;
	// running counts the ones loading; pumpQueued says a pump waits for the
	// end of the frame (Get), pumpNext that the next frame's end pumps, and
	// waiting that a loading cover is to be no longer shown (pump).
	queue                         []coverKey
	running                       int
	pumpQueued, pumpNext, waiting bool
	// parts are the downloads that gave up, by the picture's key (keyOf),
	// partBytes what they hold; fetches running beside each other share
	// them under partMu.
	partMu    sync.Mutex
	parts     map[string]part
	partBytes int
	// bytes is what the decoded covers of entries take, and keep its bound:
	// keepBytes but in tests.
	bytes, keep int
	// lastGet is the last Get, in the window's last frame; sweepQueued
	// says a sweep waits for the end of the frame (Get).
	lastGet     time.Time
	sweepQueued bool
	swept       time.Time
	// now is time.Now but in tests.
	now func() time.Time
}

// coverKey names a cover by its URL template's key (keyOf) and size: Get,
// which runs for each cover every frame, fills the URL only to fetch it.
type coverKey struct {
	template string
	px       int
}

type entry struct {
	// src is the URL template last asked for under the entry's key, which
	// a fetch reads (keyOf).
	src string
	img ui.ImageSource
	// bytes is what img takes decoded.
	bytes int
	used  time.Time
	// loading is set while the cover waits in the queue or loads; running
	// while it loads, which cancel stops.
	loading, running bool
	cancel           context.CancelFunc
	// failures counts the loads that failed in a row, the last at failedAt.
	failures int
	failedAt time.Time
}

// New returns a cache that hands decoded covers to the UI thread with post,
// which redraws.
func New(post func(func())) *Cache {
	// Covers come from Apple's CDN on the internet, so they go through the
	// system's proxy settings as a browser's requests would.
	return NewWith(post, netproxy.Transport())
}

// NewWith is New with covers fetched through rt, such as the demo's
// backend.
func NewWith(post func(func()), rt http.RoundTripper) *Cache {
	// No client timeout: download bounds each request (stallAfter).
	return &Cache{
		post:    post,
		http:    &http.Client{Transport: rt},
		entries: map[coverKey]*entry{},
		keep:    keepBytes,
		now:     time.Now,
	}
}

// URL fills an artwork URL template, whose {w}x{h} and {f} placeholders the
// catalog leaves for the client, for a square of px pixels. A URL without
// placeholders, as a private playlist's, is returned as it is.
func URL(template string, px int) string {
	n := strconv.Itoa(px)
	return strings.NewReplacer("{w}", n, "{h}", n, "{f}", "jpg").Replace(template)
}

// Get returns the cover at template sized to px pixels, or nil while it
// loads or when it cannot be had; the view redraws once it arrives. Sizes
// round up to a few steps so one cover is not fetched at every size a
// layout passes through.
func (c *Cache) Get(template string, px int) ui.ImageSource {
	if template == "" {
		return nil
	}
	key := coverKey{keyOf(template), 0}
	// A URL without a size, as a private playlist's, is the one picture
	// whatever size it shows at.
	if strings.Contains(template, "{w}") {
		key.px = bucket(px)
	}
	now := c.now()
	e := c.entries[key]
	if e == nil {
		e = &entry{}
		c.entries[key] = e
	}
	e.used, e.src, c.lastGet = now, template, now
	if e.img == nil && !e.loading && (e.failures == 0 || now.Sub(e.failedAt) >= retryWait(e.failures)) {
		e.loading = true
		if c.running < slots {
			c.start(key, e)
		} else {
			c.queue = append(c.queue, key)
			c.pumpNext = true
		}
	}
	if c.pumpNext && !c.pumpQueued {
		// After the frame, once every cover it shows has been asked for.
		// The covers it queues are still loading in the next, which queues
		// none of them again: one pump, not a loop.
		c.pumpNext, c.pumpQueued = false, true
		c.post(c.pump)
	}
	if c.swept.IsZero() {
		// Nothing to sweep yet: the first sweep falls due a while on.
		c.swept = now
	}
	if now.Sub(c.swept) > sweepEvery && !c.sweepQueued {
		// After the frame, once every cover it shows has been asked for.
		c.sweepQueued = true
		c.post(func() {
			c.sweepQueued = false
			c.sweep(c.now())
		})
	}
	return e.img
}

// retryWait is how long a cover waits after its nth failure in a row.
func retryWait(n int) time.Duration {
	return min(retryFirst<<min(n-1, 10), retryMost)
}

func bucket(px int) int {
	for _, b := range []int{64, 128, 256, 512, 768, 1200} {
		if px <= b {
			return b
		}
	}
	return 1200
}

// shown reports whether the window's last frame asked for e.
func (c *Cache) shown(e *entry) bool { return !e.used.Before(c.lastGet.Add(-lastFrame)) }

// start loads the cover of e in a slot.
func (c *Cache) start(key coverKey, e *entry) {
	ctx, cancel := context.WithCancel(context.Background())
	c.running++
	e.running, e.cancel = true, cancel
	go c.fetch(ctx, cancel, key, e.src)
}

// pump runs after a frame that queued covers: the ones that frame no longer
// asked for leave the queue, and each loading cover it no longer shows,
// the one shown longest ago first, gives its slot to one it does, as long
// as one waits. A loading cover the frame did not ask for, which a stalled
// request may hold for its whole timeout, is no longer shown lastFrame
// after it was last asked for: pump looks again then, and the frame its
// post draws asks for what the window shows; a window that draws no frame,
// as a hidden one, is not looked at again.
func (c *Cache) pump() {
	c.pumpQueued = false
	c.dropUnshown()
	var stale []*entry
	for _, e := range c.entries {
		if e.running && e.cancel != nil && !c.shown(e) {
			stale = append(stale, e)
		}
	}
	slices.SortFunc(stale, func(a, b *entry) int { return a.used.Compare(b.used) })
	for _, e := range stale[:min(len(stale), len(c.queue))] {
		e.cancel()
		// Its slot frees as its fetch returns (fetch).
		e.cancel = nil
	}
	if len(c.queue) == 0 || c.waiting {
		return
	}
	var last, soonest time.Time
	for _, k := range c.queue {
		if u := c.entries[k].used; u.After(last) {
			last = u
		}
	}
	for _, e := range c.entries {
		if e.running && e.cancel != nil && e.used.Before(last.Add(-sameFrame)) && (soonest.IsZero() || e.used.Before(soonest)) {
			soonest = e.used
		}
	}
	if !soonest.IsZero() {
		c.waiting = true
		time.AfterFunc(max(soonest.Add(lastFrame).Sub(c.lastGet), 0)+time.Millisecond, func() {
			// The frame this post draws pumps at its end (Get).
			c.post(func() { c.waiting, c.pumpNext = false, true })
		})
	}
}

// dropUnshown takes out of the queue the covers the last frame did not ask
// for; a view showing one again asks for it again.
func (c *Cache) dropUnshown() {
	c.queue = slices.DeleteFunc(c.queue, func(k coverKey) bool {
		e := c.entries[k]
		if e == nil || !e.loading || e.running {
			return true
		}
		if !c.shown(e) {
			e.loading = false
			return true
		}
		return false
	})
}

// next starts the queued covers the last frame asked for while slots are
// free, the ones asked for last first.
func (c *Cache) next() {
	c.dropUnshown()
	for c.running < slots && len(c.queue) > 0 {
		var last time.Time
		for _, k := range c.queue {
			if u := c.entries[k].used; u.After(last) {
				last = u
			}
		}
		i := slices.IndexFunc(c.queue, func(k coverKey) bool { return !c.entries[k].used.Before(last.Add(-sameFrame)) })
		k := c.queue[i]
		c.queue = slices.Delete(c.queue, i, i+1)
		c.start(k, c.entries[k])
	}
}

func (c *Cache) fetch(ctx context.Context, cancel context.CancelFunc, key coverKey, template string) {
	url := URL(template, key.px)
	img, status, err := c.loadSized(ctx, template, key.px)
	cancel()
	c.post(func() {
		c.running--
		defer c.next()
		e := c.entries[key]
		if e == nil {
			return
		}
		e.loading, e.running, e.cancel = false, false, nil
		if errors.Is(err, context.Canceled) {
			// Given up for a cover shown now (pump): asked for again, it
			// loads again.
			return
		}
		if err != nil {
			e.failures++
			e.failedAt = c.now()
			if e.failures == 1 {
				slog.Warn("artwork fetch failed", "url", url, "status", status, "err", err)
			} else {
				slog.Debug("artwork fetch failed again", "url", url, "status", status, "failures", e.failures, "err", err)
			}
			// The view asks again only as it redraws: one is due then.
			time.AfterFunc(retryWait(e.failures), func() { c.post(func() {}) })
			return
		}
		e.img, e.failures, e.bytes = img, 0, decodedBytes(img)
		c.bytes += e.bytes
		if c.bytes > c.keep {
			c.sweep(c.now())
		}
	})
}

// loadSized loads the cover at its size, or, when the CDN refuses that size,
// at the next smaller one that it serves.
func (c *Cache) loadSized(ctx context.Context, template string, want int) (ui.ImageSource, int, error) {
	img, status, err := c.load(ctx, URL(template, want))
	if err == nil || !refused(status) || !strings.Contains(template, "{w}") {
		return img, status, err
	}
	for _, px := range fallbackPx {
		if px >= want {
			continue
		}
		if smaller, _, ferr := c.load(ctx, URL(template, px)); ferr == nil {
			// The disk keeps it under the refused size's URL too, so a later
			// load of that size finds it without asking the CDN again.
			if data, ok := c.disk.get(keyOf(URL(template, px))); ok {
				c.disk.put(keyOf(URL(template, want)), data)
			}
			return smaller, http.StatusOK, nil
		}
	}
	return img, status, err
}

// keyOf is what names the picture at url in the caches: url itself, but
// for a presigned link, as a private playlist's cover is, whose signature
// changes with every read of the playlist; its path is the picture.
func keyOf(url string) string {
	if i := strings.IndexByte(url, '?'); i >= 0 && strings.Contains(url[i:], "X-Amz-Signature=") {
		return url[:i]
	}
	return url
}

// refused reports a status that another size of the same cover may not get.
func refused(status int) bool { return status == http.StatusForbidden || status == http.StatusNotFound }

// load reads and decodes the cover at url, from the disk cache when it holds
// it, with the HTTP status it came with: 200 from the disk, 0 when none came.
func (c *Cache) load(ctx context.Context, url string) (ui.ImageSource, int, error) {
	if data, ok := c.disk.get(keyOf(url)); ok {
		if img, err := decode(data); err == nil {
			return img, http.StatusOK, nil
		}
	}
	data, status, err := c.download(ctx, url)
	if err != nil {
		return nil, status, err
	}
	if err := ctx.Err(); err != nil {
		return nil, status, err
	}
	img, err := decode(data)
	if err != nil {
		return nil, status, err
	}
	c.disk.put(keyOf(url), data)
	return img, status, nil
}

// decode decodes a cover, no larger than maxSide on its longer side.
func decode(data []byte) (ui.ImageSource, error) {
	img, err := ui.DecodeImage(data)
	b, ok := img.(*ui.Bitmap)
	if err != nil || !ok {
		return img, err
	}
	w, h := b.Size()
	if max(w, h) <= maxSide {
		return img, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// A format only the view decodes shows at its own size.
		return img, nil
	}
	f := float64(maxSide) / float64(max(w, h))
	dst := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(w)*f)), max(1, int(float64(h)*f))))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return ui.NewBitmap(dst), nil
}

// part is what a download that gave up had received of a picture, and the
// validator (ETag or Last-Modified) it came with, which the rest must match.
type part struct {
	data      []byte
	validator string
}

func (c *Cache) takePart(key string) part {
	c.partMu.Lock()
	defer c.partMu.Unlock()
	p := c.parts[key]
	delete(c.parts, key)
	c.partBytes -= len(p.data)
	return p
}

func (c *Cache) keepPart(key string, p part) {
	if len(p.data) == 0 || len(p.data) > partsMost {
		return
	}
	c.partMu.Lock()
	defer c.partMu.Unlock()
	if c.parts == nil || c.partBytes+len(p.data) > partsMost {
		// The ones kept are dropped, not trimmed: past the bound a slow
		// link is the least of the trouble.
		c.parts, c.partBytes = map[string]part{}, 0
	}
	c.parts[key] = p
	c.partBytes += len(p.data)
}

// download reads the picture at url, going on from what an earlier
// download of it received when the server takes a Range and the picture
// is the same (If-Range).
func (c *Cache) download(ctx context.Context, url string) ([]byte, int, error) {
	key := keyOf(url)
	have := c.takePart(key)
	ctx, cancel := context.WithTimeout(ctx, downloadMost)
	defer cancel()
	ctx, stall := context.WithCancelCause(ctx)
	idle := time.AfterFunc(stallAfter, func() { stall(errStalled) })
	defer idle.Stop()
	// The cause: errStalled, the deadline, or a fetch given up (pump).
	why := func(err error) error {
		if cause := context.Cause(ctx); cause != nil {
			return fmt.Errorf("artwork %s: %w", url, cause)
		}
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	if len(have.data) > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(have.data)))
		if have.validator != "" {
			req.Header.Set("If-Range", have.validator)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.keepPart(key, have)
		return nil, 0, why(err)
	}
	defer resp.Body.Close()
	validator := resp.Header.Get("ETag")
	if validator == "" {
		validator = resp.Header.Get("Last-Modified")
	}
	var data []byte
	switch {
	case resp.StatusCode == http.StatusPartialContent && len(have.data) > 0 &&
		strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", len(have.data))):
		data, validator = have.data, have.validator
	case resp.StatusCode == http.StatusOK:
		// The whole picture: the server took no Range, or it changed.
	default:
		return nil, resp.StatusCode, fmt.Errorf("artwork %s: %s", url, resp.Status)
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			idle.Reset(stallAfter)
			data = append(data, buf[:n]...)
			if len(data) > pictureMost {
				return nil, resp.StatusCode, errors.New("artwork larger than 16 MiB")
			}
		}
		if err == io.EOF {
			return data, http.StatusOK, nil
		}
		if err != nil {
			c.keepPart(key, part{data, validator})
			return nil, resp.StatusCode, why(err)
		}
	}
}

// sweep forgets covers that failed and are not asked for, then releases
// the covers shown longest ago while the kept ones take more than keep,
// none that the last frame drew. It runs between frames, never in Get,
// where the covers a frame draws after it would not have been asked for
// yet.
func (c *Cache) sweep(now time.Time) {
	c.swept = now
	var old []coverKey
	for k, e := range c.entries {
		switch {
		case e.loading:
		case e.img == nil && now.Sub(e.used) > failedRelease:
			delete(c.entries, k)
		case e.img != nil && now.Sub(e.used) > shownLately && e.used.Before(c.lastGet.Add(-lastFrame)):
			old = append(old, k)
		}
	}
	if c.bytes <= c.keep {
		return
	}
	slices.SortFunc(old, func(a, b coverKey) int { return c.entries[a].used.Compare(c.entries[b].used) })
	for _, k := range old {
		if c.bytes <= c.keep {
			break
		}
		c.bytes -= c.entries[k].bytes
		delete(c.entries, k)
	}
}

// decodedBytes is what img takes decoded: four bytes a pixel, and a third
// more for the smaller copies drawn as it shows small.
func decodedBytes(img ui.ImageSource) int {
	if b, ok := img.(*ui.Bitmap); ok {
		w, h := b.Size()
		return w * h * 4 * 4 / 3
	}
	return 0
}
