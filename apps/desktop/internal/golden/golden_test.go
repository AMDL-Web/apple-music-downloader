// Package golden compares the desktop app's main screens, over the demo's
// backend, with the stills in testdata: a change to how a screen looks
// shows up as a failing still, and `go test -update` takes the new look.
//
// It is a package of its own because it pins the fonts: the stills are
// drawn in the app's own JetBrains Mono alone (fontconfig with no other
// font), so that they are the same on every Linux machine whatever fonts it
// has installed, and that must be set before the first text is laid out.
package golden

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/testkit"
	appui "amdl/apps/desktop/internal/ui"
	"amdl/internal/client"
)

var update = flag.Bool("update", false, "write the stills the screens draw now to testdata")

func TestMain(m *testing.M) {
	flag.Parse()
	conf := ""
	if runtime.GOOS == "linux" {
		var err error
		if conf, err = pinFonts(); err != nil {
			fmt.Fprintln(os.Stderr, "golden:", err)
			os.Exit(1)
		}
		os.Setenv("FONTCONFIG_FILE", conf)
	}
	// The clock the stills show is UTC's, wherever they are drawn.
	time.Local = time.UTC
	code := m.Run()
	if conf != "" {
		os.RemoveAll(filepath.Dir(conf))
	}
	os.Exit(code)
}

// pinFonts writes a fontconfig configuration whose only fonts are the
// app's own, every family falling back to them.
func pinFonts() (string, error) {
	fonts, err := filepath.Abs("../ui/fonts")
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "golden-fonts")
	if err != nil {
		return "", err
	}
	conf := filepath.Join(dir, "fonts.conf")
	err = os.WriteFile(conf, []byte(`<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>`+fonts+`</dir>
  <cachedir>`+dir+`</cachedir>
  <alias binding="strong"><family>sans-serif</family><prefer><family>JetBrains Mono</family></prefer></alias>
  <alias binding="strong"><family>system-ui</family><prefer><family>JetBrains Mono</family></prefer></alias>
  <alias binding="strong"><family>monospace</family><prefer><family>JetBrains Mono</family></prefer></alias>
</fontconfig>
`), 0o600)
	return conf, err
}

// The demo's clock stands still at this moment, so that every "ago" and
// every running job's progress is the same in each run.
var demoNow = time.Date(2026, 11, 7, 18, 30, 0, 0, time.UTC)

// screens are the stills: each main screen once, in both themes.
var screens = []struct {
	name string
	open func(a *appui.App)
}{
	{"home", func(a *appui.App) { a.Go(appui.ScreenHome) }},
	{"queue", func(a *appui.App) { a.Go(appui.ScreenQueue) }},
	{"library", func(a *appui.App) { a.Go(appui.ScreenLibrary) }},
	{"settings", func(a *appui.App) { a.GoSettings("quality") }},
	{"album", func(a *appui.App) {
		a.OpenAlbum(client.SearchHit{Type: "album", ID: "920001", Name: "Glass Weather", URL: "https://music.apple.com/us/album/glass-weather/920001"})
	}},
}

func TestMainScreensLookAsTheirStills(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the stills are drawn by Linux's text stack")
	}
	warmUp()
	for _, sc := range screens {
		for _, theme := range []string{"light", "dark"} {
			t.Run(sc.name+"-"+theme, func(t *testing.T) {
				var img *image.RGBA
				synctest.Test(t, func(t *testing.T) { img = draw(t, theme, sc.open) })
				compare(t, filepath.Join("testdata", sc.name+"-"+theme+".png"), img)
			})
		}
	}
}

// warmUp draws a frame outside any bubble, so that the text system,
// which lives as long as the program, starts outside the bubbles draw runs
// in. The rasteriser's helpers live as long too, and would share a frame's
// work between goroutines in and out of a bubble: on one core it draws
// alone.
func warmUp() {
	runtime.GOMAXPROCS(1)
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Background(ui.Hex("#ffffff")).Children(func() { ui.Text(c, "warm") })
	}, 1024, 640)
	tt.SetScale(1)
	tt.Frame()
	_ = tt.Image()
}

