package jobs

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

// recordProcessor is a processor that follows the RecordReporter contract
// the way internal/media will: read the album, plan, then one outcome per
// slot. outcome decides each slot: nil is done, errUnavailableCodec is
// unavailable, any other error fails it. skipPlan returns right after the
// read, as a processor that ignores records would.
type recordProcessor struct {
	keyedProcessor
	dir      string
	tracks   []string
	skipPlan bool
	runErr   error
	// leaveErr, when set, is returned right after planning, leaving every
	// planned slot without an outcome.
	leaveErr error
	outcome  func(PlannedSlot) error
	// hold, when set, blocks each run until it is closed or the run is
	// cancelled; started gets the run first.
	hold    chan struct{}
	started chan domain.Job

	mu    sync.Mutex
	plans []Plan
}

var errUnavailableCodec = errors.New("codec not offered")

func (p *recordProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error {
	if p.started != nil {
		p.started <- job
	}
	if p.hold != nil {
		select {
		case <-p.hold:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if job.ResourceID == "" {
		return nil
	}
	rr := reporter.(RecordReporter)
	tracks := make([]domain.Track, 0, len(p.tracks))
	for i, id := range p.tracks {
		tracks = append(tracks, domain.Track{SongID: id, TrackNumber: i + 1, SongNumber: i + 1, Name: "Track " + id})
	}
	if err := rr.ApplyAlbumRead(ctx, db.AlbumRead{
		Album:  domain.Album{ID: job.ResourceID, Name: "Album " + job.ResourceID, ReleaseTrackCount: len(tracks)},
		Tracks: tracks, Storefront: "us",
	}); err != nil {
		return err
	}
	if p.skipPlan || p.runErr != nil {
		return p.runErr
	}
	plan, err := rr.PlanRun(ctx, job)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.plans = append(p.plans, plan)
	p.mu.Unlock()
	if p.leaveErr != nil {
		return p.leaveErr
	}
	for _, slot := range plan.Slots {
		path := filepath.Join(p.dir, job.ResourceID, slot.SongID+"-"+slot.Slot+".m4a")
		if err := rr.ClaimSlot(ctx, job.ID, slot.FileKey, path); err != nil {
			return err
		}
		var outcome error
		if p.outcome != nil {
			outcome = p.outcome(slot)
		}
		switch {
		case outcome == nil:
			err = rr.SlotDone(ctx, job.ID, slot.FileKey, db.FileOutcome{Codec: "alac"})
		case errors.Is(outcome, errUnavailableCodec):
			err = rr.SlotUnavailable(ctx, job.ID, slot.FileKey)
		default:
			_, err = rr.SlotFailed(ctx, job.ID, slot.FileKey, outcome)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (p *recordProcessor) lastPlan() Plan {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.plans) == 0 {
		return Plan{}
	}
	return p.plans[len(p.plans)-1]
}

// newRecordManager is a started Manager over a fresh store with cfg's
// download keys live.
func newRecordManager(t *testing.T, p Processor, cfg config.Config) (*Manager, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	m := NewManager(store, events.NewHub(), p, 1, slog.Default())
	m.SetConfig(func() config.Config { return cfg })
	return m, store
}

func startManager(t *testing.T, m *Manager) {
	t.Helper()
	m.Start(context.Background())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
}

// waitRun waits up to 5 s for a run to reach a terminal status.
func waitRun(t *testing.T, store *db.Store, id string) domain.Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, err := store.GetJob(context.Background(), id)
		if err == nil && job.Status.IsTerminal() {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %s did not finish: %+v (%v)", id, job, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func fileState(t *testing.T, store *db.Store, album, song, slot string) domain.File {
	t.Helper()
	f, err := store.GetFile(context.Background(), domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: album, SongID: song, Slot: slot})
	if err != nil {
		t.Fatalf("slot %s/%s/%s: %v", album, song, slot, err)
	}
	return f
}

func runsOf(t *testing.T, store *db.Store, kind domain.ResourceKind, id string) []domain.Job {
	t.Helper()
	all, _, err := store.ListJobs(context.Background(), db.JobListFilter{Limit: 100, Sort: db.JobListSortCreatedAt, Order: db.JobListOrderDesc})
	if err != nil {
		t.Fatal(err)
	}
	var out []domain.Job
	for _, job := range all {
		if job.ResourceKind == kind && job.ResourceID == id {
			out = append(out, job)
		}
	}
	return out
}

// An album link adds an unresolved record and queues its one run in the
// same step; a second add of the record while that run is active answers
// duplicate_active with it, as a resubmit did before.
func TestAlbumAddQueuesOneRunPerRecord(t *testing.T) {
	m, store := newRecordManager(t, keyedProcessor{}, config.Default())
	ctx := context.Background()

	first := m.SubmitBatch(ctx, []string{"album|us|100"}, nil)
	run := first.Results[0].Job
	if first.Accepted != 1 || run == nil {
		t.Fatalf("first add = %+v, want an accepted run", first)
	}
	if run.ResourceKind != domain.ResourceAlbum || run.ResourceID != "100" || run.Trigger != domain.TriggerManual ||
		run.Priority != domain.PriorityManual || run.CanonicalKey != "" {
		t.Fatalf("run = %+v, want a manual album run of record 100 without a canonical key", run)
	}
	album, err := store.GetAlbum(ctx, "100")
	if err != nil || album.ResolvedAt != nil || !album.WantedAll || album.State != domain.RecordNone {
		t.Fatalf("record = %+v (%v), want an unresolved whole-album record", album, err)
	}
	second := m.SubmitBatch(ctx, []string{"album|us|100"}, nil)
	if got := second.Results[0]; got.Status != domain.SubmitDuplicateActive || got.ExistingJobID != run.ID {
		t.Fatalf("second add = %+v, want duplicate_active for %s", got, run.ID)
	}
	if runs := runsOf(t, store, domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Fatalf("runs = %d, want one per record", len(runs))
	}
}

// An album-track link adds the song to its album record at once; a song
// link without its album waits as a pending add the resolver reads.
func TestSongAddsTrackOrPendingAdd(t *testing.T) {
	m, store := newRecordManager(t, keyedProcessor{}, config.Default())
	ctx := context.Background()

	resp := m.SubmitBatch(ctx, []string{"song|us|222|100", "song|us|333"}, nil)
	if resp.Accepted != 2 || resp.Results[0].Job == nil || resp.Results[1].Job != nil {
		t.Fatalf("adds = %+v, want a run for the album-track link and none for the bare song", resp)
	}
	album, err := store.GetAlbum(ctx, "100")
	if err != nil || album.WantedAll {
		t.Fatalf("album = %+v (%v), want a record with one track wanted", album, err)
	}
	if f := fileState(t, store, "100", "222", domain.SlotMain); f.AudioState != domain.AudioPending {
		t.Fatalf("song slot = %s, want pending", f.AudioState)
	}
	pending, err := store.ListPending(ctx, domain.PendingAddPending)
	if err != nil || len(pending) != 1 || pending[0].Kind != domain.PendingSong || pending[0].CatalogID != "333" {
		t.Fatalf("pending adds = %+v (%v), want song 333", pending, err)
	}
}

type fakeCatalog struct {
	songAlbum    map[string]string
	artistAlbums map[string][]string
	err          error
	calls        int
}

func (c *fakeCatalog) AccountStorefront(context.Context) (string, error) { return "cn", nil }

func (c *fakeCatalog) SongAlbumID(_ context.Context, storefront, songID string) (string, error) {
	c.calls++
	if storefront != "cn" {
		return "", errors.New("read outside the account storefront")
	}
	if c.err != nil {
		return "", c.err
	}
	return c.songAlbum[songID], nil
}

func (c *fakeCatalog) ArtistAlbumIDs(_ context.Context, _, artistID, include string) ([]string, error) {
	c.calls++
	if include != "all" {
		return nil, errors.New("include = " + include)
	}
	return c.artistAlbums[artistID], nil
}

type notFound struct{}

func (notFound) Error() string      { return "404 not found" }
func (notFound) NonRetryable() bool { return true }

// The resolver turns a song into its album record with that track wanted and
// an artist into its albums, each in the account storefront, and drops the
// pending rows. A song the storefront does not have fails at once.
func TestResolverTurnsPendingAddsIntoRecords(t *testing.T) {
	cfg := config.Default()
	cfg.Download.MaxAttempts = 1
	m, store := newRecordManager(t, keyedProcessor{}, cfg)
	ctx := context.Background()
	m.SetCatalog(&fakeCatalog{
		songAlbum:    map[string]string{"333": "300"},
		artistAlbums: map[string][]string{"9": {"901", "902"}},
	})
	if resp := m.SubmitBatch(ctx, []string{"song|us|333", "artist|us|9"}, nil); resp.Accepted != 2 {
		t.Fatalf("adds = %+v", resp)
	}
	m.resolvePending(ctx)

	if pending, _ := store.ListPending(ctx, ""); len(pending) != 0 {
		t.Fatalf("pending adds left = %+v, want none", pending)
	}
	if album, err := store.GetAlbum(ctx, "300"); err != nil || album.WantedAll {
		t.Fatalf("song's album = %+v (%v), want a record with one track", album, err)
	}
	fileState(t, store, "300", "333", domain.SlotMain)
	for _, id := range []string{"901", "902"} {
		album, err := store.GetAlbum(ctx, id)
		if err != nil || !album.WantedAll {
			t.Fatalf("artist album %s = %+v (%v), want a whole-album record", id, album, err)
		}
		sources, _ := store.RecordSources(ctx, domain.ResourceAlbum, id)
		if len(sources) != 1 || sources[0] != domain.SourceArtist {
			t.Fatalf("album %s sources = %v, want artist", id, sources)
		}
		if runs := runsOf(t, store, domain.ResourceAlbum, id); len(runs) != 1 || runs[0].Trigger != domain.TriggerArtist {
			t.Fatalf("album %s runs = %+v, want one artist run", id, runs)
		}
	}

	catalog := &fakeCatalog{err: notFound{}}
	m.SetCatalog(catalog)
	m.SubmitBatch(ctx, []string{"song|us|444"}, nil)
	m.resolvePending(ctx)
	failed, _ := store.ListPending(ctx, domain.PendingAddFailed)
	if len(failed) != 1 || failed[0].CatalogID != "444" || catalog.calls != 1 {
		t.Fatalf("failed adds = %+v after %d reads, want song 444 failed after one read", failed, catalog.calls)
	}
}

// A record run reads the album, plans its due slots and reports each one.
// A failed slot backs off 30 minutes and makes the run failed; the picker
// does not queue the record again before the backoff ends.
func TestRecordRunPlansAndReportsSlots(t *testing.T) {
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2", "3"}, outcome: func(s PlannedSlot) error {
		switch s.SongID {
		case "2":
			return errors.New("decrypt failed")
		case "3":
			return errUnavailableCodec
		}
		return nil
	}}
	m, store := newRecordManager(t, p, config.Default())
	startManager(t, m)
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	done := waitRun(t, store, run.ID)
	if done.Status != domain.JobFailed {
		t.Fatalf("run = %s %q, want failed: one slot failed", done.Status, done.Error)
	}
	plan := p.lastPlan()
	if !plan.Planned || len(plan.Slots) != 3 || plan.Slots[0].ItemID == "" || plan.Slots[0].Position != 1 {
		t.Fatalf("plan = %+v, want three planned slots with items", plan)
	}
	items, _ := store.ListItems(ctx, run.ID)
	if len(items) != 3 || items[0].Slot != domain.SlotMain || items[0].OwnerID != "100" {
		t.Fatalf("items = %+v, want one per slot", items)
	}
	if f := fileState(t, store, "100", "1", domain.SlotMain); f.AudioState != domain.AudioDone || f.Path == "" {
		t.Fatalf("slot 1 = %+v, want done at its claimed path", f)
	}
	failed := fileState(t, store, "100", "2", domain.SlotMain)
	if failed.AudioState != domain.AudioFailed || failed.Failures != 1 || failed.NextAttemptAt == nil ||
		failed.NextAttemptAt.Before(time.Now().Add(29*time.Minute)) {
		t.Fatalf("slot 2 = %+v, want failed once, due in 30 minutes", failed)
	}
	if f := fileState(t, store, "100", "3", domain.SlotMain); f.AudioState != domain.AudioUnavailable {
		t.Fatalf("slot 3 = %s, want unavailable", f.AudioState)
	}
	m.pickOnce(ctx)
	if runs := runsOf(t, store, domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Fatalf("runs = %d, want no new run before the backoff ends", len(runs))
	}
	album, _ := store.GetAlbum(ctx, "100")
	if album.TracksDone != 1 || album.TracksFailed != 1 || album.State != domain.RecordPartial {
		t.Fatalf("rollup = %+v, want partial with one done and one failed", album.Rollup)
	}
}

// A run that ends without planning, as a processor that ignores records
// does, counts no failure against a slot (its error is the run's, not the
// slot's). It fails, and the record's backoff holds it back instead of the
// picker queueing it again at once.
func TestRunThatNeverPlansCountsNoSlotFailure(t *testing.T) {
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}, skipPlan: true}
	m, store := newRecordManager(t, p, config.Default())
	startManager(t, m)
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	done := waitRun(t, store, run.ID)
	if done.Status != domain.JobFailed {
		t.Fatalf("run = %s, want failed: it planned nothing", done.Status)
	}
	for _, song := range []string{"1", "2"} {
		if f := fileState(t, store, "100", song, domain.SlotMain); f.AudioState != domain.AudioPending || f.Failures != 0 {
			t.Fatalf("slot %s = %+v, want pending without a failure", song, f)
		}
	}
	m.pickOnce(ctx)
	if runs := runsOf(t, store, domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Fatalf("runs = %d, want one: the record's backoff holds it", len(runs))
	}
}

