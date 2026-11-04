package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"amdl/internal/domain"
)

// The consistency tests act on a real backend (simulated downloads) the way
// the desktop app and the console do, and after each action read every
// surface a client shows: queue rows and tab counts, the feed's runs,
// counts and record marks, GET /library/summary, the record lists, the
// record itself, the album page and search marks. They must agree.
//
// The clients derive their surfaces from these reads (apps/desktop/internal
// /store, apps/console/static/js/lib): Active is status_counts queued plus
// running; Done is the complete records; Failed is the records with a
// failed, given-up or missing track; a mark is {state, tracks_done,
// tracks_wanted, wanted_all, paused, active_job_id}, overlaid by the feed's
// latest resource_updated.

// recordView is the part of a record every surface shows.
type recordView struct {
	State        domain.RecordState `json:"state"`
	TracksDone   int                `json:"tracks_done"`
	TracksWanted int                `json:"tracks_wanted"`
	WantedAll    bool               `json:"wanted_all"`
	Paused       bool               `json:"paused"`
	ActiveJobID  string             `json:"active_job_id"`
}

type listedRecord struct {
	ID string `json:"id"`
	recordView
	TracksFailed  int                 `json:"tracks_failed"`
	TracksGivenUp int                 `json:"tracks_given_up"`
	TracksMissing int                 `json:"tracks_missing"`
	CatalogState  domain.CatalogState `json:"catalog_state"`
}

// failed reports whether the record belongs on the Failed tab: a track
// failed, was given up or is missing, or Apple does not list the record.
func (l listedRecord) failed() bool {
	return l.TracksFailed+l.TracksGivenUp+l.TracksMissing > 0 || l.CatalogState != domain.CatalogOK
}

type detailTrack struct {
	SongID string `json:"song_id"`
	Wanted bool   `json:"wanted"`
	Files  []struct {
		Slot       string            `json:"slot"`
		AudioState domain.AudioState `json:"audio_state"`
	} `json:"files"`
}

func (t detailTrack) main() domain.AudioState {
	for _, f := range t.Files {
		if f.Slot == domain.SlotMain {
			return f.AudioState
		}
	}
	return ""
}

type recordCounts struct {
	Total  int                        `json:"total"`
	States map[domain.RecordState]int `json:"states"`
	Flags  map[string]int             `json:"flags"`
}

// surfaces is one read of every surface, taken after the backend settled.
type surfaces struct {
	runs      []domain.Job
	counts    domain.JobStatusCounts
	total     int
	summary   struct{ Albums, Playlists recordCounts }
	albums    map[string]listedRecord
	playlists map[string]listedRecord
	doneTotal int
	failTotal int
	// failedFlag is the total of ?flag=failed alone.
	failedFlag int
	needsRuns  []domain.Job
	// orphans are the runs of no record, as the queues read them beside the
	// records (resource_kind=none).
	orphans   []domain.Job
	waitTotal int
	stats     struct {
		Totals struct {
			TracksCompleted int `json:"tracks_completed"`
		} `json:"totals"`
		Codecs []struct {
			Codec  string `json:"codec"`
			Tracks int    `json:"tracks"`
		} `json:"codecs"`
	}
	details map[string]*struct {
		recordView
		Tracks []detailTrack `json:"tracks"`
	}
	pageMarks  map[string]*recordView
	pageTracks map[string]map[string]domain.AudioState
	hitMarks   map[string]*recordView
	songMarks  map[string]*domain.ResourceMark
	feedRuns   map[string]domain.Job
	feedCounts domain.JobStatusCounts
	feedMarks  map[string]*recordView
	feedGone   map[string]bool
}