// draw opens a screen of the app over the demo's backend in a 1024×640
// window at scale 1 and returns its frame once it settled: once a frame
// sent nothing that was not answered and changed nothing. It runs in a
// synctest bubble, whose Wait is when every answer and cover that frame
// asked for has come back.
func draw(t *testing.T, theme string, open func(a *appui.App)) *image.RGBA {
	t.Helper()
	now := func() time.Time { return demoNow }
	b, c := testkit.Demo(t, now)
	th := testkit.NewThread(t)
	s := mode.DefaultSettings()
	s.Mode, s.RemoteURL = mode.Remote, "http://nas:18570"
	st := store.New(th.Post, nil)
	st.SetAfter(testkit.Soon)
	st.SetClock(now)
	a := &appui.App{Store: st, Art: artwork.NewWith(th.Post, b.Transport()).WithClock(now), Settings: s, Act: actions{}, Theme: theme,
		Setup: setup.New(th.Post, setup.Deps{GOOS: "linux", Now: now}), NoMotion: true, FlatBackdrop: true}
	st.Connect(c)
	t.Cleanup(st.Close)
	th.Until(func() bool { return st.Loaded })
	tt := ui.NewTester(a.View, 1024, 640)
	tt.SetScale(1)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.SetDark(theme == "dark")
	open(a)
	var last []byte
	for range 50 {
		tt.Frame()
		synctest.Wait()
		th.Drain()
		tt.Advance(100 * time.Millisecond)
		img := tt.Image()
		if last != nil && bytes.Equal(last, img.Pix) {
			return img
		}
		last = bytes.Clone(img.Pix)
	}
	t.Fatal("the screen never settled")
	return nil
}

// compare fails the test when img differs from the still at path: a pixel
// differs when a channel is off by more than channelSlack levels, and the
// still fails when more than pixelSlack of its pixels differ. It writes
// the frame and a map of the pixels that differ beside the test's output.
func compare(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	if *update {
		writePNG(t, path, img)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no still %s: run go test -update", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if want.Bounds() != img.Bounds() {
		t.Fatalf("%s is %v, the screen %v", path, want.Bounds(), img.Bounds())
	}
	diff := image.NewRGBA(img.Bounds())
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, _ := want.At(x, y).RGBA()
			o := img.PixOffset(x, y)
			if off(r, img.Pix[o]) > channelSlack || off(g, img.Pix[o+1]) > channelSlack || off(b, img.Pix[o+2]) > channelSlack {
				n++
				d := diff.PixOffset(x, y)
				diff.Pix[d], diff.Pix[d+3] = 255, 255
			}
		}
	}
	total := img.Bounds().Dx() * img.Bounds().Dy()
	if float64(n) > pixelSlack*float64(total) {
		// Kept after the test, to be looked at.
		dir, err := os.MkdirTemp("", "golden-"+strings.TrimSuffix(filepath.Base(path), ".png")+"-")
		if err != nil {
			t.Fatal(err)
		}
		writePNG(t, filepath.Join(dir, "got.png"), img)
		writePNG(t, filepath.Join(dir, "diff.png"), diff)
		t.Errorf("%d of %d pixels differ from %s by more than %d levels; the frame and the pixels that differ are in %s (go test -update takes the new look)",
			n, total, path, channelSlack, dir)
	}
}

// The slack of compare: antialiasing differs by a level or two between
// FreeType and cairo versions, and a glyph's edge by a few pixels.
const (
	channelSlack = 8
	pixelSlack   = 0.002
)

func off(want16 uint32, got uint8) int {
	d := int(want16>>8) - int(got)
	if d < 0 {
		return -d
	}
	return d
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// actions is the app's platform side, doing nothing.
type actions struct{}

func (actions) ApplySettings(mode.Settings)            {}
func (actions) TestServer(string, func([]mode.Check))  {}
func (actions) TestWrapper(string, func([]mode.Check)) {}
func (actions) Reveal(string)                          {}
func (actions) CopyText(string)                        {}
func (actions) ChooseFolder(string, func(string))      {}
func (actions) ShowWindow()                            {}
func (actions) FitPanel(float32)                       {}
func (actions) Starting() bool                         { return false }
func (actions) StartErr() string                       { return "" }
func (actions) Shared() string                         { return "" }
func (actions) Reconnect()                             {}
func (actions) ChooseFile(string, func(string))        {}
func (actions) OpenURL(string)                         {}