// nonRetryableErr is an error that says a retry cannot pass, as an amp-api
// 4xx does.
type nonRetryableErr struct{ msg string }

func (e nonRetryableErr) Error() string      { return e.msg }
func (e nonRetryableErr) NonRetryable() bool { return true }

// A non-retryable run error (a 403 on the record's read) gives up no slot:
// before planning it counts none, after planning each slot it left counts
// one failure.
func TestNonRetryableRunErrorGivesUpNoSlot(t *testing.T) {
	forbidden := nonRetryableErr{"catalog 403"}
	for name, p := range map[string]*recordProcessor{
		"before planning": {runErr: forbidden},
		"after planning":  {leaveErr: forbidden},
	} {
		t.Run(name, func(t *testing.T) {
			p.dir, p.tracks = t.TempDir(), []string{"1", "2"}
			m, store := newRecordManager(t, p, config.Default())
			startManager(t, m)
			ctx := context.Background()

			run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
			if done := waitRun(t, store, run.ID); done.Status != domain.JobFailed {
				t.Fatalf("run = %s, want failed", done.Status)
			}
			want, failures := domain.AudioPending, 0
			if p.leaveErr != nil {
				want, failures = domain.AudioFailed, 1
			}
			for _, song := range []string{"1", "2"} {
				if f := fileState(t, store, "100", song, domain.SlotMain); f.AudioState != want || f.Failures != failures {
					t.Fatalf("slot %s = %s with %d failures, want %s with %d", song, f.AudioState, f.Failures, want, failures)
				}
			}
		})
	}
}

