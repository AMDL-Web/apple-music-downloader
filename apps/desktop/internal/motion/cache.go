package motion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"
)

const (
	// idleClose is how long a player nobody showed keeps its ffmpeg.
	idleClose = 20 * time.Second
	// maxPlayers bounds the ffmpeg processes running at once.
	maxPlayers = 3
	// retryAfter is how long a cover that failed waits to be tried again.
	retryAfter = 5 * time.Minute
	// dirLimit bounds the cached renditions on disk; one is a few MB.
	dirLimit = 300 << 20
	// fileLimit bounds one rendition, against a playlist that never ends.
	fileLimit = 64 << 20
)

// Cache hands out players of motion covers, fetching each rendition once
// into its directory. Use Get and SetFFmpeg on the UI thread.
type Cache struct {
	post   func(func())
	dir    string
	http   *http.Client
	log    *slog.Logger
	ffmpeg string

	entries map[key]*entry
	slots   chan struct{}
	sweepAt bool
}

type key struct {
	master string
	px     int
}

type entry struct {
	player   *ui.VideoPlayer
	loading  bool
	failedAt time.Time
	used     time.Time
}

// New returns a cache keeping renditions in dir, which hands players to
// the UI thread with post. It decodes nothing until SetFFmpeg names an
// ffmpeg.
func New(post func(func()), dir string, log *slog.Logger) *Cache {
	if log == nil {
		log = slog.Default()
	}
	c := &Cache{
		post: post, dir: dir, log: log,
		http:    &http.Client{Transport: netproxy.Transport(), Timeout: time.Minute},
		entries: map[key]*entry{},
		slots:   make(chan struct{}, 2),
	}
	go trimDir(dir, dirLimit)
	return c
}

// SetFFmpeg sets the ffmpeg that decodes the covers; "" turns them off.
// Players of another ffmpeg stop.
func (c *Cache) SetFFmpeg(path string) {
	if path == c.ffmpeg {
		return
	}
	c.ffmpeg = path
	for k, e := range c.entries {
		if e.player != nil {
			e.player.Close()
		}
		delete(c.entries, k)
	}
}

// bucket rounds a size up, so views of nearly one size share a player.
func bucket(px int) int {
	for _, b := range []int{256, 384, 512, 768, 1024} {
		if px <= b {
			return b
		}
	}
	return 1080
}

// Get returns a looping player of the motion cover whose master playlist
// is master, at least px pixels on its shorter side; nil until it is
// ready, without an ffmpeg, or after it failed. Views call it every frame
// they show the cover: a player not asked for in idleClose stops.
func (c *Cache) Get(master string, px int) *ui.VideoPlayer {
	if master == "" || c.ffmpeg == "" {
		return nil
	}
	now := time.Now()
	c.sweep(now)
	k := key{master, bucket(px)}
	e := c.entries[k]
	if e == nil {
		e = &entry{}
		c.entries[k] = e
	}
	e.used = now
	if e.player != nil || e.loading || (!e.failedAt.IsZero() && now.Sub(e.failedAt) < retryAfter) {
		return e.player
	}
	e.loading = true
	ffmpeg := c.ffmpeg
	go func() {
		c.slots <- struct{}{}
		p, err := c.open(k, ffmpeg)
		<-c.slots
		c.post(func() {
			e.loading = false
			switch {
			case err != nil:
				e.failedAt = time.Now()
				c.log.Info("motion cover not played", "master", master, "error", err)
			case c.entries[k] != e:
				p.Close() // dropped while it opened
			default:
				e.player = p
			}
		})
	}()
	return nil
}

// Has reports whether a player of master is ready, without asking for one.
func (c *Cache) Has(master string) bool {
	for k, e := range c.entries {
		if k.master == master && e.player != nil {
			return true
		}
	}
	return false
}

// sweep stops players not asked for lately, and the least recently asked
// for beyond maxPlayers. While players run it comes back by itself, for a
// window that stopped drawing.
func (c *Cache) sweep(now time.Time) {
	var live []key
	for k, e := range c.entries {
		switch {
		case e.loading:
		case now.Sub(e.used) > idleClose:
			if e.player != nil {
				e.player.Close()
			}
			delete(c.entries, k)
		case e.player != nil:
			live = append(live, k)
		}
	}
	sort.Slice(live, func(i, j int) bool { return c.entries[live[i]].used.After(c.entries[live[j]].used) })
	for _, k := range live[min(len(live), maxPlayers):] {
		c.entries[k].player.Close()
		delete(c.entries, k)
	}
	if len(live) > 0 && !c.sweepAt {
		c.sweepAt = true
		time.AfterFunc(idleClose, func() {
			c.post(func() {
				c.sweepAt = false
				c.sweep(time.Now())
			})
		})
	}
}

