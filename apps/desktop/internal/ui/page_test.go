package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// demoAlbum is the demo catalog's "Salt & Static", which it has no download
// of, as search finds it.
var demoAlbum = client.SearchHit{Type: "album", ID: "920004", Name: "Salt & Static", ArtistName: "Nightjar Coast",
	URL: "https://music.apple.com/us/album/salt-static/920004", TrackCount: 9}

// jobOf is the demo's download of album id in status, or nil.
func jobOf(s *store.Store, album string, status domain.JobStatus) *store.Job {
	for _, j := range s.Jobs {
		if j.Status == status && j.Input != "" && j.Input[len(j.Input)-len(album):] == album {
			return j
		}
	}
	return nil
}

func TestADownloadStartedFromAnAlbumTakesItsPage(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.OpenAlbum(demoAlbum)
	run(func() bool { return s.Search.Album.Status == store.SearchReady })
	tt := stillTester(a.View, 1280, 800)
	for _, want := range []string{"RELEASED", "GENRE", "2023", "Harbor Lights"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on the album's page: %q", want, tt.Texts())
		}
	}
	if tt.HasText("ACTIVITY") || tt.HasText("Delete") {
		t.Errorf("an album without a download shows a download's parts: %q", tt.Texts())
	}
	if err := tt.Click("Download Salt & Static"); err != nil {
		t.Fatal(err)
	}
	// The songs stay through the start.
	run(func() bool {
		tt.Frame()
		if !tt.HasText("Harbor Lights") {
			t.Fatalf("the songs left as the download started: %q", tt.Texts())
		}
		return s.Detail() != nil
	})
	tt.Frame()
	if j := s.Detail(); j.Status.IsTerminal() || a.Screen != ScreenAlbum {
		t.Fatalf("the page shows %v on screen %v", j.Status, a.Screen)
	}
	for _, want := range []string{"SIZE", "SPEED", "LEFT", "TRACKS", "ACTIVITY", "Pause", "Harbor Lights", "ALBUM"} {
		if !tt.HasText(want) {
			t.Errorf("no %q once the album downloads: %q", want, tt.Texts())
		}
	}
	if _, ok := tt.Find("Download Salt & Static"); ok || tt.HasText("RELEASED") {
		t.Errorf("the album's figures or its download stay beside the download's: %q", tt.Texts())
	}
	if a.backLabel() != "Home" || a.navScreen() != ScreenHome {
		t.Errorf("opened from home, Back says %q", a.backLabel())
	}
}

func TestAFailedAlbumDownloadOpensOnItsAlbum(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenQueue)
	failed := jobOf(s, "920002", domain.JobFailed)
	if failed == nil {
		t.Fatal("the demo has no failed download of Tidal Rooms")
	}
	a.OpenJob(failed.ID)
	run(func() bool { return s.Search.Album.Status == store.SearchReady && len(s.Detail().Items) > 0 })
	tt := stillTester(a.View, 1280, 800)
	for _, want := range []string{"Tidal Rooms", "Seaglass", "Retry", "Delete", "ACTIVITY", "Failed", "Saved"} {
		if !tt.HasText(want) {
			t.Errorf("no %q on a failed download's page: %q", want, tt.Texts())
		}
	}
	if tt.HasText("Download again") || tt.HasText("RELEASED") {
		t.Errorf("a failed download's page offers %q", tt.Texts())
	}
	// Its two failed tracks are not on disk: the tracks figure says 8 of 10,
	// and the bar says how many finished and failed.
	if !tt.HasText("8 / 10") || !tt.HasText("8 / 10 finished · 2 failed") {
		t.Errorf("a failed download's tracks figure says %q", tt.Texts())
	}
	if a.backLabel() != "Queue" {
		t.Errorf("from the queue, Back says %q", a.backLabel())
	}
}

func TestAnAlbumDownloadedAgainShowsTheNewDownload(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	a.Go(ScreenQueue)
	done := jobOf(s, "920005", domain.JobCompleted)
	if done == nil {
		t.Fatal("the demo has no finished download of Orchard Lights")
	}
	a.OpenJob(done.ID)
	tt := stillTester(a.View, 1280, 800)
	if !tt.HasText("Download again") || !tt.HasText("REGION") {
		t.Errorf("a finished download's page: %q", tt.Texts())
	}
	s.SubmitHit(s.Search.Album.Hit, nil)
	// The page follows its album's downloads as it builds.
	run(func() bool { a.followAlbumJob(); return s.DetailID != done.ID })
	again := s.Detail()
	if again == nil || again.Status.IsTerminal() {
		t.Fatalf("the page moved to %+v", again)
	}
	// Opened from the queue, the earlier download stays on its page beside
	// the one that runs.
	a.Back()
	a.OpenJob(done.ID)
	tt.Frame()
	if s.DetailID != done.ID {
		t.Errorf("the finished download's page moved to %q", s.DetailID)
	}
	// From search, the album shows the download that runs.
	a.Back()
	a.OpenAlbum(s.Search.Album.Hit)
	if s.DetailID != again.ID {
		t.Errorf("from search, the album shows download %q, want the running %q", s.DetailID, again.ID)
	}
}