// An unresolved record whose run fails has no slot to carry the failure; the
// picker holds it back instead of queueing a run every time one ends.
func TestUnresolvedRecordBacksOffAfterAFailedRun(t *testing.T) {
	m, store := newRecordManager(t, &recordProcessor{}, config.Default())
	m.processor = resolveFailsProcessor{}
	startManager(t, m)
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	if done := waitRun(t, store, run.ID); done.Status != domain.JobFailed {
		t.Fatalf("run = %s, want failed", done.Status)
	}
	m.pickOnce(ctx)
	if runs := runsOf(t, store, domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Fatalf("runs = %d, want the record held back", len(runs))
	}
	// A new add of the record clears the hold.
	if again := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0]; again.Job == nil {
		t.Fatalf("add after the failure = %+v, want a new run", again)
	}
}

type resolveFailsProcessor struct{ keyedProcessor }

func (resolveFailsProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return errors.New("catalog 503")
}

// Cancel on a record run pauses the record: the run ends cancelled with its
// running slots back to pending, nothing queues the record again, and Resume
// lets the picker run it.
func TestCancelPausesTheRecordAndResumeRunsIt(t *testing.T) {
	hold := make(chan struct{})
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1"}, hold: hold, started: make(chan domain.Job, 4)}
	m, store := newRecordManager(t, p, config.Default())
	startManager(t, m)
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	<-p.started
	if err := m.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	done := waitRun(t, store, run.ID)
	if done.Status != domain.JobCancelled || done.Error != "paused" {
		t.Fatalf("run = %s %q, want cancelled by the pause", done.Status, done.Error)
	}
	if album, _ := store.GetAlbum(ctx, "100"); !album.Paused {
		t.Fatal("record not paused")
	}
	m.pickOnce(ctx)
	if runs := runsOf(t, store, domain.ResourceAlbum, "100"); len(runs) != 1 {
		t.Fatalf("runs = %d, want none queued for a paused record", len(runs))
	}

	close(hold)
	if err := m.Resume(ctx, domain.ResourceAlbum, "100"); err != nil {
		t.Fatal(err)
	}
	var resumed domain.Job
	select {
	case resumed = <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("no run after Resume")
	}
	if done := waitRun(t, store, resumed.ID); done.Status != domain.JobCompleted {
		t.Fatalf("resumed run = %s %q, want completed", done.Status, done.Error)
	}
	if f := fileState(t, store, "100", "1", domain.SlotMain); f.AudioState != domain.AudioDone {
		t.Fatalf("slot = %s, want done", f.AudioState)
	}
}