func (r *rig) read() surfaces {
	r.t.Helper()
	var s surfaces
	var list struct {
		Downloads    []domain.Job           `json:"downloads"`
		StatusCounts domain.JobStatusCounts `json:"status_counts"`
		Total        int                    `json:"total"`
	}
	r.call(http.MethodGet, "/downloads?limit=200", "", http.StatusOK, &list)
	s.runs, s.counts, s.total = list.Downloads, list.StatusCounts, list.Total
	r.call(http.MethodGet, "/library/summary", "", http.StatusOK, &s.summary)
	s.albums, s.playlists = map[string]listedRecord{}, map[string]listedRecord{}
	for kind, into := range map[string]map[string]listedRecord{"albums": s.albums, "playlists": s.playlists} {
		var page struct {
			Items []listedRecord `json:"items"`
			Total int            `json:"total"`
		}
		r.call(http.MethodGet, "/library/"+kind+"?limit=500", "", http.StatusOK, &page)
		for _, it := range page.Items {
			into[it.ID] = it
		}
		r.call(http.MethodGet, "/library/"+kind+"?state=complete&limit=1", "", http.StatusOK, &page)
		s.doneTotal += page.Total
		r.call(http.MethodGet, "/library/"+kind+"?flag=failed&flag=given_up&flag=missing&limit=1", "", http.StatusOK, &page)
		s.failTotal += page.Total
		r.call(http.MethodGet, "/library/"+kind+"?flag=failed&limit=1", "", http.StatusOK, &page)
		s.failedFlag += page.Total
		r.call(http.MethodGet, "/library/"+kind+"?flag=waiting&limit=1", "", http.StatusOK, &page)
		s.waitTotal += page.Total
	}
	// GET /stats keeps an answer per days value for a while: each read asks
	// for a days value not read before.
	r.statsDays++
	r.call(http.MethodGet, fmt.Sprintf("/stats?days=%d", r.statsDays), "", http.StatusOK, &s.stats)
	var needs struct {
		Downloads []domain.Job `json:"downloads"`
	}
	r.call(http.MethodGet, "/downloads?status=failed&current=true&limit=200", "", http.StatusOK, &needs)
	s.needsRuns = needs.Downloads
	r.call(http.MethodGet, "/downloads?resource_kind=none&limit=200", "", http.StatusOK, &needs)
	s.orphans = needs.Downloads

	s.details = map[string]*struct {
		recordView
		Tracks []detailTrack `json:"tracks"`
	}{}
	s.pageMarks, s.pageTracks = map[string]*recordView{}, map[string]map[string]domain.AudioState{}
	for _, id := range r.albumIDs() {
		if r.status(http.MethodGet, "/library/albums/"+id, "") == http.StatusOK {
			d := &struct {
				recordView
				Tracks []detailTrack `json:"tracks"`
			}{}
			r.call(http.MethodGet, "/library/albums/"+id, "", http.StatusOK, d)
			s.details[id] = d
		}
		var page struct {
			Resource *recordView `json:"resource"`
			Tracks   []struct {
				ID    string            `json:"id"`
				State domain.AudioState `json:"state"`
			} `json:"tracks"`
		}
		r.call(http.MethodGet, "/albums/"+id+"?storefront=us", "", http.StatusOK, &page)
		s.pageMarks[id] = page.Resource
		s.pageTracks[id] = map[string]domain.AudioState{}
		for _, tr := range page.Tracks {
			s.pageTracks[id][tr.ID] = tr.State
		}
	}
	var search struct {
		Albums struct {
			Items []struct {
				ID       string      `json:"id"`
				Resource *recordView `json:"resource"`
			} `json:"items"`
		} `json:"albums"`
		Songs struct {
			Items []struct {
				ID       string               `json:"id"`
				Resource *domain.ResourceMark `json:"resource"`
			} `json:"items"`
		} `json:"songs"`
	}
	r.call(http.MethodGet, "/search?term=x&types=albums,songs&storefront=us", "", http.StatusOK, &search)
	s.hitMarks, s.songMarks = map[string]*recordView{}, map[string]*domain.ResourceMark{}
	for _, h := range search.Albums.Items {
		s.hitMarks[h.ID] = h.Resource
	}
	for _, h := range search.Songs.Items {
		s.songMarks[h.ID] = h.Resource
	}

	r.feed.mu.Lock()
	defer r.feed.mu.Unlock()
	s.feedRuns = map[string]domain.Job{}
	for id, job := range r.feed.runs {
		s.feedRuns[id] = job
	}
	s.feedCounts = r.feed.counts
	s.feedMarks, s.feedGone = map[string]*recordView{}, map[string]bool{}
	for key, raw := range r.feed.records {
		var v recordView
		if err := json.Unmarshal(raw, &v); err != nil {
			r.t.Fatalf("feed record %s: %v", key, err)
		}
		s.feedMarks[key] = &v
	}
	for key := range r.feed.gone {
		s.feedGone[key] = true
	}
	return s
}

