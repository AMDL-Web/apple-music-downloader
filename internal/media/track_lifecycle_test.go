package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
	"amdl/internal/jobs"
)

// storeReporter keeps item rows the way the job store does: ListItems sees
// every write, and, like database/sql, a write on a done context fails. That
// last part is what proves a write ran on a context detached from the job's.
type storeReporter struct {
	mu     sync.Mutex
	order  []string
	items  map[string]domain.JobItem
	events []domain.Event
}

func newStoreReporter() *storeReporter {
	return &storeReporter{items: map[string]domain.JobItem{}}
}

func (r *storeReporter) SetJob(ctx context.Context, _ *domain.Job) error { return ctx.Err() }

func (r *storeReporter) SetJobMotionArtwork(ctx context.Context, _ string, _ domain.MotionArtwork) (bool, error) {
	return true, ctx.Err()
}

func (r *storeReporter) AddItem(ctx context.Context, item *domain.JobItem) error {
	return r.UpdateItem(ctx, item)
}

func (r *storeReporter) UpdateItem(ctx context.Context, item *domain.JobItem) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	item.UpdatedAt = time.Now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[item.ID]; !ok {
		r.order = append(r.order, item.ID)
	}
	r.items[item.ID] = *item
	return nil
}

func (r *storeReporter) RemoveItem(ctx context.Context, itemID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, itemID)
	return nil
}

func (r *storeReporter) ListItems(ctx context.Context, _ string) ([]domain.JobItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.snapshot(), nil
}

func (r *storeReporter) Event(ctx context.Context, ev domain.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *storeReporter) snapshot() []domain.JobItem {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.JobItem, 0, len(r.order))
	for _, id := range r.order {
		if item, ok := r.items[id]; ok {
			out = append(out, item)
		}
	}
	return out
}

func (r *storeReporter) eventList() []domain.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.Event(nil), r.events...)
}

// waitForItem polls until some stored item satisfies ok.
func waitForItem(t *testing.T, r *storeReporter, ok func(domain.JobItem) bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, item := range r.snapshot() {
			if ok(item) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no item reached the expected state; items = %+v", r.snapshot())
}

// slowSimulatedAlbum is a three-track album job in simulate mode whose
// transfers crawl at 1 KB/s, with one download permit, so one track sits in
// downloading and the other two wait for the permit until the job is stopped.
func slowSimulatedAlbum(t *testing.T) (*Downloader, domain.Job, string) {
	t.Helper()
	tracks := []applemusic.Song{
		{ID: "song-1", Name: "One", AlbumID: "album-1", AlbumName: "Album", DurationInMillis: 60_000},
		{ID: "song-2", Name: "Two", AlbumID: "album-1", AlbumName: "Album", DurationInMillis: 60_000},
		{ID: "song-3", Name: "Three", AlbumID: "album-1", AlbumName: "Album", DurationInMillis: 60_000},
	}
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1, MaxSpeedKBps: 1}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.TempDir = t.TempDir()
	// No track has a manifest, so aac falls through to the AAC-LC floor at
	// once (config validation does not accept aac-lc itself here).
	cfg.Download.QualityPriority = []string{"aac"}
	cfg.Download.CodecAlternative = true
	cfg.Download.MaxParallelDownloads = 1
	cfg.Download.EmbedCover = false
	cfg.Download.SaveAlbumCover = false
	cfg.Download.SaveArtistCover = false
	d := newTestDownloader(&Downloader{
		store:   config.NewStore(cfg),
		catalog: fakeDownloaderCatalog{album: applemusic.Collection{ID: "album-1", Name: "Album", Tracks: tracks}},
	})
	job := domain.Job{ID: "job-stop", Input: "https://music.apple.com/cn/album/a/album-1", CanonicalKey: "album:cn:album-1"}
	// A checkpoint the job owns, standing in for a partly downloaded track.
	checkpoint, _ := resumableDownloadPaths(cfg.Download.TempDir, job.ID, "out.m4a")
	if err := os.MkdirAll(filepath.Dir(checkpoint), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checkpoint, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	return d, job, checkpoint
}

// runUntilDownloading starts job and stops it with cause once a track is
// downloading.
func runUntilDownloading(t *testing.T, d *Downloader, job domain.Job, reporter *storeReporter, cause error) error {
	t.Helper()
	ctx, stop := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.ProcessJob(ctx, job, reporter) }()
	waitForItem(t, reporter, func(item domain.JobItem) bool { return item.Status == domain.ItemDownloading })
	stop(cause)
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("ProcessJob did not return after it was stopped")
		return nil
	}
}

