package ui

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/setup"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// The flicker tests. A request that answers within a frame must not show
// its waiting state, drop what the screen showed, or move anything: the
// frame the user sees between the click and the answer has to be the one
// before or the one after. noFlash asserts that for any click; a fix for a
// screen that flashes comes with a test that calls it, and a new screen with
// an async action should too.

// frameSnap is what one frame shows: its texts, the box of each, and a hash
// of its pixels, which also catches a control that greys or dims.
type frameSnap struct {
	texts  []string
	rects  map[string]ui.Rect
	pixels [sha256.Size]byte
}

// snapFrame takes tt's last frame. Rects are those of the first element
// showing each text, so two elements with one text count as one.
func snapFrame(tt *ui.Tester) frameSnap {
	s := frameSnap{texts: tt.Texts(), rects: map[string]ui.Rect{}}
	for _, x := range s.texts {
		if r, ok := tt.Find(x); ok {
			s.rects[x] = r
		}
	}
	s.pixels = sha256.Sum256(tt.Image().Pix)
	return s
}

// differs says how two frames differ, or returns "" when they match.
func (a frameSnap) differs(b frameSnap) string {
	if d := a.layoutDiffers(b); d != "" {
		return d
	}
	if a.pixels != b.pixels {
		return "pixels (something greyed, dimmed or moved without a text change)"
	}
	return ""
}

// layoutDiffers is differs without the pixels, for frames that are apart in
// time: a spinner or a pill that fades differs in pixels with no change of
// state.
func (a frameSnap) layoutDiffers(b frameSnap) string {
	if x, y := strings.Join(a.texts, "\x00"), strings.Join(b.texts, "\x00"); x != y {
		gone, came := textDiff(a.texts, b.texts)
		return fmt.Sprintf("texts: %q gone, %q new", gone, came)
	}
	for text, r := range a.rects {
		o := b.rects[text]
		if abs32(r.X-o.X) > 0.5 || abs32(r.Y-o.Y) > 0.5 || abs32(r.W-o.W) > 0.5 || abs32(r.H-o.H) > 0.5 {
			return fmt.Sprintf("the box of %q: %v vs %v", text, r, o)
		}
	}
	return ""
}

// textDiff is the texts a has that b lacks, and the ones b has that a lacks.
func textDiff(a, b []string) (gone, came []string) {
	count := map[string]int{}
	for _, x := range a {
		count[x]++
	}
	for _, x := range b {
		count[x]--
	}
	for x, n := range count {
		for ; n > 0; n-- {
			gone = append(gone, x)
		}
		for ; n < 0; n++ {
			came = append(came, x)
		}
	}
	sort.Strings(gone)
	sort.Strings(came)
	return gone, came
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// noFlash clicks the element showing click and lets answer deliver the
// answer the click waits for, which must be an instant one: a fake that has
// it ready. It fails the test when the frame rendered by the click, which a
// real answer a few milliseconds away leaves on screen for one refresh, is
// neither the frame before the click nor the frame after the answer, in
// texts, the box of each text or pixels. The pointer rests on the element
// first, so a hover fill is in the frame before as well. It returns the three
// frames for further checks.
func noFlash(t *testing.T, tt *ui.Tester, click string, answer func()) (before, mid, after frameSnap) {
	t.Helper()
	before = restOn(tt, click)
	if err := tt.Click(click); err != nil {
		t.Fatal(err)
	}
	mid = snapFrame(tt)
	answer()
	tt.Frame()
	after = snapFrame(tt)
	d1, d2 := mid.differs(before), mid.differs(after)
	if d1 != "" && d2 != "" {
		t.Errorf("clicking %q flashes: the frame before its answer is neither the one before the click (%s) nor the one after the answer (%s)", click, d1, d2)
	}
	return before, mid, after
}

// restOn rests the pointer on the element showing text and takes the frame,
// so that the frame already shows its hover.
func restOn(tt *ui.Tester, text string) frameSnap {
	if r, ok := tt.Find(text); ok {
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
	}
	tt.Frame()
	return snapFrame(tt)
}

// fakeAmdl is a backend whose every answer is ready at once.
func fakeAmdl(w http.ResponseWriter, r *http.Request) {
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/v1/wrapper/login":
		reply(http.StatusUnauthorized, map[string]any{"error": map[string]string{"code": "authentication_failed", "message": "wrong"}})
	case "POST /api/v1/config/reload", "GET /api/v1/config":
		reply(http.StatusOK, map[string]any{"config": map[string]any{}, "persisted": true})
	case "POST /api/v1/downloads":
		reply(http.StatusAccepted, domain.BatchSubmitResponse{Accepted: 1, Results: []domain.SubmitResult{
			{Job: &domain.Job{ID: "j1", Type: "song", Status: domain.JobQueued, Title: "Elma"}}}})
	case "GET /api/v1/search":
		reply(http.StatusOK, client.SearchResult{Term: "yoru", Songs: &client.HitGroup{Items: []client.SearchHit{
			{ID: "1", Type: "song", Name: "Elma", URL: "https://music.apple.com/us/song/elma/1", ArtistName: "Yorushika"}}}})
	default:
		http.NotFound(w, r)
	}
}

