package demo

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"net/http"
	"slices"
	"sort"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// clock is a time that moves only when the test moves it.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newDemo(t *testing.T) (*Backend, *client.Client, *clock) {
	t.Helper()
	clk := &clock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	b := New(clk.now)
	c, err := client.NewWith(Address, b.Transport())
	if err != nil {
		t.Fatal(err)
	}
	c.RetryMin, c.RetryMax = time.Millisecond, 5*time.Millisecond
	return b, c, clk
}

// run moves the backend on by d in Ticks.
func run(b *Backend, clk *clock, d time.Duration) {
	for range int(d / Tick) {
		clk.add(Tick)
		b.Step(Tick)
	}
}

func TestTheQueueStartsWithEveryKindOfJob(t *testing.T) {
	_, c, _ := newDemo(t)
	ctx := context.Background()
	l, err := c.Downloads(ctx, client.ListQuery{Limit: client.SnapshotLimit})
	if err != nil {
		t.Fatal(err)
	}
	want := domain.JobStatusCounts{Queued: 1, Running: 2, Completed: 2, Failed: 1, Cancelled: 1, Total: 7}
	if l.StatusCounts != want || len(l.Downloads) != 7 {
		t.Fatalf("counts %+v, %d jobs", l.StatusCounts, len(l.Downloads))
	}
	if !l.Downloads[0].CreatedAt.After(l.Downloads[6].CreatedAt) {
		t.Error("the list is not newest first")
	}
	for _, j := range l.Downloads {
		if j.Status == domain.JobRunning && (j.Progress == nil || *j.Progress <= 0 || *j.Progress >= 1) {
			t.Errorf("running job %s has progress %v", j.Title, j.Progress)
		}
		if j.Status == domain.JobFailed && j.FailedItems == 0 {
			t.Errorf("failed job %s has no failed track", j.Title)
		}
	}
	if h, err := c.Health(ctx); err != nil || h.Status != "ok" {
		t.Errorf("health %+v, %v", h, err)
	}
	if s, err := c.System(ctx); err != nil || len(s.Storage) == 0 || s.Storage[0].Path != musicDir {
		t.Errorf("system %+v, %v", s, err)
	}
	if w, err := c.WrapperStatus(ctx); err != nil || !w.LoggedIn {
		t.Errorf("wrapper %+v, %v", w, err)
	}
	if l, err := c.ListAccounts(ctx); err != nil || len(l.Accounts) != 1 || !l.Accounts[0].Default {
		t.Errorf("accounts %+v, %v", l, err)
	}
}

func TestTheFeedCarriesProgressAndTheJobItsTracks(t *testing.T) {
	synctest.Test(t, testTheFeedCarriesProgressAndTheJobItsTracks)
}