func TestUserCancelFinishesEveryUnfinishedItemAsCancelled(t *testing.T) {
	d, job, checkpoint := slowSimulatedAlbum(t)
	reporter := newStoreReporter()

	if err := runUntilDownloading(t, d, job, reporter, nil); err == nil {
		t.Fatal("ProcessJob returned nil for a cancelled job")
	}

	items := reporter.snapshot()
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}
	for _, item := range items {
		// The store refuses writes on the cancelled job context, so a cancelled
		// row here was written on a detached one.
		if item.Status != domain.ItemCancelled || item.StatusMessage != "Cancelled" {
			t.Errorf("item %s = %s/%q, want cancelled/Cancelled", item.ID, item.Status, item.StatusMessage)
		}
	}
	// Stream-only clients converge: every item's last event says cancelled.
	last := map[string]domain.Event{}
	for _, ev := range reporter.eventList() {
		if ev.ItemID != "" {
			last[ev.ItemID] = ev
		}
	}
	for _, item := range items {
		ev, ok := last[item.ID]
		if !ok || ev.Type != domain.EventItemProgress || ev.Phase != "cancelled" {
			t.Errorf("last event of %s = %+v, want item_progress with phase cancelled", item.ID, ev)
			continue
		}
		if snapshot := itemProgressSnapshot(t, ev); snapshot.Status != domain.ItemCancelled {
			t.Errorf("cancelled event snapshot status = %s", snapshot.Status)
		}
	}
	if _, err := os.Stat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("a cancelled job kept its resume checkpoint (stat err %v)", err)
	}
}

func TestShutdownLeavesItemsAndCheckpointsForRecovery(t *testing.T) {
	d, job, checkpoint := slowSimulatedAlbum(t)
	reporter := newStoreReporter()

	err := runUntilDownloading(t, d, job, reporter, jobs.ErrShutdown)
	if err == nil {
		t.Fatal("ProcessJob returned nil for an interrupted job")
	}

	statuses := map[domain.ItemStatus]int{}
	for _, item := range reporter.snapshot() {
		statuses[item.Status]++
		switch item.Status {
		case domain.ItemCompleted, domain.ItemSkipped, domain.ItemFailed, domain.ItemCancelled:
			t.Errorf("item %s = %s after shutdown, want it left mid-pipeline for recovery", item.ID, item.Status)
		}
	}
	if statuses[domain.ItemDownloading] != 1 {
		t.Fatalf("item statuses after shutdown = %v, want the downloading track left downloading", statuses)
	}
	for _, ev := range reporter.eventList() {
		if ev.Phase == "cancelled" || ev.Type == domain.EventItemFailed {
			t.Fatalf("shutdown emitted %s/%s for item %s", ev.Type, ev.Phase, ev.ItemID)
		}
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("shutdown removed the job's resume checkpoint: %v", err)
	}
}

