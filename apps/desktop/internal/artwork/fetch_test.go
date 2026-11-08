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

	"github.com/egoist/mygo/ui"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// getOnce asks for a cover and runs what is posted back until its fetch
// has landed or failed; a sweep may be posted on the way.
func getOnce(t *testing.T, c *Cache, posted chan func(), tpl string, px int) {
	t.Helper()
	c.Get(tpl, px)
	key := coverKey{keyOf(tpl), 0}
	if strings.Contains(tpl, "{w}") {
		key.px = bucket(px)
	}
	for {
		select {
		case fn := <-posted:
			fn()
		case <-time.After(10 * time.Second):
			t.Fatal("the cover never loaded nor failed")
		}
		if e := c.entries[key]; e == nil || !e.loading {
			return
		}
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

// A cover not shown for a while, as the library's while another screen
// shows, is still decoded when its screen opens again: it shows in the
// first frame, not after a fetch.
func TestACoverShownAgainLaterIsStillDecoded(t *testing.T) {
	img := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(img) }))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 4)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	clock := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	lib, other := srv.URL+"/lib/{w}x{h}bb.jpg", srv.URL+"/other/{w}x{h}bb.jpg"
	getOnce(t, c, posted, lib, 100)
	getOnce(t, c, posted, other, 100)
	// Ten minutes on another screen, whose cover is asked for all along.
	for range 20 {
		clock = clock.Add(30 * time.Second)
		c.Get(other, 100)
	}
	if c.Get(lib, 100) == nil {
		t.Error("a cover not shown for ten minutes has to load again")
	}
}

// Past the bound, the covers shown longest ago are released first, and one
// shown just now stays.
func TestCoversPastTheBoundGoLeastLatelyShownFirst(t *testing.T) {
	img := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(img) }))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 4)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	clock := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	// pngBytes is 2×2: 16 bytes a cover and a third more for its smaller
	// copies, 21; room for two.
	c.keep = 42
	names := []string{"a", "b", "c"}
	for _, n := range names {
		getOnce(t, c, posted, srv.URL+"/"+n+"/{w}x{h}bb.jpg", 100)
		clock = clock.Add(time.Minute)
	}
	shown := map[string]bool{}
	for _, n := range names {
		e := c.entries[coverKey{srv.URL + "/" + n + "/{w}x{h}bb.jpg", 128}]
		shown[n] = e != nil && e.img != nil
	}
	if shown["a"] || !shown["b"] || !shown["c"] {
		t.Errorf("kept %v, want b and c", shown)
	}
}

// A size the CDN refused is not asked of it again once a smaller one came:
// the next launch reads that one from the disk.
func TestARefusedSizeIsReadFromDiskNextTime(t *testing.T) {
	img := pngBytes(t)
	var mu sync.Mutex
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if strings.Contains(r.URL.Path, "/512x512") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Write(img)
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	tpl := srv.URL + "/gen/{w}x{h}AM.jpg"
	for launch := range 2 {
		posted := make(chan func(), 4)
		c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport).WithDisk(dir)
		getOnce(t, c, posted, tpl, 400)
		if c.Get(tpl, 400) == nil {
			t.Fatalf("launch %d: the cover does not show", launch)
		}
	}
	if len(asked) != 2 {
		t.Errorf("two launches asked the CDN %q, want the refused size and the smaller one once", asked)
	}
}

// A private playlist's cover is a presigned link whose signature changes
// with every read: the library's tile and the playlist's page name it with
// two signatures, and it is fetched and decoded once, the next launch
// reading it from the disk.
func TestAPresignedCoverIsOnePictureWhateverItsSignature(t *testing.T) {
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
	signed := func(sig string) string {
		return srv.URL + "/sq-mq-us-037/99/76/image?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&X-Amz-Signature=" + sig
	}
	posted := make(chan func(), 4)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport).WithDisk(dir)
	getOnce(t, c, posted, signed("aaaa"), 200)
	if c.Get(signed("bbbb"), 512) == nil {
		t.Error("the page's link to the tile's cover, signed again, loads it again")
	}
	next := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport).WithDisk(dir)
	getOnce(t, next, posted, signed("cccc"), 512)
	if next.Get(signed("cccc"), 512) == nil {
		t.Fatal("the cover does not show after a restart")
	}
	if hits != 1 {
		t.Errorf("the cover was fetched %d times, want once", hits)
	}
}

// A window at rest draws no frames: a cover it shows was last asked for
// minutes ago when a fetch lands past the bound, and it stays, as does
// every cover of that last frame; one shown before that goes.
func TestCoversOfTheLastFrameStayWhileTheWindowRests(t *testing.T) {
	img := pngBytes(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(img) }))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 8)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	clock := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	// Room for one cover: the two of the last frame stay over the bound.
	c.keep = 21
	// No sweep of its own falls due in the test: only the late cover's
	// landing sweeps.
	c.swept = clock.Add(time.Hour)
	tpl := func(n string) string { return srv.URL + "/" + n + "/{w}x{h}bb.jpg" }
	getOnce(t, c, posted, tpl("old"), 100)
	clock = clock.Add(time.Minute)
	// The last frame: two covers shown, one still loading.
	getOnce(t, c, posted, tpl("shown"), 100)
	c.Get(tpl("late"), 100)
	// Ten minutes at rest, then the late cover lands, past the bound.
	clock = clock.Add(10 * time.Minute)
	select {
	case fn := <-posted:
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the late cover never landed")
	}
	has := func(n string) bool {
		e := c.entries[coverKey{tpl(n), 128}]
		return e != nil && e.img != nil
	}
	if !has("shown") || !has("late") {
		t.Errorf("covers of the last frame were released at rest: shown %v, late %v", has("shown"), has("late"))
	}
	if has("old") {
		t.Error("the cover no frame drew lately stays past the bound")
	}
}

// A picture uploaded larger than any view shows, as a private playlist's
// cover, is decoded at maxSide.
func TestAnOversizedCoverIsDecodedSmaller(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 3000, 2000))); err != nil {
		t.Fatal(err)
	}
	img, err := decode(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	b, ok := img.(*ui.Bitmap)
	if !ok {
		t.Fatalf("decoded as %T", img)
	}
	if w, h := b.Size(); w != maxSide || h != maxSide*2/3 {
		t.Errorf("decoded at %d×%d, want %d×%d", w, h, maxSide, maxSide*2/3)
	}
}

// A cover whose first load failed, as a request the network dropped, is
// tried again while its view keeps showing it, and shows once it loads.
func TestACoverThatFailedOnceShowsWhenItLoads(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		first := hits == 1
		mu.Unlock()
		if first {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 16)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	clock := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	settle := func() {
		t.Helper()
		select {
		case fn := <-posted:
			fn()
		case <-time.After(10 * time.Second):
			t.Fatal("the cover never loaded nor failed")
		}
	}
	tpl := srv.URL + "/cover/{w}x{h}bb.jpg"
	if c.Get(tpl, 100) != nil {
		t.Fatal("a cover shows before it loads")
	}
	settle()
	// The view asks every frame; for the first seconds the failure stands.
	for range 10 {
		if c.Get(tpl, 100) != nil {
			t.Fatal("the failed cover shows")
		}
		clock = clock.Add(100 * time.Millisecond)
	}
	clock = clock.Add(retryFirst)
	c.Get(tpl, 100)
	settle()
	if c.Get(tpl, 100) == nil {
		t.Errorf("after %v the cover that failed once still does not show", retryFirst)
	}
}