// liveApp is an App whose store follows a fake backend. The answers queue up
// until run applies them, in order, on the test's goroutine, as the UI
// thread would, until the condition holds.
func liveApp(t *testing.T, s mode.Settings) (*App, func(until func() bool)) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(fakeAmdl))
	t.Cleanup(srv.Close)
	a, _ := newTestApp(s)
	queue := make(chan func(), 256)
	a.Store = store.New(func(fn func()) { queue <- fn }, nil)
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	return a, func(until func() bool) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !until() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("the fake backend's answer never arrived")
			}
		}
	}
}

// liveTester is a Tester for a, whose store takes its time from it.
func liveTester(a *App) *ui.Tester {
	tt := ui.NewTester(a.View, 1280, 800)
	a.Store.SetClock(tt.Now)
	return tt
}

// fakeDiagnose answers a test of a server with result, once gate (when set)
// is closed.
type fakeDiagnose struct {
	gate   chan struct{}
	result setup.Server
}

func (f *fakeDiagnose) deps(d *setup.Deps) {
	d.Diagnose = func(_ context.Context, address string) setup.Server {
		if f.gate != nil {
			<-f.gate
		}
		r := f.result
		r.Address = address
		return r
	}
}

func goodServer() setup.Server {
	return setup.Server{State: setup.Ready,
		Reach:   setup.Line{State: setup.Ready, Text: "amdl 2.0.0 · 12 ms"},
		Account: setup.Line{State: setup.Ready, Text: "us"},
		Token:   setup.Line{State: setup.Missing, Text: "None"},
		FFmpeg:  setup.Line{State: setup.Ready, Text: "/usr/bin/ffmpeg"},
		Folder:  setup.Line{State: setup.Ready, Text: "/volume1/music · 2.1 TB free"}}
}

func TestRemoteTestKeepsTheChecklistWhileItRuns(t *testing.T) {
	fd := &fakeDiagnose{result: goodServer()}
	a, _, tt, drain, _ := setupApp(t, mode.DefaultSettings(), mode.Remote, fd.deps)
	a.su.serverAddr = "http://nas:18570"
	tt.Frame()

	// The first test: nothing on screen before it, and nothing but the
	// answer after.
	before, _, after := noFlash(t, tt, "Test connection", drain)
	if before.differs(after) == "" || !tt.HasText("Media user token") {
		t.Fatalf("the first test showed nothing: %q", tt.Texts())
	}

	// Testing again keeps every row and the footer, and an answer of the same
	// facts changes nothing at all.
	before, _, after = noFlash(t, tt, "Test connection", drain)
	if d := before.differs(after); d != "" {
		t.Errorf("an unchanged answer changed the screen: %s", d)
	}
	for _, want := range []string{"Apple ID", "Media user token", "ffmpeg", "Folder", "Ready"} {
		if !tt.HasText(want) {
			t.Errorf("after the second test no %q: %q", want, tt.Texts())
		}
	}
}