// Rows left by an earlier run (here: a track interrupted by a shutdown) are
// still non-terminal when a cancel arrives before the new run reconciles them.
func TestCancelUnfinishedItemsCoversRowsThatNeverRan(t *testing.T) {
	reporter := newStoreReporter()
	for _, item := range []domain.JobItem{
		{ID: "done", JobID: "job-1", Status: domain.ItemCompleted},
		{ID: "skipped", JobID: "job-1", Status: domain.ItemSkipped},
		{ID: "failed", JobID: "job-1", Status: domain.ItemFailed},
		{ID: "interrupted", JobID: "job-1", Status: domain.ItemDecrypting, Progress: domain.ItemProgress{Download: 1, Decrypt: 0.4}},
		{ID: "queued", JobID: "job-1", Status: domain.ItemQueued},
	} {
		if err := reporter.AddItem(context.Background(), &item); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	newTestDownloader(&Downloader{cfg: config.Default()}).cancelUnfinishedItems(ctx, "job-1", reporter)

	want := map[string]domain.ItemStatus{
		"done": domain.ItemCompleted, "skipped": domain.ItemSkipped, "failed": domain.ItemFailed,
		"interrupted": domain.ItemCancelled, "queued": domain.ItemCancelled,
	}
	for _, item := range reporter.snapshot() {
		if item.Status != want[item.ID] {
			t.Errorf("item %s = %s, want %s", item.ID, item.Status, want[item.ID])
		}
		if item.ID == "interrupted" && item.Progress.Decrypt != 0.4 {
			t.Errorf("cancelling rewrote the interrupted item's progress: %+v", item.Progress)
		}
	}
	if events := reporter.eventList(); len(events) != 2 {
		t.Fatalf("events = %+v, want one cancelled item_progress per unfinished item", events)
	}
}

// failingOnceReporter fails the first write of a terminal item state, the way
// SQLITE_BUSY would.
type failingOnceReporter struct {
	*storeReporter
	failed atomic.Bool
}

func (r *failingOnceReporter) UpdateItem(ctx context.Context, item *domain.JobItem) error {
	if item.Status == domain.ItemCompleted && r.failed.CompareAndSwap(false, true) {
		return errors.New("database is locked")
	}
	return r.storeReporter.UpdateItem(ctx, item)
}

func TestTerminalItemWriteIsRetriedOnce(t *testing.T) {
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.QualityPriority = []string{"aac-lc"}
	cfg.Download.CodecAlternative = false
	reporter := &failingOnceReporter{storeReporter: newStoreReporter()}
	d := newTestDownloader(&Downloader{cfg: cfg})

	err := d.processTrack(context.Background(), domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"},
		applemusic.Song{ID: "song-1", Name: "Track", DurationInMillis: 1000}, "cn", applemusic.TypeSong, "", "", 1, "", reporter)
	if err != nil {
		t.Fatal(err)
	}
	if !reporter.failed.Load() {
		t.Fatal("the completed write never failed; the test did not exercise the retry")
	}
	if items := reporter.snapshot(); len(items) != 1 || items[0].Status != domain.ItemCompleted {
		t.Fatalf("stored item = %+v, want completed after the retry", items)
	}
}

// panickyCatalog panics while the track's metadata is resolved.
type panickyCatalog struct{ fakeDownloaderCatalog }

func (panickyCatalog) SongMetadata(context.Context, string, string) (applemusic.Song, error) {
	panic("catalog exploded")
}

func TestTrackPanicFailsOnlyThatItem(t *testing.T) {
	cfg := config.Default()
	reporter := newStoreReporter()
	d := newTestDownloader(&Downloader{cfg: cfg, catalog: panickyCatalog{}})

	err := d.processTrack(context.Background(), domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"},
		applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter)
	if err == nil || !strings.Contains(err.Error(), "catalog exploded") {
		t.Fatalf("error = %v, want the panic as the track's error", err)
	}
	items := reporter.snapshot()
	if len(items) != 1 || items[0].Status != domain.ItemFailed || !strings.Contains(items[0].Error, "track pipeline panicked: catalog exploded") {
		t.Fatalf("stored item = %+v, want failed with the panic", items)
	}
}

func TestRunTrackTasksTurnsAPanicIntoThatTasksError(t *testing.T) {
	var completed atomic.Int32
	err := runTrackTasks(context.Background(), discardLogger(), 3, make([]bool, 3), false, func(_ context.Context, i int) error {
		if i == 1 {
			panic("boom")
		}
		completed.Add(1)
		return nil
	})
	if err == nil || err.Error() != "track 1 panicked: boom" {
		t.Fatalf("error = %v, want track 1's panic", err)
	}
	if completed.Load() != 2 {
		t.Fatalf("completed tasks = %d, want the other 2", completed.Load())
	}
}

// A song without a manifest cannot gain one on retry: the codec gives up after
// one attempt, without the backoff, and the chain falls back.
func TestMissingEnhancedManifestFallsBackWithoutRetrying(t *testing.T) {
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.MaxAttempts = 3
	cfg.Download.QualityPriority = []string{"alac"}
	cfg.Download.CodecAlternative = true
	reporter := &recordingReporter{}
	d := newTestDownloader(&Downloader{cfg: cfg})

	start := time.Now()
	err := d.processTrack(context.Background(), domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"},
		applemusic.Song{ID: "song-1", Name: "Track", DurationInMillis: 1000}, "cn", applemusic.TypeSong, "", "", 1, "", reporter)
	if err != nil {
		t.Fatal(err)
	}
	var failed *domain.Event
	for i, ev := range reporter.events {
		if ev.Type == domain.EventOperationRetry {
			t.Fatalf("a missing manifest was retried: %+v", ev)
		}
		if ev.Type == domain.EventCodecFailed {
			failed = &reporter.events[i]
		}
	}
	if failed == nil || failed.Phase != "alac" || !strings.Contains(failed.Message, "song song-1: no enhanced hls manifest") {
		t.Fatalf("codec_failed = %+v, want alac failing on the missing manifest", failed)
	}
	// The retry backoff starts at one second; the whole fallback track takes
	// about one simulated AAC-LC download.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("track took %v, want no retry backoff", elapsed)
	}
}