// A reset is refused while the record has a run, then re-arms the slots and
// queues a run with trigger reset that replaces each done file at its stored
// path. Retry of a failed run re-arms only the failed and given-up slots.
func TestResetAndRetryQueueRunsWithTriggerReset(t *testing.T) {
	failSong2 := true
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}, outcome: func(s PlannedSlot) error {
		if s.SongID == "2" && failSong2 {
			return errors.New("boom")
		}
		return nil
	}}
	cfg := config.Default()
	cfg.Download.GiveUpAfterRuns = 1
	m, store := newRecordManager(t, p, cfg)
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	if _, err := m.Reset(ctx, ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: ResetAudio}); !errors.Is(err, db.ErrRunActive) {
		t.Fatalf("reset with a queued run = %v, want ErrRunActive", err)
	}
	startManager(t, m)
	if done := waitRun(t, store, run.ID); done.Status != domain.JobFailed {
		t.Fatalf("first run = %s, want failed", done.Status)
	}
	if f := fileState(t, store, "100", "2", domain.SlotMain); f.AudioState != domain.AudioGivenUp {
		t.Fatalf("slot 2 = %s, want given up after give_up_after_runs 1", f.AudioState)
	}

	failSong2 = false
	if err := m.Retry(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	retried := runsOf(t, store, domain.ResourceAlbum, "100")
	if len(retried) != 2 || retried[0].Trigger != domain.TriggerReset {
		t.Fatalf("runs after retry = %+v, want a new reset run", retried)
	}
	waitRun(t, store, retried[0].ID)
	if plan := p.lastPlan(); len(plan.Slots) != 1 || plan.Slots[0].SongID != "2" {
		t.Fatalf("retry plan = %+v, want only the given-up slot", plan.Slots)
	}

	result, err := m.Reset(ctx, ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: ResetAudio, SongIDs: []string{"1"}})
	if err != nil || result.Slots != 1 || result.Run == nil || result.Run.Trigger != domain.TriggerReset {
		t.Fatalf("reset = %+v (%v), want one slot and a reset run", result, err)
	}
	waitRun(t, store, result.Run.ID)
	plan := p.lastPlan()
	if len(plan.Slots) != 1 || !plan.Slots[0].Replace || plan.Slots[0].FixedPath == "" {
		t.Fatalf("reset plan = %+v, want the done slot replaced at its stored path", plan.Slots)
	}
	if again, err := m.RetryRun(ctx, run.ID); err != nil || again.Job != nil || !again.AlreadyDownloaded {
		t.Fatalf("retry of a complete record = %+v (%v), want already downloaded", again, err)
	}
}

