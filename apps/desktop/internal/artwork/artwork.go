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

// Cache holds decoded covers. Use Get on the UI thread.
type Cache struct {
	post    func(func())
	http    *http.Client
	slots   chan struct{}
	entries map[string]*entry
	swept   time.Time
}

type entry struct {
	img     ui.ImageSource
	failed  bool
	used    time.Time
	loading bool
}

// New returns a cache that hands decoded covers to the UI thread with post,
// which redraws.
func New(post func(func())) *Cache {
	return &Cache{
		post: post,
		// Covers come from Apple's CDN on the internet, so they go through
		// the system's proxy settings as a browser's requests would.
		http:    &http.Client{Transport: netproxy.Transport(), Timeout: 30 * time.Second},
		slots:   make(chan struct{}, 6),
		entries: map[string]*entry{},
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
	now := time.Now()
	e := c.entries[key]
	if e == nil {
		e = &entry{}
		c.entries[key] = e
	}
	e.used = now
	if e.img == nil && !e.loading && !e.failed {
		e.loading = true
		go c.fetch(key, e)
	}
	if now.Sub(c.swept) > idleRelease/2 {
		c.sweep(now)
	}
	return e.img
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
			e.failed = true
			return
		}
		e.img = img
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

// sweep drops covers not asked for lately; a failed one is forgotten too,
// so it is tried again when it shows next.
func (c *Cache) sweep(now time.Time) {
	c.swept = now
	for k, e := range c.entries {
		if !e.loading && now.Sub(e.used) > idleRelease {
			delete(c.entries, k)
		}
	}
}
