package ui

import (
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/filmcheck"
	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/demo"
	"amdl/internal/domain"
)

// refresh is one frame of a 60 Hz display. It lives in a file the race
// build compiles, as the scenes do.
const refresh = time.Second / 60

// The scene set: the screens and transitions a look or motion round can
// break, filmed at 60 Hz in the maintainer's window and checked frame by
// frame by package filmcheck. tools/lookround runs it, a theme and a scene
// to a process; each scene writes its verdict, thumbnails and the full
// frames it found wrong under $SCENE_OUT, and
// apps/desktop/tools/framecheck sheet turns them into one contact sheet
// and summary.

// sceneW and sceneH are the maintainer's window: the album page 957 wide
// beside the 232 of the sidebar (sidebarWidth), as on their Mac.
const sceneW, sceneH = 1189, 838

// sceneThemes are filmed one by one; a theme is a subtest, so a shard can
// take one.
var sceneThemes = []string{"light", "dark"}

// scenes are run in this order by TestScenes; a name is a subtest.
var scenes = []struct {
	name string
	// rename are the demo's names this scene shows as others.
	rename map[string]string
	run    func(r *sceneRig)
}{
	{name: "head-none", run: sceneHeadNone},
	{name: "head-partial", run: sceneHeadPartial},
	{name: "head-complete", run: sceneHeadComplete},
	{name: "head-playlist", run: sceneHeadPlaylist},
	{name: "head-queued", run: sceneHeadQueued},
	{name: "head-downloading", run: sceneHeadDownloading, rename: longTitle},
	{name: "head-failed", run: sceneHeadFailed},
	{name: "artist-album", run: sceneArtistAlbum},
	{name: "second-album", run: sceneSecondAlbum},
	{name: "first-search", run: sceneFirstSearch},
	{name: "switch-sidebar", run: sceneSwitchSidebar},
	{name: "switch-queue-tabs", run: sceneSwitchQueueTabs},
	{name: "switch-search-tabs", run: sceneSwitchSearchTabs},
	{name: "switch-library-kinds", run: sceneSwitchLibraryKinds},
}