func (r *rig) albumIDs() []string {
	r.catalog.mu.Lock()
	defer r.catalog.mu.Unlock()
	out := make([]string, 0, len(r.catalog.albums))
	for id := range r.catalog.albums {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// knownDefects names, per check of one step, the checklist item whose
// defect fails it; the item's fix removes the entry.
type knownDefects map[string]string

// check reads every surface and asserts that they agree, each kind of
// agreement in its own subtest so one known defect hides no other.
func (r *rig) check(step string, known ...knownDefects) surfaces {
	r.t.Helper()
	r.settled()
	s := r.read()
	run := func(name string, f func(t *testing.T)) {
		r.t.Run(step+"/"+name, func(t *testing.T) {
			for _, k := range known {
				if item, ok := k[name]; ok {
					t.Skipf("known defect, checklist %s", item)
				}
			}
			f(t)
		})
	}
	run("queue", func(t *testing.T) {
		hist := domain.JobStatusCounts{Total: len(s.runs)}
		active := map[string]string{}
		for _, j := range s.runs {
			switch j.Status {
			case domain.JobQueued:
				hist.Queued++
			case domain.JobRunning:
				hist.Running++
			case domain.JobCompleted:
				hist.Completed++
			case domain.JobFailed:
				hist.Failed++
			case domain.JobCancelled:
				hist.Cancelled++
			}
			if j.ResourceID != "" && !j.Status.IsTerminal() {
				key := recordKeyOf(j.ResourceKind, j.ResourceID)
				if other, ok := active[key]; ok {
					t.Errorf("record %s has two active runs: %s and %s", key, other, j.ID)
				}
				active[key] = j.ID
			}
		}
		if s.counts != hist || s.total != len(s.runs) {
			t.Errorf("GET /downloads status_counts %+v total %d, rows say %+v", s.counts, s.total, hist)
		}
		for id, rec := range s.albums {
			if want := active[recordKeyOf(domain.ResourceAlbum, id)]; rec.ActiveJobID != want {
				t.Errorf("album %s active_job_id %q, the queue's active run %q", id, rec.ActiveJobID, want)
			}
		}
		// Every run shows in a queue: as its record's row, or as a run of
		// no record.
		orphan := map[string]bool{}
		for _, j := range s.orphans {
			orphan[j.ID] = true
			if j.ResourceID != "" {
				t.Errorf("run %s of record %s is listed as a run of no record", j.ID, j.ResourceID)
			}
		}
		for _, j := range s.runs {
			set := s.albums
			if j.ResourceKind == domain.ResourcePlaylist {
				set = s.playlists
			}
			if _, listed := set[j.ResourceID]; (j.ResourceID == "" && !orphan[j.ID]) || (j.ResourceID != "" && !listed) {
				t.Errorf("run %s (%s, record %q) shows in no queue", j.ID, j.Type, j.ResourceID)
			}
		}
	})
	run("feed runs", func(t *testing.T) {
		if s.feedCounts != s.counts {
			t.Errorf("feed status_counts %+v, GET /downloads %+v", s.feedCounts, s.counts)
		}
		listed := map[string]domain.JobStatus{}
		for _, j := range s.runs {
			listed[j.ID] = j.Status
			if f, ok := s.feedRuns[j.ID]; !ok {
				t.Errorf("run %s (%s) is listed but the feed never sent it", j.ID, j.Status)
			} else if f.Status != j.Status {
				t.Errorf("run %s: feed %s, GET /downloads %s", j.ID, f.Status, j.Status)
			}
		}
		for id, j := range s.feedRuns {
			if _, ok := listed[id]; !ok {
				t.Errorf("the feed holds run %s (%s) that GET /downloads no longer lists", id, j.Status)
			}
		}
	})
	run("tab counts", func(t *testing.T) {
		done := s.summary.Albums.States[domain.RecordComplete] + s.summary.Playlists.States[domain.RecordComplete]
		listedDone, listedFail := 0, 0
		for _, set := range []map[string]listedRecord{s.albums, s.playlists} {
			for _, rec := range set {
				if rec.State == domain.RecordComplete {
					listedDone++
				}
				if rec.failed() {
					listedFail++
				}
			}
		}
		if done != s.doneTotal || done != listedDone {
			t.Errorf("Done: summary %d, ?state=complete total %d, listed complete %d", done, s.doneTotal, listedDone)
		}
		if s.failTotal != listedFail {
			t.Errorf("Failed: ?flag= total %d, listed with failed tracks or not listed by Apple %d", s.failTotal, listedFail)
		}
		if failed := s.summary.Albums.Flags["failed"] + s.summary.Playlists.Flags["failed"]; failed != s.failedFlag {
			t.Errorf("summary failed %d, ?flag=failed total %d", failed, s.failedFlag)
		}
		if n := s.summary.Albums.Total + s.summary.Playlists.Total; n != len(s.albums)+len(s.playlists) {
			t.Errorf("summary totals %d, listed records %d", n, len(s.albums)+len(s.playlists))
		}
		paused := s.summary.Albums.Flags["paused"] + s.summary.Playlists.Flags["paused"]
		listedPaused := 0
		for _, set := range []map[string]listedRecord{s.albums, s.playlists} {
			for _, rec := range set {
				if rec.Paused {
					listedPaused++
				}
			}
		}
		if paused != listedPaused {
			t.Errorf("summary paused %d, listed paused %d", paused, listedPaused)
		}
		if waiting := s.summary.Albums.Flags["waiting"] + s.summary.Playlists.Flags["waiting"]; waiting != s.waitTotal {
			t.Errorf("summary waiting %d, ?flag=waiting total %d", waiting, s.waitTotal)
		}
	})
	run("needs attention", func(t *testing.T) {
		for _, j := range s.needsRuns {
			set := s.albums
			if j.ResourceKind == domain.ResourcePlaylist {
				set = s.playlists
			}
			rec, ok := set[j.ResourceID]
			if j.ResourceID == "" || !ok {
				continue
			}
			if rec.State == domain.RecordComplete && !rec.failed() {
				t.Errorf("failed run %s needs attention, yet its record %s is complete with nothing failed", j.ID, j.ResourceID)
			}
		}
	})
	run("stats", func(t *testing.T) {
		sum := 0
		for _, c := range s.stats.Codecs {
			sum += c.Tracks
			if c.Codec == "" {
				t.Errorf("stats count %d tracks of no codec", c.Tracks)
			}
		}
		if sum != s.stats.Totals.TracksCompleted {
			t.Errorf("stats: %d tracks completed, %d by codec", s.stats.Totals.TracksCompleted, sum)
		}
	})
	run("marks", func(t *testing.T) {
		for _, id := range r.albumIDs() {
			listed, inList := s.albums[id]
			detail := s.details[id]
			page, hit := s.pageMarks[id], s.hitMarks[id]
			if !inList {
				if detail != nil || page != nil || hit != nil {
					t.Errorf("album %s is not listed, yet detail %v, page mark %v, search mark %v", id, detail != nil, page, hit)
				}
				continue
			}
			if detail == nil {
				t.Errorf("album %s is listed but GET /library/albums/%s fails", id, id)
				continue
			}
			want := listed.recordView
			for name, got := range map[string]*recordView{"detail": &detail.recordView, "album page": page, "search": hit} {
				if got == nil || *got != want {
					t.Errorf("album %s: %s %+v, list %+v", id, name, got, want)
				}
			}
			for _, tr := range detail.Tracks {
				if !tr.Wanted {
					continue
				}
				if got := s.pageTracks[id][tr.SongID]; got != tr.main() {
					t.Errorf("album %s song %s: album page %q, record %q", id, tr.SongID, got, tr.main())
				}
				if m := s.songMarks[tr.SongID]; m == nil || m.SongState != tr.main() {
					t.Errorf("album %s song %s: search song mark %+v, record %q", id, tr.SongID, m, tr.main())
				}
			}
		}
	})
	run("feed records", func(t *testing.T) {
		for key, got := range s.feedMarks {
			kind, id, _ := strings.Cut(key, "/")
			set := s.albums
			if kind == string(domain.ResourcePlaylist) {
				set = s.playlists
			}
			listed, ok := set[id]
			if !ok {
				t.Errorf("the feed still holds record %s, which is gone", key)
				continue
			}
			if *got != listed.recordView {
				t.Errorf("record %s: feed %+v, list %+v", key, *got, listed.recordView)
			}
		}
	})
	return s
}

// albumFile is the content of song's main file in album, "" when absent.
func (r *rig) albumFile(album, song string) string {
	raw, err := os.ReadFile(filepath.Join(r.proc.dir, album, song+"-"+domain.SlotMain+".m4a"))
	if err != nil {
		return ""
	}
	return string(raw)
}

func runOf(results []domain.SubmitResult) string {
	if len(results) != 1 || results[0].Job == nil {
		return ""
	}
	return results[0].Job.ID
}

func (r *rig) waitStarted() string {
	r.t.Helper()
	select {
	case id := <-r.proc.started:
		return id
	case <-time.After(10 * time.Second):
		r.t.Fatal("no run started")
		return ""
	}
}

// TestRecordActionsKeepSurfacesConsistent walks the actions lyjw named for
// checklist i19 (delete, download again, a song then its album, an album
// then one song, pause and resume, the overwrite setting) and checks every
// surface after each.
func TestRecordActionsKeepSurfacesConsistent(t *testing.T) {
	r := newRig(t, map[string][]string{
		"100": {"11", "12"},
		"200": {"21", "22"},
		"300": {"31", "32"},
		"400": {"41", "42"},
	}, map[string][]string{"pl.1": {"11", "51"}})

	r.add("album|us|100")
	s := r.check("album added")
	if a := s.albums["100"]; a.State != domain.RecordComplete || !a.WantedAll || a.TracksDone != 2 {
		t.Fatalf("album 100 = %+v, want complete with both tracks", a)
	}

	r.add("song|us|21|200")
	s = r.check("one song")
	if a := s.albums["200"]; a.State != domain.RecordComplete || a.WantedAll || a.TracksDone != 1 {
		t.Fatalf("album 200 after one song = %+v, want complete in itself without wanted_all", a)
	}
	r.add("album|us|200")
	s = r.check("then its album")
	if a := s.albums["200"]; !a.WantedAll || a.TracksDone != 2 {
		t.Fatalf("album 200 after the album = %+v, want both tracks", a)
	}

	r.add("album|us|300")
	r.check("album first")
	res := r.add("song|us|31|300")
	if len(res) != 1 || res[0].Status != domain.SubmitAlreadyDownloaded {
		t.Fatalf("one song of a downloaded album = %+v, want already_downloaded", res)
	}
	r.check("then one of its songs")

	r.add("playlist|us|pl.1")
	r.check("playlist added")

	r.add("station|us|ra.1")
	if s = r.check("a station"); len(s.orphans) != 1 || s.orphans[0].Type != "station" {
		t.Fatalf("runs of no record = %+v, want the station's", s.orphans)
	}

	before := r.albumFile("100", "11")
	var reset struct {
		Job *domain.Job `json:"job"`
	}
	r.call(http.MethodPost, "/library/albums/100/reset", `{"what":"all"}`, http.StatusOK, &reset)
	if reset.Job == nil {
		t.Fatal("download again queued no run")
	}
	s = r.check("download again")
	if after := r.albumFile("100", "11"); after == before || after != reset.Job.ID {
		t.Fatalf("download again left the file %q (was %q), want it written by %s", after, before, reset.Job.ID)
	}
	if s.albums["100"].State != domain.RecordComplete {
		t.Fatalf("album 100 after download again = %+v", s.albums["100"])
	}

	var runs struct {
		Downloads []domain.Job `json:"downloads"`
	}
	r.call(http.MethodGet, "/downloads?resource_kind=album&resource_id=100&status=completed", "", http.StatusOK, &runs)
	if len(runs.Downloads) < 2 {
		t.Fatalf("album 100 completed runs = %d, want the first and download again", len(runs.Downloads))
	}
	oldest := runs.Downloads[len(runs.Downloads)-1].ID
	r.call(http.MethodDelete, "/downloads/"+oldest, "", http.StatusOK, nil)
	r.check("a finished run deleted")

	for _, overwrite := range []bool{false, true} {
		step := fmt.Sprintf("overwrite %v", overwrite)
		r.call(http.MethodDelete, "/library/albums/100", "", http.StatusNoContent, nil)
		s = r.check(step + ": record deleted")
		if _, ok := s.albums["100"]; ok || s.pageMarks["100"] != nil || s.hitMarks["100"] != nil {
			t.Fatalf("%s: album 100 still shows after its delete", step)
		}
		kept := r.albumFile("100", "11")
		if kept == "" {
			t.Fatalf("%s: the record delete removed the file", step)
		}
		cfg := r.cfg.Get()
		cfg.Download.OverwriteExisting = overwrite
		r.cfg.Set(cfg)
		job := runOf(r.add("album|us|100"))
		s = r.check(step + ": added again")
		if s.albums["100"].State != domain.RecordComplete {
			t.Fatalf("%s: album 100 = %+v, want complete", step, s.albums["100"])
		}
		if got := r.albumFile("100", "11"); overwrite != (got == job) || (!overwrite && got != kept) {
			t.Fatalf("%s: file %q (kept %q, run %s)", step, got, kept, job)
		}
	}

	release := r.proc.holdRuns()
	defer release()
	r.add("album|us|400")
	r.waitStarted()
	r.call(http.MethodPost, "/library/albums/400/pause", "", http.StatusOK, nil)
	release()
	s = r.check("paused while running")
	if a := s.albums["400"]; !a.Paused || a.ActiveJobID != "" {
		t.Fatalf("album 400 after pause = %+v, want paused without a run", a)
	}
	r.call(http.MethodPost, "/library/albums/400/resume", "", http.StatusOK, nil)
	s = r.check("resumed")
	if a := s.albums["400"]; a.Paused || a.State != domain.RecordComplete {
		t.Fatalf("album 400 after resume = %+v, want complete", a)
	}

	r.call(http.MethodPost, "/library/albums/300/pause", "", http.StatusOK, nil)
	s = r.check("complete record paused")
	if a := s.albums["300"]; !a.Paused || a.State != domain.RecordComplete {
		t.Fatalf("album 300 = %+v, want paused and complete", a)
	}
	r.call(http.MethodPost, "/library/albums/300/resume", "", http.StatusOK, nil)
	r.check("complete record resumed")

	release = r.proc.holdRuns()
	r.call(http.MethodPost, "/library/albums/400/reset", `{"what":"all"}`, http.StatusOK, nil)
	r.waitStarted()
	r.call(http.MethodPost, "/library/albums/400/pause", "", http.StatusOK, nil)
	t.Run("delete right after pause", func(t *testing.T) {
		if code := r.status(http.MethodDelete, "/library/albums/400", ""); code != http.StatusNoContent {
			t.Errorf("delete right after pause = %d, want 204", code)
		}
	})
	release()
	r.check("deleted after pause")
	if r.status(http.MethodGet, "/library/albums/400", "") == http.StatusOK {
		r.call(http.MethodDelete, "/library/albums/400", "", http.StatusNoContent, nil)
	}
	r.check("record deleted")

	r.call(http.MethodGet, "/downloads?resource_kind=album&resource_id=200", "", http.StatusOK, &runs)
	var gone struct {
		Deleted  string              `json:"deleted"`
		Resource *domain.ResourceRef `json:"resource"`
	}
	r.call(http.MethodDelete, "/downloads/"+runs.Downloads[0].ID+"?record=true", "", http.StatusOK, &gone)
	if gone.Deleted != "record" || gone.Resource == nil || gone.Resource.ID != "200" {
		t.Fatalf("deleting a run with record=true = %+v, want its record 200", gone)
	}
	s = r.check("a run deleted with its record")
	if _, ok := s.albums["200"]; ok {
		t.Fatal("album 200 is still listed after its run was deleted with record=true")
	}

	r.call(http.MethodGet, "/downloads?resource_kind=playlist&resource_id=pl.1", "", http.StatusOK, &runs)
	if len(runs.Downloads) == 0 || len(s.orphans) != 1 {
		t.Fatalf("runs of playlist pl.1 = %d, of no record = %d", len(runs.Downloads), len(s.orphans))
	}
	playlistRun, stationRun := runs.Downloads[0].ID, s.orphans[0].ID
	r.corruptOverrides(playlistRun, stationRun)
	s = r.check("runs whose overrides do not decode")
	if len(s.orphans) != 1 || s.playlists["pl.1"].ID == "" {
		t.Fatalf("after their overrides broke: runs of no record %+v, playlist %+v", s.orphans, s.playlists["pl.1"])
	}
	for _, id := range []string{playlistRun, stationRun} {
		var refused struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		r.call(http.MethodPost, "/downloads/"+id+"/retry", "", http.StatusConflict, &refused)
		if refused.Error.Code != "job_not_retryable" || strings.Contains(refused.Error.Message, "decode") {
			t.Errorf("retry of run %s whose overrides do not decode = %+v, want job_not_retryable saying why in words", id, refused.Error)
		}
	}
	r.call(http.MethodDelete, "/downloads/"+playlistRun+"?record=true", "", http.StatusOK, &gone)
	if gone.Deleted != "record" || gone.Resource == nil || gone.Resource.ID != "pl.1" {
		t.Fatalf("deleting a run whose overrides do not decode with record=true = %+v, want playlist pl.1", gone)
	}
	r.call(http.MethodDelete, "/downloads/"+stationRun+"?record=true", "", http.StatusOK, &gone)
	s = r.check("they are deleted")
	if _, ok := s.playlists["pl.1"]; ok || len(s.orphans) != 0 {
		t.Fatalf("after the deletes: playlist pl.1 listed %v, runs of no record %+v", ok, s.orphans)
	}
}

// needsAttention reports whether a failed run of album id is among the runs
// the console's Needs attention lists.
func (s surfaces) needsAttention(id string) bool {
	for _, j := range s.needsRuns {
		if j.ResourceKind == domain.ResourceAlbum && j.ResourceID == id {
			return true
		}
	}
	return false
}

// TestStuckAndUnseenStatesClear walks the states checklist i16 names: a
// record its storefront did not have, a track no codec offers, a playlist
// song gone while its failure waits, an extra quality that failed, and a
// lyrics refresh; every surface agrees after each.
func TestStuckAndUnseenStatesClear(t *testing.T) {
	r := newRig(t, map[string][]string{
		"600": {"61", "62"},
		"700": {"71"},
		"100": {"11", "12"},
	}, map[string][]string{"pl.2": {"81", "82"}})

	r.add("album|us|500")
	s := r.check("album not in the storefront")
	if a, ok := s.albums["500"]; !ok || a.State != domain.RecordNone {
		t.Errorf("album 500 = %+v (%v), want a record with nothing downloaded", a, ok)
	}
	if a := s.albums["500"]; a.CatalogState != domain.CatalogNotInStorefront || s.failedFlag != 1 || !s.needsAttention("500") {
		t.Errorf("album 500 %s: ?flag=failed total %d, its run needs attention %v; want it failed on every surface",
			a.CatalogState, s.failedFlag, s.needsAttention("500"))
	}
	r.catalog.setAlbum("500", []string{"51"})
	if code := r.status(http.MethodPost, "/library/albums/500/download", ""); code != http.StatusAccepted {
		t.Errorf("Download of the album its storefront has now = %d, want 202 with a run", code)
	}
	s = r.check("downloaded once the storefront has it")
	if a := s.albums["500"]; a.State != domain.RecordComplete {
		t.Errorf("album 500 after Download = %+v, want complete", a)
	}

	r.proc.setUnavailable("62", true)
	r.add("album|us|600")
	s = r.check("a track no codec offers")
	if a := s.albums["600"]; a.State != domain.RecordComplete || a.TracksWanted != 1 {
		t.Errorf("album 600 = %+v, want complete without the track no codec offers", a)
	}

	r.proc.setFailing("82", true)
	r.add("playlist|us|pl.2")
	s = r.check("a playlist song failed")
	if s.waitTotal != 1 {
		t.Errorf("records waiting for a retry = %d, want the playlist", s.waitTotal)
	}
	r.catalog.setPlaylist("pl.2", []string{"81"})
	r.call(http.MethodPost, "/library/playlists/pl.2/reset", `{"what":"audio","songs":["81"]}`, http.StatusOK, nil)
	s = r.check("the failed song left the playlist")
	if p := s.playlists["pl.2"]; s.waitTotal != 0 || p.TracksFailed != 0 || p.State != domain.RecordComplete {
		t.Errorf("after the song left: %d waiting, playlist %+v; want none waiting and the playlist complete", s.waitTotal, p)
	}

	r.add("album|us|700")
	r.check("an album")
	r.proc.setFailing("71/aac", true)
	r.call(http.MethodPost, "/library/albums/700/download", `{"codec":"aac"}`, http.StatusAccepted, nil)
	s = r.check("its extra quality failed")
	if a := s.albums["700"]; a.State != domain.RecordComplete || a.TracksFailed != 1 || !s.needsAttention("700") {
		t.Errorf("album 700 = %+v, needs attention %v; want complete, one failed track, and its run in Needs attention", a, s.needsAttention("700"))
	}

	r.add("album|us|100")
	before := r.check("before a lyrics refresh").stats.Totals.TracksCompleted
	r.call(http.MethodPost, "/library/albums/100/reset", `{"what":"lyrics"}`, http.StatusOK, nil)
	if after := r.check("lyrics refreshed").stats.Totals.TracksCompleted; after != before {
		t.Errorf("tracks completed %d after a lyrics refresh, %d before", after, before)
	}
}