func testTheFeedCarriesProgressAndTheJobItsTracks(t *testing.T) {
	b, c, clk := newDemo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs := make(chan domain.DownloadFeedMessage, 1024)
	live := make(chan bool, 1)
	go c.FollowFeed(ctx, client.FeedHandlers{
		Message: func(m domain.DownloadFeedMessage) { msgs <- m },
		State: func(s client.ConnState) {
			if s.Phase == client.Live {
				live <- true
			}
		},
	})
	synctest.Wait()
	if !received(live) {
		t.Fatal("the feed never went live")
	}
	l, err := c.Downloads(ctx, client.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var running domain.Job
	for _, j := range l.Downloads {
		if j.Status == domain.JobRunning {
			running = j
			break
		}
	}
	events := make(chan domain.Event, 4096)
	followed := make(chan error, 1)
	go func() {
		followed <- c.FollowJob(ctx, running.ID, client.JobHandlers{Event: func(e domain.Event) { events <- e }})
	}()
	// The job stream subscribes once it has read the job.
	synctest.Wait()
	run(b, clk, 3*time.Minute)
	synctest.Wait()
	select {
	case err := <-followed:
		if err != nil {
			t.Fatalf("follow ended with %v", err)
		}
	default:
		t.Fatal("the job's follow did not end once it finished")
	}
	var progress, completed, finished int
	for len(events) > 0 {
		switch e := <-events; e.Type {
		case domain.EventItemProgress:
			progress++
		case domain.EventItemCompleted:
			completed++
		case domain.EventJobFinished:
			finished++
		}
	}
	if progress == 0 || completed == 0 || finished != 1 {
		t.Errorf("job events: %d progress, %d completed, %d finished", progress, completed, finished)
	}
	// The feed is a stream of its own; synctest.Wait above let it deliver
	// every message.
	var moved, done bool
	for len(msgs) > 0 {
		if m := <-msgs; m.Job != nil && m.Job.ID == running.ID {
			moved = moved || (m.Job.Progress != nil && *m.Job.Progress > *running.Progress)
			done = done || m.Job.Status == domain.JobCompleted
		}
	}
	if !moved || !done {
		t.Errorf("feed: moved %v, finished %v", moved, done)
	}
	if l, _ := c.Downloads(ctx, client.ListQuery{}); l.StatusCounts.Queued != 0 {
		t.Errorf("the waiting job did not start: %+v", l.StatusCounts)
	}
}

func TestSubmitQueuesAJobOnceAndRefusesTheRest(t *testing.T) {
	_, c, _ := newDemo(t)
	ctx := context.Background()
	link := albums[3].url()
	res, err := c.Submit(ctx, []string{link}, nil)
	if err != nil || res.Accepted != 1 || res.Results[0].Job == nil || res.Results[0].Job.Title != albums[3].title {
		t.Fatalf("submit %+v, %v", res, err)
	}
	res, err = c.Submit(ctx, []string{link, "not a link"}, nil)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "no_urls_accepted" || res.Rejected != 2 ||
		res.Results[0].Status != domain.SubmitDuplicateActive || res.Results[1].Status != domain.SubmitInvalid {
		t.Fatalf("second submit %+v, %v", res, err)
	}
	// A song of an album, and a link the catalog does not hold.
	res, err = c.Submit(ctx, []string{albums[0].url() + "?i=" + songID(albums[0], 2), "https://music.apple.com/us/album/x/1440000000"}, nil)
	if err != nil || res.Accepted != 2 || res.Results[0].Job.Type != "song" || res.Results[0].Job.Title != albums[0].tracks[2] {
		t.Fatalf("song submit %+v, %v", res, err)
	}
	if l, err := c.Lookup(ctx, albums[5].url()); err != nil || l.Title != albums[5].title || l.TrackCount != len(albums[5].tracks) {
		t.Errorf("lookup %+v, %v", l, err)
	}
}

func TestCancelAndRetry(t *testing.T) {
	b, c, clk := newDemo(t)
	ctx := context.Background()
	l, _ := c.Downloads(ctx, client.ListQuery{})
	var running, failed string
	for _, j := range l.Downloads {
		switch j.Status {
		case domain.JobRunning:
			running = j.ID
		case domain.JobFailed:
			failed = j.ID
		}
	}
	if err := c.Cancel(ctx, running); err != nil {
		t.Fatal(err)
	}
	if d, _ := c.Download(ctx, running); d.Job.Status != domain.JobCancelled {
		t.Errorf("after cancel: %s", d.Job.Status)
	}
	var apiErr *client.APIError
	if err := c.Cancel(ctx, running); !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
		t.Errorf("cancelling again: %v", err)
	}
	if _, err := c.Retry(ctx, failed); err != nil {
		t.Fatal(err)
	}
	run(b, clk, 4*time.Minute)
	if d, _ := c.Download(ctx, failed); d.Job.Status != domain.JobCompleted || d.Job.FailedItems != 0 {
		t.Errorf("after retry: %s, %d failed", d.Job.Status, d.Job.FailedItems)
	}
}

func TestSearchAlbumAndArtist(t *testing.T) {
	_, c, _ := newDemo(t)
	ctx := context.Background()
	r, err := c.Search(ctx, client.SearchQuery{Term: "glass"})
	if err != nil || r.Albums == nil || len(r.Albums.Items) != 1 || r.Songs == nil || len(r.Songs.Items) == 0 {
		t.Fatalf("search %+v, %v", r, err)
	}
	// Glass Weather is running: its hit's record carries the run.
	if hit := r.Albums.Items[0]; hit.Name != "Glass Weather" || hit.Resource == nil || hit.Resource.ActiveJobID == "" {
		t.Errorf("album hit %+v", hit)
	}
	if r, _ := c.Search(ctx, client.SearchQuery{Term: "夏夜"}); r.Albums == nil || len(r.Albums.Items) != 1 {
		t.Errorf("a Chinese title: %+v", r)
	}
	a, err := c.Album(ctx, albums[2].id, "")
	if err != nil || len(a.Tracks) != len(albums[2].tracks) || a.Tracks[0].TrackNumber != 1 {
		t.Errorf("album %+v, %v", a, err)
	}
	aa, err := c.ArtistAlbums(ctx, artists[1].id, client.PageQuery{})
	if err != nil || aa.Artist.Name != "Nightjar Coast" || len(aa.Albums.Items) != 2 {
		t.Errorf("artist albums %+v, %v", aa, err)
	}
}