// TestScenes films every scene in both themes when $SCENE_OUT names where
// the results go. With $SCENE_LIST it only writes the subtests' names
// there, one a line, for a runner to give each its own process. With
// SCENE_KEEP=all it also keeps every frame, full size, as all-<frame>.png,
// to measure a finding on.
func TestScenes(t *testing.T) {
	if p := os.Getenv("SCENE_LIST"); p != "" {
		var b strings.Builder
		for _, sc := range scenes {
			for _, theme := range sceneThemes {
				fmt.Fprintf(&b, "%s/%s\n", sc.name, theme)
			}
		}
		if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	out := os.Getenv("SCENE_OUT")
	if out == "" {
		t.Skip("SCENE_OUT names where the scenes' results go")
	}
	if k := os.Getenv("SCENE_KEEP"); k != "" && k != "all" {
		t.Fatalf("SCENE_KEEP is all or unset, not %q", k)
	}
	if err := RegisterFonts(); err != nil {
		t.Fatal(err)
	}
	for _, sc := range scenes {
		t.Run(sc.name, func(t *testing.T) {
			for _, theme := range sceneThemes {
				t.Run(theme, func(t *testing.T) {
					r := newSceneRig(t, filepath.Join(out, sc.name+"."+theme), theme, sc.rename)
					start := time.Now()
					defer func() { r.write(sc.name, theme, time.Since(start)) }()
					sc.run(r)
				})
			}
		})
	}
}

// sceneRig is the demo app in one theme at the maintainer's size, whose
// backend answers wait until the rig hands them to the UI thread, before
// each frame, as the maintainer's local backend answers within one.
type sceneRig struct {
	t      *testing.T
	a      *App
	tt     *ui.Tester
	queue  chan func()
	net    *sceneNet
	dir    string
	segs   []*sceneSeg
	frames int
	notes  []string
}

// sceneSeg is one film: an input and the motion it starts.
type sceneSeg struct {
	Name     string              `json:"name"`
	First    int                 `json:"first"`
	Frames   int                 `json:"frames"`
	Findings []filmcheck.Finding `json:"findings"`
	Cells    []sceneCell         `json:"cells"`
	Settled  string              `json:"settled,omitempty"`
	film     *filmcheck.Film
	thumbs   []*image.RGBA
	texts    [][]string
	// full holds the segment's frames until its findings are known.
	full []*image.RGBA
}

type sceneCell struct {
	File    string `json:"file"`
	Caption string `json:"caption"`
	Bad     bool   `json:"bad"`
}

// sceneResult is result.json, which framecheck sheet reads.
type sceneResult struct {
	Scene    string      `json:"scene"`
	Theme    string      `json:"theme"`
	Failed   bool        `json:"failed"`
	Error    string      `json:"error,omitempty"`
	Seconds  float64     `json:"seconds"`
	Segments []*sceneSeg `json:"segments"`
	Notes    []string    `json:"notes,omitempty"`
}

func newSceneRig(t *testing.T, dir, theme string, rename map[string]string) *sceneRig {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestApp(remoteSettings())
	a.PageMotion, a.Theme = true, theme
	queue := make(chan func(), 4096)
	post := func(fn func()) { queue <- fn }
	net := &sceneNet{rt: demo.New(time.Now).Transport(), rename: rename}
	a.Store = store.New(post, nil)
	a.Art = artwork.NewWith(post, net)
	c, err := client.NewWith(demo.Address, net)
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	r := &sceneRig{t: t, a: a, queue: queue, net: net, dir: dir}
	r.tt = ui.NewTester(a.View, sceneW, sceneH)
	r.tt.SetScale(1)
	r.until("the demo loaded", func() bool { return a.Store.Loaded })
	return r
}

// longTitle names the queue hero's album with a title that wraps in the
// hero and not on the album page, at the maintainer's window size: its
// words move between lines as the page flies, both ways.
var longTitle = map[string]string{"Low Tide Signals": "First Love (Remastered 2014)"}

// pump hands the answers that arrived to the UI thread and reports
// whether there were any: once every answer is read and nothing more has
// been posted for 2 ms, or after 300 ms.
func (r *sceneRig) pump() bool {
	deadline := time.Now().Add(300 * time.Millisecond)
	for r.net.inflight.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Microsecond)
	}
	for last := -1; len(r.queue) != last && time.Now().Before(deadline); {
		last = len(r.queue)
		time.Sleep(2 * time.Millisecond)
	}
	n := 0
	for {
		select {
		case fn := <-r.queue:
			fn()
			n++
		default:
			return n > 0
		}
	}
}

// until runs the app, unfilmed, until ok, for at most 20 seconds.
func (r *sceneRig) until(what string, ok func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			r.t.Fatalf("%s: never happened; the window shows %q", what, r.tt.Texts())
		}
		if r.pump() {
			r.tt.Frame()
		}
		r.tt.Advance(50 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	r.tt.Frame()
}

// settle runs the app, unfilmed, for d of its clock, answers arriving as
// they come, and covers loaded.
func (r *sceneRig) settle(d time.Duration) {
	end := r.tt.Now().Add(d)
	for r.tt.Now().Before(end) {
		if r.pump() {
			r.tt.Frame()
		}
		r.tt.Advance(50 * time.Millisecond)
		time.Sleep(time.Millisecond)
	}
	r.pump()
	r.tt.Frame()
}

