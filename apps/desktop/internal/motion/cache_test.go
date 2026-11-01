package motion

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// hls serves motion covers as Apple's CDN does: a master playlist with one
// square H.264 rendition, its media playlist of two byte ranges of one
// file, and the file, which honors ranges. It counts the requests for each
// path, and answers 404 for the covers in missing.
type hls struct {
	*httptest.Server
	media   []byte
	mu      sync.Mutex
	hits    map[string]int
	missing map[string]bool
}

func newHLS(t *testing.T, media []byte) *hls {
	h := &hls{media: media, hits: map[string]int{}, missing: map[string]bool{}}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		cover, kind, _ := strings.Cut(name, ".")
		h.mu.Lock()
		h.hits[name]++
		missing := h.missing[cover]
		h.mu.Unlock()
		n := len(h.media)
		switch {
		case missing:
			http.NotFound(w, r)
		case kind == "master.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=900000,CODECS=\"avc1.64001f\",FRAME-RATE=30.000,RESOLUTION=768x768\n%s.video.m3u8\n", cover)
		case kind == "video.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-MAP:URI=\"%s.mp4\",BYTERANGE=\"%d@0\"\n#EXTINF:1,\n#EXT-X-BYTERANGE:%d@%d\n%s.mp4\n", cover, n/2, n-n/2, n/2, cover)
		case kind == "mp4":
			http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(h.media))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hls) master(cover string) string { return h.URL + "/" + cover + ".master.m3u8" }

// asked is how many times cover's master playlist was fetched.
func (h *hls) asked(cover string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hits[cover+".master.m3u8"]
}

// video is a decoder of numbered frames, 100 ms each, whose number is the
// red of every pixel; it tells when it closes. broken ones decode nothing.
type video struct {
	broken bool
	n      int
	once   sync.Once
	closed chan struct{}
}

func (v *video) Next() (ui.VideoFrame, error) {
	if v.broken {
		return ui.VideoFrame{}, errors.New("not a video")
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for i := range 4 {
		img.Set(i%2, i/2, color.RGBA{uint8(v.n), 0, 0, 255})
	}
	v.n++
	return ui.VideoFrame{Image: img, Duration: 100 * time.Millisecond}, nil
}

func (v *video) Rewind() error { v.n = 0; return nil }

func (v *video) Close() error {
	v.once.Do(func() { close(v.closed) })
	return nil
}

// waitClosed fails unless v closes within a few seconds: a player closes
// its decoder on a goroutine of its own.
func waitClosed(t *testing.T, v *video, what string) {
	t.Helper()
	select {
	case <-v.closed:
	case <-time.After(5 * time.Second):
		t.Errorf("%s: its decoder never closed", what)
	}
}

func isClosed(v *video) bool {
	select {
	case <-v.closed:
		return true
	default:
		return false
	}
}

// fakes is a cache of covers from an hls server, on a clock the test
// moves, whose posts the test runs as the UI thread would, and whose
// decoders are videos, or a real ffmpeg's when real.
type fakes struct {
	*Cache
	posted chan func()
	clock  time.Time
	mu     sync.Mutex
	videos []*video
	broken bool
}

func newFakes(t *testing.T, real bool) *fakes {
	f := &fakes{posted: make(chan func(), 64), clock: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	f.Cache = New(func(fn func()) { f.posted <- fn }, t.TempDir(), nil)
	// No proxy between the test and its server.
	f.http = &http.Client{Timeout: 10 * time.Second}
	f.now = func() time.Time { return f.clock }
	if !real {
		f.decoder = func(string, string, int, int, float64) (ui.VideoDecoder, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			v := &video{broken: f.broken, closed: make(chan struct{})}
			f.videos = append(f.videos, v)
			return v, nil
		}
		f.SetFFmpeg("ffmpeg")
	}
	t.Cleanup(func() { f.SetFFmpeg("") })
	return f
}

// started is the decoders started so far.
func (f *fakes) started() []*video {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*video(nil), f.videos...)
}

// ready asks for master, as each frame showing it does, until its player
// is there.
func (f *fakes) ready(t *testing.T, master string) *ui.VideoPlayer {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if p := f.Get(master, 512); p != nil {
			return p
		}
		select {
		case fn := <-f.posted:
			fn()
		case <-deadline:
			t.Fatalf("%s never played", master)
		}
	}
}

// settle runs what the cache posts back once a player it asked for opened
// or failed to.
func (f *fakes) settle(t *testing.T) {
	t.Helper()
	select {
	case fn := <-f.posted:
		fn()
	case <-time.After(10 * time.Second):
		t.Fatal("the cache never answered")
	}
}

var fakeMedia = []byte("the file of a rendition, which only a real ffmpeg reads")