// open fetches the rendition for k when it is not cached yet and starts a
// player of it.
func (c *Cache) open(k key, ffmpeg string) (*ui.VideoPlayer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	base, err := url.Parse(k.master)
	if err != nil {
		return nil, err
	}
	text, err := c.text(ctx, k.master)
	if err != nil {
		return nil, err
	}
	v, ok := Pick(ParseMaster(text, base), k.px)
	if !ok {
		return nil, errors.New("motion: no video rendition")
	}
	file, err := c.file(ctx, v)
	if err != nil {
		return nil, err
	}
	w, h := outSize(v, k.px)
	d, err := newDecoder(ffmpeg, file, w, h, v.FrameRate)
	if err != nil {
		return nil, err
	}
	p, err := ui.NewVideoPlayer(d)
	if err != nil {
		d.Close()
		return nil, err
	}
	return p.SetLoop(true), nil
}

// outSize is the decoded size: px on the shorter side, the rendition's
// shape, even numbers as encoders want.
func outSize(v Variant, px int) (w, h int) {
	even := func(n float64) int { return max(2, int(n/2+0.5)*2) }
	if v.Width <= 0 || v.Height <= 0 {
		return even(float64(px)), even(float64(px))
	}
	if v.Width <= v.Height {
		return even(float64(px)), even(float64(px) * float64(v.Height) / float64(v.Width))
	}
	return even(float64(px) * float64(v.Width) / float64(v.Height)), even(float64(px))
}

// file returns the cached copy of v, fetching it first when there is none.
func (c *Cache) file(ctx context.Context, v Variant) (string, error) {
	sum := sha1.Sum([]byte(v.URL))
	path := filepath.Join(c.dir, hex.EncodeToString(sum[:10])+".mp4")
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		now := time.Now()
		// Only the cache's trimming reads the time; failing to touch it
		// trims this file a little early.
		_ = os.Chtimes(path, now, now)
		return path, nil
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return "", err
	}
	base, err := url.Parse(v.URL)
	if err != nil {
		return "", err
	}
	text, err := c.text(ctx, v.URL)
	if err != nil {
		return "", err
	}
	pieces, err := parseMedia(text, base)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(c.dir, "*.part")
	if err != nil {
		return "", err
	}
	// Gone after the rename; left behind only by a fetch that failed.
	defer os.Remove(tmp.Name())
	written := int64(0)
	for _, p := range pieces {
		n, err := c.copyPiece(ctx, tmp, p, fileLimit-written)
		if err != nil {
			tmp.Close()
			return "", err
		}
		written += n
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return path, os.Rename(tmp.Name(), path)
}

// copyPiece appends one byte range to w, reading at most limit bytes.
func (c *Cache) copyPiece(ctx context.Context, w io.Writer, p piece, limit int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return 0, err
	}
	if p.n >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", p.off, p.off+p.n-1))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body := io.Reader(resp.Body)
	want := limit
	switch {
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusOK && p.n >= 0:
		// The server sent the whole file: take the range out of it.
		if _, err := io.CopyN(io.Discard, body, p.off); err != nil {
			return 0, err
		}
		want = min(want, p.n)
	case resp.StatusCode == http.StatusOK:
	default:
		return 0, fmt.Errorf("motion: %s: %s", p.url, resp.Status)
	}
	if p.n >= 0 {
		want = min(want, p.n)
	}
	n, err := io.Copy(w, io.LimitReader(body, want))
	if err == nil && p.n >= 0 && n != p.n {
		err = fmt.Errorf("motion: %s: got %d of %d bytes", p.url, n, p.n)
	}
	return n, err
}

func (c *Cache) text(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("motion: %s: %s", u, resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(raw), err
}

// trimDir deletes the least recently played renditions beyond limit bytes,
// and parts of fetches that never finished.
func trimDir(dir string, limit int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		at   time.Time
	}
	var files []file
	for _, de := range entries {
		info, err := de.Info()
		if err != nil || de.IsDir() {
			continue
		}
		path := filepath.Join(dir, de.Name())
		// Trimming is best effort: a file that will not go is tried again
		// on the next start.
		if strings.HasSuffix(de.Name(), ".part") {
			os.Remove(path)
			continue
		}
		files = append(files, file{path, info.Size(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.After(files[j].at) })
	total := int64(0)
	for _, f := range files {
		total += f.size
		if total > limit {
			os.Remove(f.path)
		}
	}
}