// film records the frame before act, then a frame per refresh: at least
// min of them, and while the page flies, until it has rested 8.
func (r *sceneRig) film(name string, min int, act func()) *sceneSeg {
	r.t.Helper()
	seg := &sceneSeg{Name: name, First: r.frames, film: &filmcheck.Film{Content: r.content()}}
	r.segs = append(r.segs, seg)
	r.shoot(seg)
	act()
	rest := 0
	for i := 1; i < 150; i++ {
		pumped := r.pump()
		r.tt.Advance(refresh)
		if pumped {
			r.tt.Frame()
		}
		r.shoot(seg)
		if r.a.fly.moving {
			rest = 0
		} else {
			rest++
		}
		if i >= min && rest >= 8 {
			break
		}
	}
	seg.Frames = seg.film.Frames()
	seg.Findings = seg.film.Finish()
	r.keepBad(seg)
	seg.full = nil
	return seg
}

// content is the window's right of the sidebar, when the sidebar shows.
func (r *sceneRig) content() image.Rectangle {
	if r.a.compact {
		return image.Rectangle{}
	}
	return image.Rect(sidebarWidth, 0, sceneW, sceneH)
}

func (r *sceneRig) shoot(seg *sceneSeg) {
	img := r.tt.Image()
	texts := r.tt.Texts()
	seg.film.Add(img, texts, r.a.fly.moving)
	seg.thumbs = append(seg.thumbs, filmcheck.Shrink(img, 4))
	seg.texts = append(seg.texts, texts)
	full := image.NewRGBA(img.Bounds())
	copy(full.Pix, img.Pix)
	seg.full = append(seg.full, full)
	r.frames++
}

// keepBad writes the full frames the segment's findings name, and one on
// either side; with SCENE_KEEP=all, every frame besides.
func (r *sceneRig) keepBad(seg *sceneSeg) {
	if os.Getenv("SCENE_KEEP") == "all" {
		for i, img := range seg.full {
			r.save(fmt.Sprintf("all-%03d.png", seg.First+i), img)
		}
	}
	saved := map[int]bool{}
	for _, f := range seg.Findings {
		for i := max(f.Frame-1, 0); i <= f.Frame+f.Frames && i < len(seg.full); i++ {
			if !saved[i] {
				saved[i] = true
				r.save(fmt.Sprintf("bad-%03d.png", seg.First+i), seg.full[i])
			}
		}
	}
}

// still saves the window as it shows now, full size, as the segment's
// settled frame: a JPEG, to copy back over a slow link.
func (r *sceneRig) still(seg *sceneSeg) {
	seg.Settled = fmt.Sprintf("settled-%02d-%s.jpg", len(r.segs)-1, seg.Name)
	r.save(seg.Settled, r.tt.Image())
}

// save writes img as name, a PNG or, by its extension, a JPEG.
func (r *sceneRig) save(name string, img image.Image) {
	f, err := os.Create(filepath.Join(r.dir, name))
	if err != nil {
		r.t.Error(err)
		return
	}
	defer f.Close()
	if strings.HasSuffix(name, ".jpg") {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(f, img)
	}
	if err != nil {
		r.t.Error(err)
	}
}

// note is a line of the scene's result, for what is not a finding.
func (r *sceneRig) note(format string, args ...any) {
	r.notes = append(r.notes, fmt.Sprintf(format, args...))
}