func TestASearchWithoutAMatchShowsTheCatalog(t *testing.T) {
	_, c, _ := newDemo(t)
	ctx := context.Background()
	r, err := c.Search(ctx, client.SearchQuery{Term: "yorushika"})
	if err != nil {
		t.Fatal(err)
	}
	for name, g := range map[string]*client.HitGroup{"artists": r.Artists, "albums": r.Albums, "playlists": r.Playlists, "songs": r.Songs} {
		if g == nil || len(g.Items) == 0 {
			t.Errorf("%s: no hits", name)
		}
	}
	if r.Artists != nil && len(r.Artists.Items) != len(artists) {
		t.Errorf("artists: %d hits, want the catalog's %d", len(r.Artists.Items), len(artists))
	}
	// A term that matches something keeps to its matches.
	r, err = c.Search(ctx, client.SearchQuery{Term: "tidal"})
	if err != nil || r.Albums == nil || len(r.Albums.Items) != 1 || r.Albums.Items[0].Name != "Tidal Rooms" || (r.Artists != nil && len(r.Artists.Items) != 0) {
		t.Errorf("tidal: %+v, %v", r, err)
	}
}

func TestCoversAreImagesOfTheSizeAsked(t *testing.T) {
	b, _, _ := newDemo(t)
	req, _ := http.NewRequest(http.MethodGet, Address+"/art/920001/300x300bb.jpg", nil)
	resp, err := b.Transport().RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(&buf)
	if err != nil || img.Bounds().Dx() != 300 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("cover: %v, %v", img, err)
	}
}

func TestSettingsAndSignInChangeOnlyMemory(t *testing.T) {
	_, c, _ := newDemo(t)
	ctx := context.Background()
	if _, err := c.Call(ctx, http.MethodPut, "/api/v1/config", nil, map[string]any{"download": map[string]any{"cover_size": 1200}}); err != nil {
		t.Fatal(err)
	}
	raw, err := c.Call(ctx, http.MethodGet, "/api/v1/config", nil, nil)
	if err != nil || !bytes.Contains(raw, []byte(`"cover_size":1200`)) || !bytes.Contains(raw, []byte(musicDir)) {
		t.Fatalf("config %s, %v", raw, err)
	}
	if _, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/logout", nil, map[string]string{"username": "demo@example.com"}); err != nil {
		t.Fatal(err)
	}
	if w, _ := c.WrapperStatus(ctx); w.LoggedIn {
		t.Error("still signed in after logging out")
	}
	if _, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/login/demo-login/2fa", nil, map[string]string{"two_step_code": "12"}); err == nil {
		t.Error("a two-digit code was taken")
	}
	if _, err := c.Call(ctx, http.MethodPost, "/api/v1/wrapper/login/demo-login/2fa", nil, map[string]string{"two_step_code": "123456"}); err != nil {
		t.Fatal(err)
	}
	if w, _ := c.WrapperStatus(ctx); !w.LoggedIn {
		t.Error("not signed in after the code")
	}
}

// queuedInOrder lists the demo's queued jobs by queue_order.
func queuedInOrder(t *testing.T, c *client.Client) []domain.Job {
	t.Helper()
	l, err := c.Downloads(context.Background(), client.ListQuery{Limit: client.SnapshotLimit})
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Job
	for _, j := range l.Downloads {
		if j.Status == domain.JobQueued {
			if j.QueueOrder == nil {
				t.Fatalf("queued job %s has no queue_order", j.ID)
			}
			out = append(out, j)
		}
	}
	sort.Slice(out, func(x, y int) bool { return *out[x].QueueOrder < *out[y].QueueOrder })
	return out
}

func TestMoveChangesWhichJobStartsNext(t *testing.T) {
	b, c, clk := newDemo(t)
	ctx := context.Background()
	for _, a := range []*album{albums[3], albums[4], albums[1]} {
		if res, err := c.Submit(ctx, []string{a.url()}, nil); err != nil || res.Accepted != 1 {
			t.Fatalf("submit %s: %+v, %v", a.title, res, err)
		}
	}
	queue := queuedInOrder(t, c)
	if len(queue) != 4 {
		t.Fatalf("queue = %d jobs, want the seeded one and three new", len(queue))
	}
	last := queue[3]
	moved, err := c.Move(ctx, last.ID, domain.MoveRequest{To: "first"})
	if err != nil || moved.ID != last.ID || moved.QueueOrder == nil || *moved.QueueOrder >= *queue[0].QueueOrder {
		t.Fatalf("move to first = %+v, %v", moved, err)
	}
	if _, err := c.Move(ctx, queue[0].ID, domain.MoveRequest{After: queue[2].ID}); err != nil {
		t.Fatal(err)
	}
	want := []string{last.ID, queue[1].ID, queue[2].ID, queue[0].ID}
	var got []string
	for _, j := range queuedInOrder(t, c) {
		got = append(got, j.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("queue after moves = %v, want %v", got, want)
	}

	var apiErr *client.APIError
	l, _ := c.Downloads(ctx, client.ListQuery{})
	for _, j := range l.Downloads {
		if j.Status == domain.JobRunning {
			if _, err := c.Move(ctx, j.ID, domain.MoveRequest{To: "last"}); !errors.As(err, &apiErr) || apiErr.Code != "job_not_queued" {
				t.Errorf("moving a running job: %v", err)
			}
			break
		}
	}
	if _, err := c.Move(ctx, last.ID, domain.MoveRequest{To: "middle"}); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Errorf("to middle: %v", err)
	}

	// The next job to start is the moved one.
	for range 2000 {
		run(b, clk, Tick)
		if d, _ := c.Download(ctx, last.ID); d.Job.Status != domain.JobQueued {
			break
		}
	}
	for _, j := range queuedInOrder(t, c) {
		if j.ID == last.ID {
			t.Fatal("the moved job never started")
		}
	}
	if d, _ := c.Download(ctx, queue[1].ID); d.Job.Status != domain.JobQueued && d.Job.StartedAt.Before(*mustStarted(t, c, last.ID)) {
		t.Errorf("%s started before the job moved ahead of it", queue[1].ID)
	}
}