func TestAnAlbumReadThatFailsLeavesTheDownloadsTracks(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Settings.HideSidebar = true
	j := detailFixture(time.Now())
	j.Input = "https://music.apple.com/us/album/midnight-archive/1700000001"
	j.ResourceKind, j.ResourceID = domain.ResourceAlbum, "1700000001"
	for i := range j.Items {
		j.Items[i].AdamID = "t" + j.Items[i].ID
	}
	j = openFixture(a, j)
	p := &a.Store.Search.Album
	if p.ID != "1700000001" || p.Status != store.SearchFailed {
		t.Fatalf("the album page holds %q in %v", p.ID, p.Status)
	}
	tt := stillTester(detailView(a), 1280, 800)
	if tt.HasText(p.Err) || !tt.HasText("Track Number 3") || !tt.HasText("Saved") {
		t.Errorf("with the album unread, the page shows %q", tt.Texts())
	}
	// Read later, the album's songs take the rows, in the download's states.
	p.Status, p.Album = store.SearchReady, client.Album{ID: "1700000001", Title: "Midnight Archive", ArtistName: "The Long Signal"}
	for _, it := range j.Items {
		p.Album.Tracks = append(p.Album.Tracks, client.AlbumTrack{ID: it.AdamID, TrackNumber: it.Index, DiscNumber: 1, Name: "Song " + it.Title, DurationMS: 200_000})
	}
	tt.Frame()
	for _, want := range []string{"Song Track Number 3", "Saved", "Failed", "3:20"} {
		if !tt.HasText(want) {
			t.Errorf("no %q once the album is read: %q", want, tt.Texts())
		}
	}
}

func TestBackToTheQueueKeepsItsTabAndPlace(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Screen = ScreenQueue
	now := time.Now()
	var jobs []domain.Job
	for i := range 40 {
		done := now.Add(-time.Duration(i) * time.Minute)
		jobs = append(jobs, domain.Job{ID: string(rune('a'+i%26)) + string(rune('a'+i/26)), Type: "album", Status: domain.JobCompleted,
			Title: "Album " + string(rune('A'+i%26)) + string(rune('A'+i/26)), ArtistName: "Artist", TotalItems: 5, CreatedAt: done, FinishedAt: &done})
	}
	a.Store.ApplySnapshot(client.DownloadList{StatusCounts: domain.JobStatusCounts{Completed: 40, Total: 40}, Downloads: jobs})
	a.Store.SeedQueue(nil)
	a.Tab = int(store.TabDone)
	tt := ui.NewTester(a.View, 1280, 800)
	// Over the list, under the hero and the covers.
	tt.Scroll(700, 720, 0, 600)
	tt.Frame()
	first, _ := a.queue.Visible()
	if first == 0 {
		t.Fatalf("the queue did not scroll: %q", tt.Texts())
	}
	a.OpenJob("ka")
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if again, _ := a.queue.Visible(); a.Screen != ScreenQueue || a.Tab != int(store.TabDone) || again != first {
		t.Errorf("back on screen %v, tab %v, first row %d: want the queue on Done from row %d", a.Screen, a.Tab, again, first)
	}
}

func TestRowsWithAndWithoutALineKeepOneRhythm(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.OpenAlbum(client.SearchHit{Type: "album", ID: "1", Name: "Split", ArtistName: "One"})
	p := &a.Store.Search.Album
	p.Status, p.Album = store.SearchReady, client.Album{ID: "1", Title: "Split", ArtistName: "One", Tracks: []client.AlbumTrack{
		{ID: "a", TrackNumber: 1, Name: "Alone", DurationMS: 61_000},
		{ID: "b", TrackNumber: 2, Name: "Together", ArtistName: "One & Two", DurationMS: 62_000},
		{ID: "c", TrackNumber: 3, Name: "Alone Again", DurationMS: 63_000},
		{ID: "d", TrackNumber: 4, Name: "Loud", DurationMS: 64_000, ContentRating: "explicit"},
	}}
	tt := ui.NewTester(a.View, 1280, 800)
	find := func(text string) ui.Rect {
		r, ok := tt.Find(text)
		if !ok {
			t.Fatalf("no %q in %q", text, tt.Texts())
		}
		return r
	}
	find("One & Two")
	// The lengths sit in the middle of their rows.
	l1, l2, l3, l4 := find("1:01"), find("1:02"), find("1:03"), find("1:04")
	if l2.Y-l1.Y != l3.Y-l2.Y || l4.Y-l3.Y != l3.Y-l2.Y {
		t.Errorf("rows %v, %v and %v tall: with a line under the title, without, and explicit", l2.Y-l1.Y, l3.Y-l2.Y, l4.Y-l3.Y)
	}
	title := find("Alone")
	if d := (title.Y + title.H/2) - (l1.Y + l1.H/2); d < -1 || d > 1 {
		t.Errorf("a title without a line is %v off its row's middle", d)
	}
}