// write saves result.json and the thumbnails the sheet shows: of each
// segment, the frame before, four through it, the last and every frame a
// finding names.
func (r *sceneRig) write(scene, theme string, took time.Duration) {
	res := sceneResult{Scene: scene, Theme: theme, Seconds: took.Seconds(), Segments: r.segs, Notes: r.notes}
	if r.t.Failed() {
		res.Failed, res.Error = true, "the scene did not run to its end; see the test's log"
	}
	for si, seg := range r.segs {
		n := len(seg.thumbs)
		if n == 0 {
			continue
		}
		pick := map[int]bool{0: true, n - 1: true, n / 4: true, n / 2: true, 3 * n / 4: true}
		bad := map[int]bool{}
		for _, f := range seg.Findings {
			for i := f.Frame; i < f.Frame+max(f.Frames, 1) && i < n; i++ {
				pick[i], bad[i] = true, true
			}
		}
		var idx []int
		for i := range pick {
			idx = append(idx, i)
		}
		slices.Sort(idx)
		for _, i := range idx {
			file := fmt.Sprintf("cell-%02d-%03d.png", si, i)
			r.save(file, seg.thumbs[i])
			seg.Cells = append(seg.Cells, sceneCell{File: file, Caption: fmt.Sprintf("%s %d", seg.Name, i), Bad: bad[i]})
		}
		if len(seg.Findings) > 0 {
			res.Failed = true
		}
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, "result.json"), b, 0o644); err != nil {
		r.t.Fatal(err)
	}
	for _, seg := range r.segs {
		for _, f := range seg.Findings {
			r.t.Logf("%s/%s: %s", seg.Name, theme, f)
		}
	}
}

// search types words into home's field and waits for the results and
// their covers.
func (r *sceneRig) search(words string) {
	r.t.Helper()
	a, s := r.a, r.a.Store
	a.Go(ScreenHome)
	r.settle(300 * time.Millisecond)
	r.tt.Type(words)
	r.until("the results for "+words, func() bool {
		return s.Search.Status == store.SearchReady && s.Search.Result != nil && s.Search.Term == words
	})
	r.until("the results' covers", func() bool {
		for _, h := range s.Search.ShownHits() {
			if a.Art.Get(h.ArtworkURL, 320) == nil {
				return false
			}
		}
		return true
	})
	r.settle(600 * time.Millisecond)
}

// queue shows the queue on tab, its rows and hero read.
func (r *sceneRig) queueOn(tab store.Tab) {
	r.t.Helper()
	r.a.Go(ScreenQueue)
	r.a.Tab = int(tab)
	r.until("the queue's list", func() bool { return r.a.Store.Queue.List(tab).Status == store.SearchReady })
	r.settle(800 * time.Millisecond)
}

// open films a click on name, the page landing and settling, and checks
// it shows the state want names.
func (r *sceneRig) open(name, want string) *sceneSeg {
	r.t.Helper()
	return r.openWith(func() { r.click(name) }, name, want)
}

// openWith is open, the page opened by act.
func (r *sceneRig) openWith(act func(), name, want string) *sceneSeg {
	r.t.Helper()
	seg := r.film("open", 30, act)
	r.settle(800 * time.Millisecond)
	if r.a.Screen != ScreenAlbum {
		r.t.Fatalf("a click on %q left screen %v", name, r.a.Screen)
	}
	if got := r.headState(); got != want {
		r.t.Fatalf("the page of %q shows %s, want %s", name, got, want)
	}
	r.still(seg)
	return seg
}

// back films Escape, the page going back, and settling.
func (r *sceneRig) back() {
	r.t.Helper()
	seg := r.film("back", 30, func() { r.tt.Key(0, ui.KeyEscape) })
	r.settle(600 * time.Millisecond)
	if r.a.Screen == ScreenAlbum {
		r.t.Fatal("Escape left the album page up")
	}
	r.still(seg)
}

func (r *sceneRig) click(name string) {
	r.t.Helper()
	if err := r.tt.Click(name); err != nil {
		r.t.Fatalf("no %q to click: %v; the window shows %q", name, err, r.tt.Texts())
	}
	// The pointer leaves for the sidebar's empty middle: a row the landed
	// page puts under it would light up at once, which is a hover, not a
	// pop.
	r.tt.Move(sidebarWidth/2, sceneH/2)
}

// stillOf records the window as it rests now as a segment of one frame,
// for a state whose way in the scene cannot take as the maintainer does.
func (r *sceneRig) stillOf(name string) *sceneSeg {
	seg := &sceneSeg{Name: name, First: r.frames, film: &filmcheck.Film{Content: r.content()}}
	r.segs = append(r.segs, seg)
	r.shoot(seg)
	seg.Frames = seg.film.Frames()
	seg.Findings = seg.film.Finish()
	seg.full = nil
	r.still(seg)
	return seg
}