// A retry of a failed run whose record has only missing files left queues a
// run that downloads them again; the missing slots are not left waiting for
// a reset nothing would start.
func TestRetryDownloadsMissingFiles(t *testing.T) {
	failSong2 := true
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}, outcome: func(s PlannedSlot) error {
		if s.SongID == "2" && failSong2 {
			return errors.New("boom")
		}
		return nil
	}}
	m, store := newRecordManager(t, p, config.Default())
	ctx := context.Background()
	startManager(t, m)

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	if done := waitRun(t, store, run.ID); done.Status != domain.JobFailed {
		t.Fatalf("first run = %s, want failed", done.Status)
	}
	failSong2 = false
	reset, err := m.Reset(ctx, ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: ResetAudio, SongIDs: []string{"2"}})
	if err != nil || reset.Run == nil {
		t.Fatalf("reset = %+v (%v), want a run", reset, err)
	}
	waitRun(t, store, reset.Run.ID)
	key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "100", SongID: "1", Slot: domain.SlotMain}
	if marked, err := store.MarkFileMissing(ctx, key, time.Now().UTC()); err != nil || !marked {
		t.Fatalf("mark missing = %v (%v)", marked, err)
	}

	res, err := m.RetryRun(ctx, run.ID)
	if err != nil || res.Job == nil {
		t.Fatalf("retry with only a missing file = %+v (%v), want a queued run", res, err)
	}
	waitRun(t, store, res.Job.ID)
	if plan := p.lastPlan(); len(plan.Slots) != 1 || plan.Slots[0].SongID != "1" {
		t.Fatalf("retry plan = %+v, want the missing slot", plan.Slots)
	}
	if f := fileState(t, store, "100", "1", domain.SlotMain); f.AudioState != domain.AudioDone {
		t.Fatalf("slot 1 = %s, want done again", f.AudioState)
	}
}