// A cover asked for again and again before it plays, by every frame of the
// queue hero and of the album page at their sizes, is fetched once and has
// one decoder; every view then shows that one player.
func TestEveryViewOfACoverShowsOnePlayer(t *testing.T) {
	h, f := newHLS(t, fakeMedia), newFakes(t, false)
	m := h.master("a")
	for range 20 {
		f.Get(m, 392)
		f.Get(m, 496)
	}
	p := f.ready(t, m)
	for _, px := range []int{392, 496, 512} {
		if got := f.Get(m, px); got != p {
			t.Errorf("at %d px the cover has player %p, not %p", px, got, p)
		}
	}
	if n := len(f.started()); n != 1 {
		t.Errorf("%d decoders started for one cover", n)
	}
	if n := h.asked("a"); n != 1 {
		t.Errorf("the cover's playlist was fetched %d times", n)
	}
	if !f.Has(m) {
		t.Errorf("Has says the cover plays nowhere")
	}
}

// A fourth cover playing stops the one shown longest ago, and only it.
func TestAFourthCoverStopsTheOneShownLongestAgo(t *testing.T) {
	h, f := newHLS(t, fakeMedia), newFakes(t, false)
	players := map[string]*ui.VideoPlayer{}
	for _, c := range []string{"a", "b", "c", "d"} {
		players[c] = f.ready(t, h.master(c))
		f.clock = f.clock.Add(time.Second)
	}
	v := f.started()
	waitClosed(t, v[0], "the cover shown longest ago")
	for i, c := range []string{"b", "c", "d"} {
		if isClosed(v[i+1]) {
			t.Errorf("cover %s stopped too", c)
		}
		if got := f.Get(h.master(c), 512); got != players[c] {
			t.Errorf("cover %s has player %p, not its %p", c, got, players[c])
		}
	}
	if f.Get(h.master("a"), 512) != nil || f.Has(h.master("a")) {
		t.Errorf("the stopped cover still has a player")
	}
}

// A cover away from the screen, behind Settings or a hidden window, keeps
// its player for up to five minutes; after that it starts over in a new
// one, and the old one's decoder stops.
func TestACoverAwayKeepsItsPlayerForFiveMinutes(t *testing.T) {
	h, f := newHLS(t, fakeMedia), newFakes(t, false)
	m := h.master("a")
	p := f.ready(t, m)
	f.clock = f.clock.Add(4*time.Minute + 59*time.Second)
	if got := f.Get(m, 512); got != p {
		t.Errorf("back after 4 min 59 s, the cover has player %p, not its %p", got, p)
	}
	f.clock = f.clock.Add(5*time.Minute + time.Second)
	if got := f.Get(m, 512); got != nil {
		t.Errorf("back after 5 min 1 s, the cover still has a player")
	}
	waitClosed(t, f.started()[0], "the cover away for long")
	if q := f.ready(t, m); q == p {
		t.Errorf("the cover started over in its old player")
	}
	if n := len(f.started()); n != 2 {
		t.Errorf("%d decoders started, not the first and a new one", n)
	}
}

// A player out of the view holds its frame and, shown again, goes on from
// there, however long it was gone: what keeping a player lets a cover do.
func TestAPlayerShownAgainGoesOnFromItsFrame(t *testing.T) {
	p, err := ui.NewVideoPlayer(&video{closed: make(chan struct{})})
	if err != nil {
		t.Fatal(err)
	}
	shown := true
	tt := ui.NewTester(func(c *ui.Context) {
		if shown {
			ui.Video(c, p).Size(4, 4)
		}
	}, 4, 4)
	frame := func() int { img := tt.Image(); return int(img.Pix[img.PixOffset(2, 2)]) }
	// The frames decode on a goroutine of their own, in real time.
	play := func(n int) {
		deadline := time.Now().Add(5 * time.Second)
		for at := frame(); frame() < at+n && time.Now().Before(deadline); {
			tt.Advance(100 * time.Millisecond)
			time.Sleep(5 * time.Millisecond)
		}
	}
	play(5)
	left := frame()
	if left < 5 {
		t.Fatalf("the video got to frame %d only", left)
	}
	shown = false
	tt.Frame()
	tt.Advance(30 * time.Second)
	shown = true
	tt.Frame()
	if got := frame(); got < left || got > left+1 {
		t.Errorf("shown again after 30 s, the video is at frame %d, not %d where it left", got, left)
	}
	at := frame()
	play(3)
	if frame() < at+3 {
		t.Errorf("shown again, the video stays at frame %d", frame())
	}
}