// headState names what the album page's head shows, by the store: the
// states a look round has to keep apart.
func (r *sceneRig) headState() string {
	s := r.a.Store
	p, j := &s.Search.Album, s.Detail()
	if j != nil {
		switch j.Status {
		case domain.JobQueued:
			return "queued"
		case domain.JobRunning:
			return "downloading"
		case domain.JobFailed, domain.JobCancelled:
			return "failed"
		case domain.JobCompleted:
			return "complete"
		}
		return string(j.Status)
	}
	if p.Kind() == domain.ResourcePlaylist {
		return "playlist"
	}
	switch mark, _ := s.HitMark(p.Hit); mark {
	case store.MarkDone:
		return "complete"
	case store.MarkPartial:
		return "partial"
	case store.MarkNone:
		return "none"
	default:
		return fmt.Sprintf("mark %d", mark)
	}
}

func sceneHeadNone(r *sceneRig) {
	r.search("salt")
	r.open("Salt & Static", "none")
	r.back()
}

func sceneHeadPartial(r *sceneRig) {
	r.search("late bloom")
	r.open("Late Bloom", "partial")
	r.back()
}

func sceneHeadComplete(r *sceneRig) {
	r.search("orchard lights")
	r.open("Orchard Lights", "complete")
	r.back()
}

func sceneHeadPlaylist(r *sceneRig) {
	r.search("glass and neon")
	r.open("Glass and Neon", "playlist")
	r.back()
}

func sceneHeadQueued(r *sceneRig) {
	r.queueOn(store.TabAll)
	r.open("夏夜信号", "queued")
	r.back()
}

func sceneHeadDownloading(r *sceneRig) {
	r.queueOn(store.TabAll)
	j := r.a.heroJob()
	if j == nil {
		r.t.Fatal("the queue has no hero")
	}
	r.open("Open "+j.Name(), "downloading")
	r.back()
}

// A failed download's own page, as a latest-download hero's details open
// it, which the demo has no hero for: a still. Its queue row opens the
// album, whose record holds the songs it got (head-partial's state).
func sceneHeadFailed(r *sceneRig) {
	r.queueOn(store.TabFailed)
	var failed *store.Job
	for _, j := range r.a.Store.Jobs {
		if j.Status == domain.JobFailed {
			failed = j
		}
	}
	if failed == nil {
		r.t.Fatal("the demo has no failed download")
	}
	r.a.OpenJob(failed.ID)
	r.settle(800 * time.Millisecond)
	if got := r.headState(); got != "failed" {
		r.t.Fatalf("the page of %q shows %s, want failed", failed.Name(), got)
	}
	r.stillOf("page")
}

func sceneArtistAlbum(r *sceneRig) {
	r.search("mira solenne")
	s := r.a.Store
	r.click("Artists")
	r.settle(600 * time.Millisecond)
	r.film("artist", 30, func() { r.click("Mira Solenne") })
	r.until("the artist's albums", func() bool { return s.Search.Artist.Status == store.SearchReady })
	r.settle(800 * time.Millisecond)
	r.open("Glass Weather", "downloading")
	r.back()
}