func TestResolveEnhancedHLSWrapsTheSentinel(t *testing.T) {
	d := newTestDownloader(&Downloader{cfg: config.Default()})
	_, _, err := d.resolveEnhancedHLS(context.Background(), "cn", applemusic.Song{ID: "song-9"}, "alac")
	if !errors.Is(err, applemusic.ErrNoEnhancedHLS) || !isNonRetryableError(err) {
		t.Fatalf("error = %v, want non-retryable ErrNoEnhancedHLS", err)
	}
	if err.Error() != "song song-9: no enhanced hls manifest" {
		t.Fatalf("error = %q, want it to name the song", err)
	}
}

// The simulated AAC-LC asset request is the resolving step the real one is.
func TestSimulatedAACLCRequestIsResolving(t *testing.T) {
	cfg := config.Default()
	cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
	cfg.Download.DownloadsDir = t.TempDir()
	cfg.Download.QualityPriority = []string{"aac-lc"}
	cfg.Download.CodecAlternative = false
	reporter := &recordingReporter{}
	d := newTestDownloader(&Downloader{cfg: cfg})
	if err := d.processTrack(context.Background(), domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"},
		applemusic.Song{ID: "song-1", Name: "Track", DurationInMillis: 1000}, "cn", applemusic.TypeSong, "", "", 1, "", reporter); err != nil {
		t.Fatal(err)
	}
	for _, ev := range reporter.events {
		if ev.Type == domain.EventItemProgress && ev.Message == "requesting AAC-LC WebPlayback asset" {
			if ev.Phase != string(domain.ItemResolving) {
				t.Fatalf("AAC-LC asset request published as %q, want resolving", ev.Phase)
			}
			return
		}
	}
	t.Fatal("no item_progress for the AAC-LC asset request")
}

// lifecycleStep is one published event with what a client acts on.
type lifecycleStep struct {
	Type    domain.EventType
	Phase   string
	Message string
}

