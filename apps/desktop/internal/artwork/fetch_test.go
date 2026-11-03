package artwork

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// getOnce asks for a cover and runs what the fetch posts back.
func getOnce(t *testing.T, c *Cache, posted chan func(), tpl string, px int) {
	t.Helper()
	c.Get(tpl, px)
	select {
	case fn := <-posted:
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the cover never loaded nor failed")
	}
}

// Apple's CDN refuses Go's default User-Agent for a size it has not
// rendered, so a cover request says it is a browser from the web player.
func TestACoverRequestLooksLikeTheWebPlayers(t *testing.T) {
	img := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "Safari") || r.Referer() != "https://music.apple.com/" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Write(img)
	}))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 4)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	tpl := srv.URL + "/a/{w}x{h}bb.jpg"
	getOnce(t, c, posted, tpl, 100)
	if c.Get(tpl, 100) == nil {
		t.Fatal("a cover the CDN serves to browsers does not show")
	}
}

// A cover whose size is refused, as a generated playlist cover's 512, shows
// at a smaller size the CDN serves.
func TestARefusedSizeFallsBackToASmallerOne(t *testing.T) {
	img := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/512x512") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Write(img)
	}))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 4)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	tpl := srv.URL + "/gen/{w}x{h}AM.jpg"
	getOnce(t, c, posted, tpl, 400)
	if c.Get(tpl, 400) == nil {
		t.Fatal("a cover refused at 512 pixels does not show at a smaller size")
	}
}

// A cover fetched once is read from the disk cache by a later launch,
// without the network.
func TestACoverFetchedOnceComesFromDiskAfterARestart(t *testing.T) {
	img := pngBytes(t)
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.Write(img)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tpl := srv.URL + "/a/{w}x{h}bb.jpg"
	for launch := range 2 {
		posted := make(chan func(), 4)
		c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport).WithDisk(dir)
		getOnce(t, c, posted, tpl, 100)
		if c.Get(tpl, 100) == nil {
			t.Fatalf("launch %d: the cover does not show", launch)
		}
	}
	if hits != 1 {
		t.Errorf("two launches fetched the cover %d times, want 1", hits)
	}
}

// Trimming keeps the most recently used covers within the limit.
func TestTrimDropsTheLeastRecentlyUsedFirst(t *testing.T) {
	dir := t.TempDir()
	d := &diskCache{dir: dir}
	now := time.Now()
	for i, name := range []string{"old", "mid", "new"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, 100), 0o600); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
	d.trim(200)
	for name, want := range map[string]bool{"old": false, "mid": true, "new": true} {
		_, err := os.Stat(filepath.Join(dir, name))
		if got := err == nil; got != want {
			t.Errorf("%s kept = %v, want %v", name, got, want)
		}
	}
}