func TestRemoteTestSaysTestingOnlyOnceItIsSlow(t *testing.T) {
	fd := &fakeDiagnose{result: goodServer(), gate: make(chan struct{})}
	a, _, tt, drain, _ := setupApp(t, mode.DefaultSettings(), mode.Remote, fd.deps)
	a.su.serverAddr = "http://nas:18570"
	a.Setup.Server = fd.result
	a.Setup.Server.Address = "http://nas:18570"
	before := restOn(tt, "Test connection")
	if err := tt.Click("Test connection"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(statusDelay - time.Millisecond)
	if d := snapFrame(tt).layoutDiffers(before); d != "" || tt.HasText("Testing") {
		t.Fatalf("before statusDelay the screen changed (%s): %q", d, tt.Texts())
	}
	tt.Advance(2 * time.Millisecond)
	for _, want := range []string{"Testing", "Apple ID", "Media user token", "ffmpeg", "Folder", "Ready"} {
		if !tt.HasText(want) {
			t.Errorf("past statusDelay no %q: the rows stay and the Server row says it is testing: %q", want, tt.Texts())
		}
	}
	mid := snapFrame(tt)
	for _, text := range []string{"Server", "Apple ID", "Folder"} {
		if r, o := mid.rects[text], before.rects[text]; r != o {
			t.Errorf("the row %q moved while waiting: %v vs %v", text, r, o)
		}
	}
	close(fd.gate)
	drain()
	if tt.HasText("Testing") || !tt.HasText("amdl 2.0.0 · 12 ms") {
		t.Errorf("after the answer: %q", tt.Texts())
	}
}

func TestRemoteFirstSlowTestShowsOneRowThenTheChecklist(t *testing.T) {
	fd := &fakeDiagnose{result: goodServer(), gate: make(chan struct{})}
	a, _, tt, drain, _ := setupApp(t, mode.DefaultSettings(), mode.Remote, fd.deps)
	a.su.serverAddr = "http://nas:18570"
	before := restOn(tt, "Test connection")
	if err := tt.Click("Test connection"); err != nil {
		t.Fatal(err)
	}
	if d := snapFrame(tt).differs(before); d != "" {
		t.Errorf("the click alone changed the screen: %s", d)
	}
	tt.Advance(statusDelay + time.Millisecond)
	if !tt.HasText("Testing") || tt.HasText("Apple ID") {
		t.Errorf("a slow first test: %q", tt.Texts())
	}
	close(fd.gate)
	drain()
	if tt.HasText("Testing") || !tt.HasText("Apple ID") {
		t.Errorf("after the answer: %q", tt.Texts())
	}
}

func TestWrapperAddressTestKeepsStartWhileItRuns(t *testing.T) {
	a, act, tt, _, _ := setupApp(t, mode.DefaultSettings(), mode.Standalone)
	if err := tt.Click("Another machine"); err != nil {
		t.Fatal(err)
	}
	a.Setup.FFmpeg = setup.Tool{State: setup.Ready, Path: "/opt/homebrew/bin/ffmpeg", Source: "system"}
	a.su.wrapperAddr = "http://nas:30020"
	checks := []mode.Check{{OK: true, Text: "Reached", Note: "wrapper-lite 1.0"}, {OK: true, Text: "someone@icloud.com"}}
	answer := func() { act.wrapperDone(checks) }
	tt.Frame()
	noFlash(t, tt, "Test connection", answer)
	if err := tt.Click("Start"); err != nil || act.applied == nil {
		t.Fatalf("Start after a good test: %v, applied %+v", err, act.applied)
	}
	act.applied = nil
	// Testing again leaves Start, the account row and the footer as they
	// were.
	before, _, after := noFlash(t, tt, "Test connection", answer)
	if d := before.differs(after); d != "" {
		t.Errorf("an unchanged answer changed the screen: %s", d)
	}
}

func TestSignInKeepsTheFormWhileTheAnswerIsOnItsWay(t *testing.T) {
	serving := func() *client.WrapperStatus {
		return &client.WrapperStatus{Ready: true, Regions: []string{}, Accounts: []string{}, LoginSupported: true,
			Managed: &client.ManagedWrapper{Mode: "serving", Accel: "kvm"}}
	}
	t.Run("Settings", func(t *testing.T) {
		a, run := liveApp(t, remoteSettings())
		a.GoSettings("account")
		sv := &a.Store.Server
		sv.Read, sv.Wrapper = time.Now(), serving()
		a.set.login.user, a.set.login.pass = "someone@example.com", "not-a-real-password"
		tt := liveTester(a)
		noFlash(t, tt, "Log in", func() { run(func() bool { return !a.Store.Login.Busy }) })
		if !tt.HasText("Apple did not accept") {
			t.Errorf("the refusal is not shown: %q", tt.Texts())
		}
	})
	t.Run("Setup", func(t *testing.T) {
		s := mode.DefaultSettings()
		s.Mode = mode.Standalone
		a, run := liveApp(t, s)
		a.OpenSetup(mode.Standalone)
		sv := &a.Store.Server
		sv.Read, sv.Wrapper = time.Now(), serving()
		a.su.login.user, a.su.login.pass = "someone@example.com", "not-a-real-password"
		tt := liveTester(a)
		noFlash(t, tt, "Log in", func() { run(func() bool { return !a.Store.Login.Busy }) })
		if !tt.HasText("Apple did not accept") {
			t.Errorf("the refusal is not shown: %q", tt.Texts())
		}
	})
}

func TestStartingADownloadKeepsTheButtonUntilItIsSlow(t *testing.T) {
	t.Run("Home", func(t *testing.T) {
		a, run := liveApp(t, remoteSettings())
		a.Screen = ScreenHome
		s := a.Store
		s.SetOmniText("https://music.apple.com/cn/album/x/1858184006")
		l := s.Omni.Links[0]
		l.State = store.LinkReady
		l.Lookup = client.Lookup{Type: "album", ID: "1858184006", Title: "Test Album", ArtistName: "Some Artist", TrackCount: 5, Storefront: "cn"}
		tt := liveTester(a)
		noFlash(t, tt, "Download 1 album", func() { run(func() bool { return !s.Omni.Submitting }) })
	})
	t.Run("a search hit", func(t *testing.T) {
		a, run := liveApp(t, remoteSettings())
		a.Screen = ScreenHome
		s := a.Store
		s.SetOmniText("yoru")
		tt := liveTester(a)
		run(func() bool { return s.Search.Status == store.SearchReady })
		tt.Frame()
		noFlash(t, tt, "Download Elma", func() { run(func() bool { return len(s.Search.Starting) == 0 }) })
	})
}

func TestReloadingTheConfigKeepsTheButtonUntilItIsSlow(t *testing.T) {
	a, run := liveApp(t, remoteSettings())
	a.GoSettings("server")
	sv := &a.Store.Server
	sv.Read = time.Now()
	a.Store.Config.Values = map[string]any{}
	tt := liveTester(a)
	noFlash(t, tt, "Reload", func() { run(func() bool { return !a.Store.Config.Reloading }) })
}

// A request that is slow says so, once statusDelay has passed and not
// before; the fake backend's answers wait in the queue meanwhile.
func TestWaitingStatesShowOnlyOnceTheRequestIsSlow(t *testing.T) {
	serving := &client.WrapperStatus{Ready: true, Regions: []string{}, Accounts: []string{}, LoginSupported: true,
		Managed: &client.ManagedWrapper{Mode: "serving", Accel: "kvm"}}
	cases := []struct {
		name, click, word string
		stage             func(t *testing.T) (*ui.Tester, func(until func() bool), func() bool)
	}{
		{"sign in", "Log in", "Logging in…", func(t *testing.T) (*ui.Tester, func(func() bool), func() bool) {
			a, run := liveApp(t, remoteSettings())
			a.GoSettings("account")
			a.Store.Server.Read, a.Store.Server.Wrapper = time.Now(), serving
			a.set.login.user, a.set.login.pass = "someone@example.com", "not-a-real-password"
			return liveTester(a), run, func() bool { return !a.Store.Login.Busy }
		}},
		{"reload", "Reload", "Reloading…", func(t *testing.T) (*ui.Tester, func(func() bool), func() bool) {
			a, run := liveApp(t, remoteSettings())
			a.GoSettings("server")
			a.Store.Server.Read = time.Now()
			a.Store.Config.Values = map[string]any{}
			return liveTester(a), run, func() bool { return !a.Store.Config.Reloading }
		}},
		{"download", "Download 1 album", "Starting…", func(t *testing.T) (*ui.Tester, func(func() bool), func() bool) {
			a, run := liveApp(t, remoteSettings())
			a.Screen = ScreenHome
			a.Store.SetOmniText("https://music.apple.com/cn/album/x/1858184006")
			l := a.Store.Omni.Links[0]
			l.State, l.Lookup = store.LinkReady, client.Lookup{Type: "album", ID: "1858184006", Title: "Test Album", TrackCount: 5, Storefront: "cn"}
			return liveTester(a), run, func() bool { return !a.Store.Omni.Submitting }
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tt, run, done := tc.stage(t)
			before := restOn(tt, tc.click)
			if err := tt.Click(tc.click); err != nil {
				t.Fatal(err)
			}
			tt.Advance(statusDelay - time.Millisecond)
			if d := snapFrame(tt).layoutDiffers(before); d != "" || tt.HasText(tc.word) {
				t.Fatalf("before statusDelay the screen changed (%s)", d)
			}
			tt.Advance(2 * time.Millisecond)
			if !tt.HasText(tc.word) {
				t.Errorf("past statusDelay no %q: %q", tc.word, tt.Texts())
			}
			run(done)
			tt.Frame()
			if tt.HasText(tc.word) {
				t.Errorf("after the answer still %q", tc.word)
			}
		})
	}
}
