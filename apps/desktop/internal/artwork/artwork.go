// Package artwork fetches and decodes album covers for the views: off the UI
// thread, a few at a time, sized for where they show, and released a while
// after they stop showing.
package artwork

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"
)

// idleRelease is how long a cover nobody asked for keeps its pixels.
const idleRelease = 2 * time.Minute

// retryFirst is how long a cover that failed to load waits before it is
// tried again, doubling with each failure up to retryMost: a view asks for
// it every frame, so without a retry one dropped request would leave its
// placeholder for as long as it shows.
const (
	retryFirst = 5 * time.Second
	retryMost  = 5 * time.Minute
)

// Cache holds decoded covers. Use Get on the UI thread.
type Cache struct {
	post    func(func())
	http    *http.Client
	slots   chan struct{}
	entries map[string]*entry
	swept   time.Time
	// now is time.Now but in tests.
	now func() time.Time
}

type entry struct {
	img     ui.ImageSource
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
		entries: map[string]*entry{},
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
	px = bucket(px)
	key := URL(template, px)
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
	if now.Sub(c.swept) > idleRelease/2 {
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

func (c *Cache) fetch(url string, e *entry) {
	c.slots <- struct{}{}
	defer func() { <-c.slots }()
	img, err := c.load(url)
	c.post(func() {
		e.loading = false
		if err != nil {
			e.failures++
			e.failedAt = c.now()
			// The view asks again only as it redraws: one is due then.
			time.AfterFunc(retryWait(e.failures), func() { c.post(func() {}) })
			return
		}
		e.img, e.failures = img, 0
	})
}

func (c *Cache) load(url string) (ui.ImageSource, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("artwork %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	return ui.DecodeImage(data)
}

// sweep drops covers not asked for lately, failed ones too.
func (c *Cache) sweep(now time.Time) {
	c.swept = now
	for k, e := range c.entries {
		if !e.loading && now.Sub(e.used) > idleRelease {
			delete(c.entries, k)
		}
	}
}