// A retry of a record's run that the full queue cannot take queues nothing
// and is not "already downloaded": the scheduler queues the record later.
func TestRetryWithAFullQueueWaits(t *testing.T) {
	m, _ := newRecordManager(t, &recordProcessor{dir: t.TempDir(), tracks: []string{"1"}}, config.Default())
	m.capacity = 1
	ctx := context.Background()

	run := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	if err := m.Cancel(ctx, run.ID); err != nil {
		t.Fatal(err)
	}
	if other := m.SubmitBatch(ctx, []string{"album|us|200"}, nil).Results[0]; other.Job == nil {
		t.Fatalf("second album = %+v, want a queued run filling the queue", other)
	}
	res, err := m.RetryRun(ctx, run.ID)
	if err != nil || res.Job != nil || res.AlreadyDownloaded {
		t.Fatalf("retry with a full queue = %+v (%v), want neither a run nor already downloaded", res, err)
	}
}

// The library monitor does not add back an album removed from amdl; a
// manual add does, and clears the removal.
func TestAddAlbumsSkipsRemovedRecords(t *testing.T) {
	m, store := newRecordManager(t, keyedProcessor{}, config.Default())
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := store.AddAlbum(ctx, db.AlbumAdd{ID: "100", WantedAll: true, Source: domain.SourceManual, Now: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteRecord(ctx, domain.ResourceAlbum, "100", now); err != nil {
		t.Fatal(err)
	}

	out, err := m.AddAlbums(ctx, domain.SourceLibrarySync, []string{"100", "200"})
	if err != nil || out["100"] != AddTombstoned || out["200"] != AddQueued {
		t.Fatalf("AddAlbums = %v (%v), want 100 tombstoned and 200 queued", out, err)
	}
	if _, err := store.GetAlbum(ctx, "100"); !errors.Is(err, db.ErrRecordNotFound) {
		t.Fatalf("removed album = %v, want it still removed", err)
	}
	if runs := runsOf(t, store, domain.ResourceAlbum, "200"); len(runs) != 1 || runs[0].Priority != domain.PriorityAuto ||
		runs[0].Trigger != domain.TriggerLibrarySync {
		t.Fatalf("library album runs = %+v, want one auto library_sync run", runs)
	}

	if resp := m.SubmitBatch(ctx, []string{"album|us|100"}, nil); resp.Results[0].Job == nil {
		t.Fatalf("manual add = %+v, want a run", resp)
	}
	if again, _ := m.AddAlbums(ctx, domain.SourceLibrarySync, []string{"100"}); again["100"] == AddTombstoned {
		t.Fatal("a manual add did not clear the removal")
	}
}

// The picker queues one run per record with work, auto class after manual,
// and a retry run for a record whose failed slot is due again.
func TestPickerQueuesEligibleRecords(t *testing.T) {
	m, store := newRecordManager(t, keyedProcessor{}, config.Default())
	ctx := context.Background()
	now := time.Now().UTC()
	for _, add := range []db.AlbumAdd{
		{ID: "auto", WantedAll: true, Source: domain.SourceLibrarySync, Priority: domain.PriorityAuto, Now: now},
		{ID: "manual", SongIDs: []string{"1"}, Source: domain.SourceManual, Now: now},
	} {
		if _, err := store.AddAlbum(ctx, add); err != nil {
			t.Fatal(err)
		}
	}
	key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "manual", SongID: "1", Slot: domain.SlotMain}
	if _, err := store.MarkFileFailed(ctx, key, db.FileFailure{Error: "boom", Now: now.Add(-time.Hour), GiveUpAfter: 3}); err != nil {
		t.Fatal(err)
	}

	m.pickOnce(ctx)
	queued, err := store.QueuedJobIDs(ctx)
	if err != nil || len(queued) != 2 {
		t.Fatalf("queued = %v (%v), want one run per record", queued, err)
	}
	first, _ := store.GetJob(ctx, queued[0])
	if first.ResourceID != "manual" || first.Trigger != domain.TriggerRetry {
		t.Fatalf("first queued run = %+v, want the manual record's retry", first)
	}
	m.pickOnce(ctx)
	if again, _ := store.QueuedJobIDs(ctx); len(again) != 2 {
		t.Fatalf("queued after a second pass = %v, want still one run per record", again)
	}
	now2, err := m.DownloadNow(ctx, queued[1])
	if err != nil || now2.Priority != domain.PriorityNow {
		t.Fatalf("DownloadNow = %+v (%v)", now2, err)
	}
	if order, _ := store.QueuedJobIDs(ctx); order[0] != queued[1] {
		t.Fatalf("run order = %v, want %s first", order, queued[1])
	}
}

// Startup sets the slots a stopped process left running back to pending.
func TestRecoverUnfinishedReleasesRunningSlots(t *testing.T) {
	m, store := newRecordManager(t, keyedProcessor{}, config.Default())
	ctx := context.Background()
	now := time.Now().UTC()
	if _, err := store.AddAlbum(ctx, db.AlbumAdd{ID: "100", SongIDs: []string{"1"}, Source: domain.SourceManual, Now: now}); err != nil {
		t.Fatal(err)
	}
	key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "100", SongID: "1", Slot: domain.SlotMain}
	if err := store.ClaimPath(ctx, key, filepath.Join(t.TempDir(), "1.m4a"), "job-gone", now); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RecoverUnfinished(ctx); err != nil {
		t.Fatal(err)
	}
	if f := fileState(t, store, "100", "1", domain.SlotMain); f.AudioState != domain.AudioPending || f.Failures != 0 {
		t.Fatalf("slot = %+v, want pending without a failure", f)
	}
}

