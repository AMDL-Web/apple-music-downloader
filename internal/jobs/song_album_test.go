package jobs

import (
	"context"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
)

// albumMark is album id's record mark, as search hits carry it.
func albumMark(t *testing.T, store *db.Store, id string) domain.ResourceMark {
	t.Helper()
	marks, err := store.RecordMarks(context.Background(), domain.ResourceAlbum, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := marks[id]
	if !ok {
		t.Fatalf("no mark for album %s", id)
	}
	return m
}

// waitAlbumDone waits up to 5 s for album id's record to have done tracks
// done.
func waitAlbumDone(t *testing.T, store *db.Store, id string, done int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := store.GetAlbum(context.Background(), id)
		if err == nil && a.TracksDone == done {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("album %s: %+v (%v), want %d tracks done", id, a, err, done)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// One song of an album downloaded first leaves a record complete in itself
// whose mark says it wants only that song; the album link then downloads
// the tracks it lacks instead of answering already_downloaded.
func TestAlbumAfterOneOfItsSongsDownloadsTheRest(t *testing.T) {
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}}
	m, store := newRecordManager(t, p, config.Default())
	ctx := context.Background()
	startManager(t, m)

	song := m.SubmitBatch(ctx, []string{"song|us|1|100"}, nil).Results[0]
	if song.Job == nil {
		t.Fatalf("song add = %+v, want a run", song)
	}
	waitRun(t, store, song.Job.ID)
	if mark := albumMark(t, store, "100"); mark.State != domain.RecordComplete || mark.WantedAll || mark.TracksWanted != 1 {
		t.Fatalf("mark after the song = %+v, want complete with wanted_all false", mark)
	}

	album := m.SubmitBatch(ctx, []string{"album|us|100"}, nil).Results[0]
	if album.Status != domain.SubmitAccepted || album.Job == nil {
		t.Fatalf("album add = %+v, want an accepted run", album)
	}
	waitRun(t, store, album.Job.ID)
	if plan := p.lastPlan(); len(plan.Slots) != 1 || plan.Slots[0].SongID != "2" {
		t.Fatalf("plan = %+v, want only the missing track", plan.Slots)
	}
	if mark := albumMark(t, store, "100"); mark.State != domain.RecordComplete || !mark.WantedAll || mark.TracksDone != 2 {
		t.Fatalf("mark after the album = %+v, want the whole album done", mark)
	}
}

// The whole album added while a run of one of its songs runs is taken: the
// picker downloads the rest after that run. A second add of the same song
// asks for nothing new and still answers duplicate_active.
func TestAlbumAddWhileASongRunsIsAccepted(t *testing.T) {
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}, hold: make(chan struct{}), started: make(chan domain.Job, 4)}
	m, store := newRecordManager(t, p, config.Default())
	ctx := context.Background()
	startManager(t, m)

	song := m.SubmitBatch(ctx, []string{"song|us|1|100"}, nil).Results[0]
	if song.Job == nil {
		t.Fatalf("song add = %+v, want a run", song)
	}
	select {
	case <-p.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the song's run did not start")
	}

	album := m.SubmitBatch(ctx, []string{"album|us|100"}, nil)
	if got := album.Results[0]; album.Accepted != 1 || got.Status != domain.SubmitAccepted || got.Job != nil ||
		got.Resource == nil || got.Resource.ID != "100" {
		t.Fatalf("album add during the song's run = %+v, want accepted without a run", got)
	}
	again := m.SubmitBatch(ctx, []string{"song|us|1|100"}, nil).Results[0]
	if again.Status != domain.SubmitDuplicateActive || again.ExistingJobID != song.Job.ID {
		t.Fatalf("the same song again = %+v, want duplicate_active for %s", again, song.Job.ID)
	}

	close(p.hold)
	waitRun(t, store, song.Job.ID)
	waitAlbumDone(t, store, "100", 2)
}

// A song link without its album names a song an album record already
// lists: it is added there at once, without a catalog read, and answers
// already_downloaded when its track is done, or queues a run for it.
func TestSongLinkOfAHeldSongAnswersAtOnce(t *testing.T) {
	p := &recordProcessor{dir: t.TempDir(), tracks: []string{"1", "2"}}
	m, store := newRecordManager(t, p, config.Default())
	ctx := context.Background()
	startManager(t, m)

	first := m.SubmitBatch(ctx, []string{"song|us|1|100"}, nil).Results[0]
	waitRun(t, store, first.Job.ID)

	held := m.SubmitBatch(ctx, []string{"song|us|1"}, nil).Results[0]
	if held.Status != domain.SubmitAlreadyDownloaded || held.Resource == nil || held.Resource.ID != "100" {
		t.Fatalf("bare link of a downloaded song = %+v, want already_downloaded naming album 100", held)
	}
	other := m.SubmitBatch(ctx, []string{"song|us|2"}, nil).Results[0]
	if other.Status != domain.SubmitAccepted || other.Job == nil {
		t.Fatalf("bare link of a listed song = %+v, want an accepted run", other)
	}
	if pending, err := store.ListPending(ctx, ""); err != nil || len(pending) != 0 {
		t.Fatalf("pending adds = %+v (%v), want none", pending, err)
	}
	waitRun(t, store, other.Job.ID)
	if f := fileState(t, store, "100", "2", domain.SlotMain); f.AudioState != domain.AudioDone {
		t.Fatalf("song 2 = %s, want done", f.AudioState)
	}
}