// lifecycleSteps drops meter-only item_progress events, whose count depends on
// transfer pacing, and keeps everything else in order.
func lifecycleSteps(events []domain.Event) []lifecycleStep {
	var steps []lifecycleStep
	for _, ev := range events {
		if ev.Type == domain.EventItemProgress && strings.HasSuffix(ev.Message, "%") {
			continue
		}
		steps = append(steps, lifecycleStep{Type: ev.Type, Phase: ev.Phase, Message: ev.Message})
	}
	return steps
}

// A simulated track publishes the same transitions, messages and events, in
// the same order, as a real download of the same track through fakes.
func TestSimulatedTrackMatchesTheRealLifecycle(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}
	media := filepath.Join(t.TempDir(), "media.m4a")
	gen := exec.Command("ffmpeg", "-y", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "frag_keyframe+empty_moov+default_base_moof", media)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("generate fragmented media: %v: %s", err, out)
	}
	payload, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio-alac-stereo-44100-16\",NAME=\"Lossless\",BIT-DEPTH=16,SAMPLE-RATE=44100\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=900000,AVERAGE-BANDWIDTH=800000,AUDIO=\"audio-alac-stereo-44100-16\",CODECS=\"alac\"\n" +
				"media.m3u8\n"))
		case "/media.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/P000000000/s1/e1/c23\"\n" +
				"#EXT-X-MAP:URI=\"encrypted.mp4\"\n"))
		case "/encrypted.mp4":
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	song := applemusic.Song{
		ID: "song-1", Name: "Track", ArtistName: "Artist", AlbumName: "Album", HasLyrics: true,
		DurationInMillis: 2000, EnhancedHLS: server.URL + "/master.m3u8",
	}
	run := func(t *testing.T, simulate bool) []lifecycleStep {
		t.Helper()
		cfg := config.Default()
		cfg.Simulate = config.SimulateConfig{Enabled: simulate, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
		cfg.Download.DownloadsDir = t.TempDir()
		cfg.Download.TempDir = t.TempDir()
		cfg.Download.QualityPriority = []string{"alac"}
		cfg.Download.CodecAlternative = false
		cfg.Download.CheckIntegrity = true
		cfg.Download.EmbedCover = false
		cfg.Download.EmbedLyrics = true
		cfg.Download.LyricsFormat = "lrc"
		cfg.Download.SaveAlbumCover = false
		cfg.Download.SaveArtistCover = false
		// Every meter move is published, so the gate never holds one back.
		cfg.Download.ProgressEventIntervalMS = 0
		cfg.Tools.FFmpeg = "ffmpeg"
		d := newTestDownloader(&Downloader{
			cfg: cfg, catalog: fakeDownloaderCatalog{song: song},
			wrapper: fakeDownloaderWrapper{lyrics: simulatedLyricsTTML},
			http:    server.Client(), tools: NewToolChecker(cfg.Tools),
		})
		reporter := &recordingReporter{}
		if err := d.processTrack(context.Background(), domain.Job{ID: "job-1"}, domain.JobItem{ID: "item-1", JobID: "job-1"},
			applemusic.Song{ID: "song-1"}, "cn", applemusic.TypeAlbum, "Album", "album-1", 1, "", reporter); err != nil {
			t.Fatalf("simulate=%v: %v", simulate, err)
		}
		final := reporter.items[len(reporter.items)-1]
		if final.Status != domain.ItemCompleted || !final.Progress.Verified || final.LyricsStatus != domain.LyricsFetched {
			t.Fatalf("simulate=%v: final item = %+v", simulate, final)
		}
		return lifecycleSteps(reporter.events)
	}

	real := run(t, false)
	simulated := run(t, true)
	if len(real) != len(simulated) {
		t.Fatalf("real lifecycle has %d steps, simulated %d\nreal: %+v\nsimulated: %+v", len(real), len(simulated), real, simulated)
	}
	for i := range real {
		if real[i] != simulated[i] {
			t.Fatalf("step %d: real %+v, simulated %+v\nreal: %+v", i, real[i], simulated[i], real)
		}
	}
}