// restartProcessor writes items as internal/media does: song 1 fails, song 2
// blocks until the run's context ends while block is set, else lands.
type restartProcessor struct {
	keyedProcessor
	dir     string
	block   bool
	claimed chan struct{}
}

func (p *restartProcessor) ProcessJob(ctx context.Context, job domain.Job, reporter Reporter) error {
	rr := reporter.(RecordReporter)
	tracks := []domain.Track{{SongID: "1", TrackNumber: 1, SongNumber: 1}, {SongID: "2", TrackNumber: 2, SongNumber: 2}}
	if err := rr.ApplyAlbumRead(ctx, db.AlbumRead{Album: domain.Album{ID: job.ResourceID, Name: "A", ReleaseTrackCount: 2}, Tracks: tracks, Storefront: "us"}); err != nil {
		return err
	}
	plan, err := rr.PlanRun(ctx, job)
	if err != nil {
		return err
	}
	items, err := reporter.ListItems(ctx, job.ID)
	if err != nil {
		return err
	}
	byID := map[string]domain.JobItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	for _, slot := range plan.Slots {
		item := byID[slot.ItemID]
		if err := rr.ClaimSlot(ctx, job.ID, slot.FileKey, filepath.Join(p.dir, slot.SongID+".m4a")); err != nil {
			return err
		}
		if slot.SongID == "1" {
			item.Status, item.Error = domain.ItemFailed, "decrypt failed"
			if err := reporter.UpdateItem(ctx, &item); err != nil {
				return err
			}
			if _, err := rr.SlotFailed(ctx, job.ID, slot.FileKey, errors.New("decrypt failed")); err != nil {
				return err
			}
			continue
		}
		if p.block {
			close(p.claimed)
			<-ctx.Done()
			return ctx.Err()
		}
		item.Status = domain.ItemCompleted
		if err := reporter.UpdateItem(ctx, &item); err != nil {
			return err
		}
		if err := rr.SlotDone(ctx, job.ID, slot.FileKey, db.FileOutcome{Codec: "alac"}); err != nil {
			return err
		}
	}
	return nil
}