// A cover that cannot play leaves the still, without a crash, and is not
// fetched again on every frame: without an ffmpeg nothing is fetched; one
// that will not start, a video that will not decode or a cover the CDN no
// longer has is tried again only after a while.
func TestACoverThatCannotPlayLeavesTheStill(t *testing.T) {
	t.Run("no ffmpeg", func(t *testing.T) {
		h, f := newHLS(t, fakeMedia), newFakes(t, false)
		f.SetFFmpeg("")
		if f.Get(h.master("a"), 512) != nil || f.Has(h.master("a")) || h.asked("a") != 0 {
			t.Errorf("without an ffmpeg, a cover played or was fetched (%d times)", h.asked("a"))
		}
	})
	for _, c := range []struct {
		name  string
		setup func(h *hls, f *fakes)
	}{
		{"an ffmpeg that will not start", func(h *hls, f *fakes) {
			f.SetFFmpeg(filepath.Join(f.dir, "no-ffmpeg"))
		}},
		{"a video that will not decode", func(h *hls, f *fakes) {
			f.broken = true
		}},
		{"a cover the CDN no longer has", func(h *hls, f *fakes) {
			h.missing["a"] = true
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			real := strings.Contains(c.name, "ffmpeg")
			h, f := newHLS(t, fakeMedia), newFakes(t, real)
			c.setup(h, f)
			m := h.master("a")
			f.Get(m, 512)
			f.settle(t)
			for range 10 {
				if f.Get(m, 512) != nil {
					t.Fatal("the cover plays")
				}
			}
			// A frame that tried it again would hear back.
			select {
			case fn := <-f.posted:
				fn()
				t.Errorf("a frame tried the cover again")
			case <-time.After(300 * time.Millisecond):
			}
			if f.Has(m) || h.asked("a") != 1 {
				t.Errorf("Has %v, fetched %d times within a minute", f.Has(m), h.asked("a"))
			}
			for _, v := range f.started() {
				waitClosed(t, v, "the video that did not play")
			}
			f.clock = f.clock.Add(retryAfter + time.Second)
			f.Get(m, 512)
			f.settle(t)
			if h.asked("a") != 2 {
				t.Errorf("after %v the cover was fetched %d times, not tried again", retryAfter, h.asked("a"))
			}
		})
	}
}

// Another ffmpeg, as Settings picks, stops the players of the last one.
func TestAnotherFFmpegStopsThePlayers(t *testing.T) {
	h, f := newHLS(t, fakeMedia), newFakes(t, false)
	f.ready(t, h.master("a"))
	f.SetFFmpeg("another-ffmpeg")
	waitClosed(t, f.started()[0], "the player of the last ffmpeg")
	if f.Has(h.master("a")) {
		t.Errorf("the cover still has a player")
	}
}

// With a real ffmpeg, the process of the cover a fourth one stops exits.
func TestAStoppedCoversFFmpegExits(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil || runtime.GOOS == "windows" {
		t.Skip("no ffmpeg, or no ps to count its processes")
	}
	clip := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=64x64:rate=10", "-t", "1", "-c:v", "mpeg4", "-pix_fmt", "yuv420p", clip).CombinedOutput(); err != nil {
		t.Fatalf("making a clip: %v: %s", err, out)
	}
	media, err := os.ReadFile(clip)
	if err != nil {
		t.Fatal(err)
	}
	h, f := newHLS(t, media), newFakes(t, true)
	f.SetFFmpeg(ff)
	// running counts the ffmpeg processes decoding from the cache's files.
	running := func() int {
		out, err := exec.Command("ps", "-A", "-o", "command=").Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(out), f.dir+string(filepath.Separator))
	}
	for _, c := range []string{"a", "b", "c"} {
		f.ready(t, h.master(c))
		f.clock = f.clock.Add(time.Second)
	}
	if n := running(); n != 3 {
		t.Fatalf("%d ffmpeg processes for three covers", n)
	}
	f.ready(t, h.master("d"))
	deadline := time.Now().Add(5 * time.Second)
	for running() != 3 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := running(); n != 3 {
		t.Errorf("%d ffmpeg processes after a fourth cover stopped the first", n)
	}
	if f.Has(h.master("a")) {
		t.Errorf("the first cover still has a player")
	}
}

// The cache's directory keeps the renditions played last within its limit,
// and drops parts of fetches that never finished.
func TestTheCacheDirKeepsTheLastPlayed(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, name := range []string{"old.mp4", "mid.mp4", "new.mp4", "x.part"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, 100), 0o644); err != nil {
			t.Fatal(err)
		}
		at := now.Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
	}
	trimDir(dir, 250)
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if got := strings.Join(left, " "); got != "mid.mp4 new.mp4" {
		t.Errorf("the directory keeps %q, not the two played last", got)
	}
}
