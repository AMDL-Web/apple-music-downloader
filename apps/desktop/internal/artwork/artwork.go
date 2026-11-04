// Package artwork fetches and decodes album covers for the views: off the UI
// thread, a few at a time, sized for where they show, and kept decoded until
// they take more memory than keepBytes, the least lately shown released
// first.
package artwork

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"
)

// keepBytes bounds the decoded covers kept: past it, the covers shown
// longest ago are released, never one shown in the last shownLately. A
// screen left and opened again finds its covers decoded, which a fetch,
// even from the disk, would show only some frames late, as placeholders
// first. failedRelease is how long a cover that failed and is no longer
// asked for is remembered, and sweepEvery how often the cache looks.
const (
	keepBytes     = 384 << 20
	shownLately   = 5 * time.Second
	failedRelease = 2 * time.Minute
	sweepEvery    = time.Minute
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

// fallbackPx are the smaller sizes a cover is asked for when its size is
// refused: an Apple-generated playlist cover serves 512 pixels to nobody.
var fallbackPx = []int{256, 128}

// Cache holds decoded covers. Use Get on the UI thread.
type Cache struct {
	post func(func())
	http *http.Client
	// disk, when set, keeps fetched covers across launches.
	disk    *diskCache
	slots   chan struct{}
	entries map[coverKey]*entry
	// bytes is what the decoded covers of entries take, and keep its bound:
	// keepBytes but in tests.
	bytes, keep int
	swept       time.Time
	// now is time.Now but in tests.
	now func() time.Time
}

// coverKey names a cover by its URL template and size: Get, which runs for
// each cover every frame, fills the URL only to fetch it.
type coverKey struct {
	template string
	px       int
}

type entry struct {
	img ui.ImageSource
	// bytes is what img takes decoded.
	bytes   int
	used    time.Time
	loading bool
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
	return &Cache{
		post:    post,
		http:    &http.Client{Transport: rt, Timeout: 30 * time.Second},
		slots:   make(chan struct{}, 6),
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
	key := coverKey{template, 0}
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
	e.used = now
	if e.img == nil && !e.loading && (e.failures == 0 || now.Sub(e.failedAt) >= retryWait(e.failures)) {
		e.loading = true
		go c.fetch(key, e)
	}
	if now.Sub(c.swept) > sweepEvery {
		c.sweep(now)
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

func (c *Cache) fetch(key coverKey, e *entry) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	url := URL(key.template, key.px)
	img, status, err := c.loadSized(key)
	c.post(func() {
		e.loading = false
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
func (c *Cache) loadSized(key coverKey) (ui.ImageSource, int, error) {
	img, status, err := c.load(URL(key.template, key.px))
	if err == nil || !refused(status) || !strings.Contains(key.template, "{w}") {
		return img, status, err
	}
	for _, px := range fallbackPx {
		if px >= key.px {
			continue
		}
		if smaller, _, ferr := c.load(URL(key.template, px)); ferr == nil {
			// The disk keeps it under the refused size's URL too, so a later
			// load of that size finds it without asking the CDN again.
			if data, ok := c.disk.get(URL(key.template, px)); ok {
				c.disk.put(URL(key.template, key.px), data)
			}
			return smaller, http.StatusOK, nil
		}
	}
	return img, status, err
}

// refused reports a status that another size of the same cover may not get.
func refused(status int) bool { return status == http.StatusForbidden || status == http.StatusNotFound }

// load reads and decodes the cover at url, from the disk cache when it holds
// it, with the HTTP status it came with: 200 from the disk, 0 when none came.
func (c *Cache) load(url string) (ui.ImageSource, int, error) {
	if data, ok := c.disk.get(url); ok {
		if img, err := ui.DecodeImage(data); err == nil {
			return img, http.StatusOK, nil
		}
	}
	data, status, err := c.download(url)
	if err != nil {
		return nil, status, err
	}
	img, err := ui.DecodeImage(data)
	if err != nil {
		return nil, status, err
	}
	c.disk.put(url, data)
	return img, status, nil
}

func (c *Cache) download(url string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("artwork %s: %s", url, resp.Status)
	}
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > limit {
		return nil, resp.StatusCode, errors.New("artwork larger than 16 MiB")
	}
	return data, resp.StatusCode, nil
}

// sweep forgets covers that failed and are not asked for, then releases
// the covers shown longest ago while the kept ones take more than keep.
func (c *Cache) sweep(now time.Time) {
	c.swept = now
	var old []coverKey
	for k, e := range c.entries {
		switch {
		case e.loading:
		case e.img == nil && now.Sub(e.used) > failedRelease:
			delete(c.entries, k)
		case e.img != nil && now.Sub(e.used) > shownLately:
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