func mustStarted(t *testing.T, c *client.Client, id string) *time.Time {
	t.Helper()
	d, err := c.Download(context.Background(), id)
	if err != nil || d.Job.StartedAt == nil {
		t.Fatalf("job %s: %+v, %v", id, d.Job, err)
	}
	return d.Job.StartedAt
}

// Pause, resume, reset and remove leave a record as the backend's do: a
// pause stops its run as paused, a resume queues the rest, a reset of a
// paused record waits for its resume unless it resets everything, and a
// removal tells the feed.
func TestRecordActionsMatchTheBackend(t *testing.T) {
	synctest.Test(t, testRecordActionsMatchTheBackend)
}

func testRecordActionsMatchTheBackend(t *testing.T) {
	b, c, clk := newDemo(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, _ := c.Downloads(ctx, client.ListQuery{})
	var run0 domain.Job
	for _, j := range l.Downloads {
		if j.Status == domain.JobRunning {
			run0 = j
			break
		}
	}
	album := run0.ResourceID
	if m, err := c.PauseRecord(ctx, domain.ResourceAlbum, album); err != nil || !m.Paused || m.ActiveJobID != "" {
		t.Fatalf("pause: %+v, %v", m, err)
	}
	if d, _ := c.Download(ctx, run0.ID); d.Job.Status != domain.JobCancelled || d.Job.Error != "paused" {
		t.Errorf("the paused record's run: %s %q, want cancelled as paused", d.Job.Status, d.Job.Error)
	}
	if job, err := c.ResetRecord(ctx, domain.ResourceAlbum, album, client.RecordReset{What: "audio"}); err != nil || job != nil {
		t.Errorf("audio reset of a paused record: %+v, %v, want no run until its resume", job, err)
	}
	m, err := c.ResumeRecord(ctx, domain.ResourceAlbum, album)
	if err != nil || m.Paused || m.ActiveJobID == "" {
		t.Fatalf("resume: %+v, %v, want a run for the rest", m, err)
	}
	run(b, clk, 6*time.Minute)
	if m, err := c.PauseRecord(ctx, domain.ResourceAlbum, album); err != nil || !m.Paused || m.State != domain.RecordComplete {
		t.Fatalf("pause of the complete record: %+v, %v", m, err)
	}
	if m, err := c.ResumeRecord(ctx, domain.ResourceAlbum, album); err != nil || m.ActiveJobID != "" {
		t.Errorf("resume of the complete record: %+v, %v, want no run", m, err)
	}
	// The feed opens now: a feed that falls behind loses messages, as the
	// simulated minutes above would make it.
	msgs := make(chan domain.DownloadFeedMessage, 1024)
	live := make(chan bool, 1)
	go c.FollowFeed(ctx, client.FeedHandlers{
		Message: func(m domain.DownloadFeedMessage) { msgs <- m },
		State: func(s client.ConnState) {
			if s.Phase == client.Live {
				live <- true
			}
		},
	})
	synctest.Wait()
	if !received(live) {
		t.Fatal("the feed never went live")
	}
	if err := c.DeleteRecord(ctx, domain.ResourceAlbum, album); err != nil {
		t.Fatal(err)
	}
	synctest.Wait()
	for len(msgs) > 0 {
		if m := <-msgs; m.Type == "resource_deleted" && m.ResourceKind == domain.ResourceAlbum && m.ResourceID == album {
			return
		}
	}
	t.Fatal("the feed did not say the record was removed")
}

// received reports whether c has a value ready, without waiting.
func received[T any](c <-chan T) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}