// A run recovered after a restart re-plans without the slot it failed
// before the restart (that slot waits for its backoff), and still ends
// failed with that slot's item failed.
func TestRecoveredRunKeepsASlotFailedBeforeTheRestart(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	dir := t.TempDir()
	cfg := config.Default()
	ctx := context.Background()
	manager := func(p Processor) *Manager {
		m := NewManager(store, events.NewHub(), p, 1, slog.Default())
		m.SetConfig(func() config.Config { return cfg })
		return m
	}

	first := &restartProcessor{dir: dir, block: true, claimed: make(chan struct{})}
	m1 := manager(first)
	m1.Start(ctx)
	run := m1.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0].Job
	select {
	case <-first.claimed:
	case <-time.After(5 * time.Second):
		t.Fatal("song 2 was never claimed")
	}
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := m1.Shutdown(stopCtx); err != nil {
		t.Fatal(err)
	}

	m2 := manager(&restartProcessor{dir: dir})
	if _, err := m2.RecoverUnfinished(ctx); err != nil {
		t.Fatal(err)
	}
	startManager(t, m2)
	done := waitRun(t, store, run.ID)
	if done.Status != domain.JobFailed || done.FailedItems != 1 || done.CompletedItems != 1 {
		t.Fatalf("recovered run = %s (completed %d, failed %d), want failed with one item failed and one completed",
			done.Status, done.CompletedItems, done.FailedItems)
	}
	if f := fileState(t, store, "100", "1", domain.SlotMain); f.AudioState != domain.AudioFailed || f.Failures != 1 {
		t.Fatalf("slot 1 = %s with %d failures, want failed once", f.AudioState, f.Failures)
	}
	if f := fileState(t, store, "100", "2", domain.SlotMain); f.AudioState != domain.AudioDone {
		t.Fatalf("slot 2 = %s, want done", f.AudioState)
	}
}