// Two finished albums opened one after the other from the queue, the
// feed holding neither's download, so each page reads its figures: no
// frame of the second shows the first's.
func sceneSecondAlbum(r *sceneRig) {
	s := r.a.Store
	// Downloads past the newest the feed lists, before the queue shows.
	s.Jobs = nil
	r.queueOn(store.TabDone)
	under := textSet(r.tt.Texts())
	first := r.open("Orchard Lights", "complete")
	firstTexts := textSet(r.tt.Texts())
	r.back()
	for k := range textSet(r.tt.Texts()) {
		under[k] = true
	}
	second := r.open("海边的耳机", "complete")
	secondTexts := textSet(r.tt.Texts())
	var only []string
	for k := range firstTexts {
		if !secondTexts[k] && !under[k] {
			only = append(only, k)
		}
	}
	slices.Sort(only)
	if len(only) == 0 {
		r.t.Fatal("the two pages show the same texts: nothing to tell them apart")
	}
	r.note("first page's own texts: %q", only)
	_ = first
	for i, texts := range second.texts {
		var stale []string
		for _, x := range texts {
			if slices.Contains(only, strings.TrimSpace(x)) {
				stale = append(stale, x)
			}
		}
		if len(stale) > 0 {
			f := filmcheck.Finding{Kind: "stale", Frame: i, Frames: 1,
				Detail: fmt.Sprintf("the second album's page shows the first's %q", stale)}
			second.Findings = append(second.Findings, f)
		}
	}
	if final := r.tt.Texts(); slices.ContainsFunc(final, func(x string) bool { return slices.Contains(only, strings.TrimSpace(x)) }) {
		second.Findings = append(second.Findings, filmcheck.Finding{Kind: "stale", Frame: len(second.texts) - 1,
			Detail: "the second album's settled page shows the first's texts"})
	}
	r.back()
}

func textSet(texts []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range texts {
		m[strings.TrimSpace(x)] = true
	}
	return m
}

// The first search of a session, typed a key at a time.
func sceneFirstSearch(r *sceneRig) {
	r.a.Go(ScreenHome)
	r.settle(600 * time.Millisecond)
	for i, ch := range "glass" {
		r.film(fmt.Sprintf("type-%d", i), 3, func() { r.tt.Type(string(ch)) })
	}
	seg := r.film("results", 45, func() {})
	r.settle(600 * time.Millisecond)
	if r.a.Store.Search.Status != store.SearchReady {
		r.t.Fatalf("the search is %v after typing", r.a.Store.Search.Status)
	}
	r.still(seg)
}

func sceneSwitchSidebar(r *sceneRig) {
	r.a.Go(ScreenHome)
	r.settle(600 * time.Millisecond)
	for _, name := range []string{"Queue", "Library", "Settings", "Home", "Queue"} {
		seg := r.film("to-"+strings.ToLower(name), 30, func() { r.click(name) })
		r.settle(600 * time.Millisecond)
		r.still(seg)
	}
}

func sceneSwitchQueueTabs(r *sceneRig) {
	r.queueOn(store.TabAll)
	for _, tab := range []string{"Active", "Done", "Failed", "All"} {
		label := ""
		for _, x := range r.tt.Texts() {
			if strings.HasPrefix(x, tab) {
				label = x
				break
			}
		}
		if label == "" {
			r.t.Fatalf("no %s tab in %q", tab, r.tt.Texts())
		}
		seg := r.film("tab-"+strings.ToLower(tab), 30, func() { r.click(label) })
		r.settle(600 * time.Millisecond)
		r.still(seg)
	}
}

func sceneSwitchSearchTabs(r *sceneRig) {
	r.search("glass")
	for _, tab := range []string{"Albums", "Songs", "Playlists", "Artists", "Overview"} {
		seg := r.film("tab-"+strings.ToLower(tab), 30, func() { r.click(tab) })
		r.settle(600 * time.Millisecond)
		r.still(seg)
	}
}

func sceneSwitchLibraryKinds(r *sceneRig) {
	r.a.Go(ScreenLibrary)
	r.until("the Apple library", func() bool { return r.a.Store.Library.AppleStatus == store.SearchReady })
	r.settle(800 * time.Millisecond)
	for _, kind := range []string{"Songs", "Playlists", "Albums"} {
		seg := r.film("kind-"+strings.ToLower(kind), 30, func() { r.click(kind) })
		r.settle(600 * time.Millisecond)
		r.still(seg)
	}
}