func TestARunningDownloadsPageShowsItsWholeLog(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	run(func() bool { return s.Loaded })
	j := jobOf(s, "920001", domain.JobRunning)
	// The store follows every running job from the time it saw it.
	if !s.Following(j.ID) {
		t.Fatal("the store does not follow a running job")
	}
	a.OpenJob(j.ID)
	run(func() bool { return len(s.Detail().Activity) > 0 && len(s.Detail().Items) > 0 })
	seen := map[string]bool{}
	for _, e := range s.Detail().Activity {
		if key := e.At.String() + e.Text; seen[key] {
			t.Errorf("%q twice in the log", e.Text)
		} else {
			seen[key] = true
		}
	}
}

func TestARunningDownloadsPageOpensWithItsColumnsInPlace(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	a.Settings.HideSidebar = true
	now := time.Now()
	// Waiting tracks: no stripes ask for frames, so the page shows its
	// columns' own motion alone.
	j := &store.Job{Job: domain.Job{ID: "run", Type: "album", Status: domain.JobRunning, Title: "Waiting Room", ArtistName: "Someone",
		TotalItems: 3, CreatedAt: now, StartedAt: &now}}
	for i := 1; i <= 3; i++ {
		j.Items = append(j.Items, domain.JobItem{ID: fmt.Sprint("w", i), Index: i, Title: fmt.Sprint("Song ", i), Status: domain.ItemQueued, DurationMS: 200_000 + i*1000})
	}
	openFixture(a, j)
	tt := ui.NewTester(detailView(a), 1280, 800)
	first, ok := tt.Find("3:21")
	if !ok {
		t.Fatalf("no length of the first song: %q", tt.Texts())
	}
	tt.Advance(time.Second)
	tt.Frame()
	if later, _ := tt.Find("3:21"); later.X != first.X {
		t.Errorf("the lengths moved from %v to %v as the page opened", first.X, later.X)
	}
}

// A finished download's page says how long it took where a running one's
// says how fast it goes.
func TestAFinishedDownloadsPageSaysHowLongItTook(t *testing.T) {
	a, run := demoApp(t)
	s := a.Store
	var done *store.Job
	run(func() bool {
		for _, j := range s.Jobs {
			if j.Status == domain.JobCompleted && j.StartedAt != nil && j.FinishedAt != nil {
				done = j
				return true
			}
		}
		return false
	})
	a.OpenJob(done.ID)
	tt := stillTester(a.View, 1280, 800)
	took := done.FinishedAt.Sub(*done.StartedAt).Round(time.Second)
	want := fmt.Sprintf("%d:%02d", int(took.Minutes()), int(took.Seconds())%60)
	if !tt.HasText("TOTAL TIME") || !tt.HasText(want) {
		t.Errorf("no total time %s on a finished download's page: %q", want, tt.Texts())
	}
	if tt.HasText("AVG SPEED") {
		t.Errorf("a finished download's page still shows its average speed")
	}
}

// An album not downloaded yet plays the motion cover it is read with, in
// that cover's own palette; a download of it with a motion cover of its
// own plays that one.
func TestAnAlbumNotDownloadedPlaysItsMotionCover(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	p := &a.Store.Search.Album
	p.ID, p.Status = "1", store.SearchReady
	p.Album = client.Album{ID: "1", Title: "Album", ArtworkBgColor: "f4f0e8", ArtworkTextColor1: "101010",
		MotionArtworkURL: "https://mvod.example/album.m3u8", MotionArtworkBgColor: "102030", MotionArtworkTextColor1: "f0e0d0"}
	master, pal := a.pageMotion()
	if master != "https://mvod.example/album.m3u8" || !pal.OK || pal.Bg != ui.Hex("102030") || pal.Text1 != ui.Hex("f0e0d0") {
		t.Errorf("the album's page plays %q in %+v, want its motion cover in that cover's palette", master, pal)
	}

	a.Store.ApplySnapshot(client.DownloadList{StatusCounts: domain.JobStatusCounts{Completed: 1, Total: 1}, Downloads: []domain.Job{{
		ID: "j", Type: "album", Status: domain.JobCompleted, Title: "Album",
		MotionArtworkURL: "https://mvod.example/job.m3u8", MotionArtworkBgColor: "302010", MotionArtworkTextColor1: "e0f0d0"}}})
	a.Store.OpenDetail("j")
	if master, pal := a.pageMotion(); master != "https://mvod.example/job.m3u8" || pal.Bg != ui.Hex("302010") {
		t.Errorf("with its download open the page plays %q in %+v, want the download's", master, pal)
	}
}
