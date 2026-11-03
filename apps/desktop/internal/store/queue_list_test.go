package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueFake serves the record lists by activity, a page at a time, and
// the station runs, as the backend does.
type queueFake struct {
	mu        sync.Mutex
	albums    []client.AlbumRecord
	playlists []client.PlaylistRecord
	stations  []domain.Job
	reads     map[string]int
}

func (f *queueFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	page := func(n int) (int, int) {
		from := min(offset, n)
		to := n
		if limit > 0 {
			to = min(from+limit, n)
		}
		return from, to
	}
	reply := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	f.reads[r.URL.Path]++
	switch r.URL.Path {
	case "/api/v1/library/albums":
		var items []client.AlbumRecord
		for _, a := range f.albums {
			if q.Get("state") == "" || string(a.State) == q.Get("state") {
				items = append(items, a)
			}
		}
		from, to := page(len(items))
		reply(map[string]any{"items": items[from:to], "total": len(items)})
	case "/api/v1/library/playlists":
		from, to := page(len(f.playlists))
		reply(map[string]any{"items": f.playlists[from:to], "total": len(f.playlists)})
	case "/api/v1/downloads":
		from, to := page(len(f.stations))
		reply(client.DownloadList{Downloads: f.stations[from:to], Total: len(f.stations)})
	case "/api/v1/library/summary":
		reply(client.LibrarySummary{})
	case "/api/v1/library/pending":
		reply(map[string]any{"items": []any{}})
	default:
		http.NotFound(w, r)
	}
}

// The queue's All tab merges the album and playlist records and the
// stations' runs by activity, one row per record, and shows rows only as
// far as every list not read to its end goes; the next page fills in what
// it held back, and a fresh read keeps every page read.
func TestTheQueueMergesRecordsAndStationsByActivity(t *testing.T) {
	t0 := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	f := &queueFake{reads: map[string]int{}}
	for i := range 150 {
		a := client.AlbumRecord{ActivityAt: t0.Add(-time.Duration(2*i) * time.Minute)}
		a.ID, a.Name, a.State = fmt.Sprint("a", i), fmt.Sprint("Album ", i), domain.RecordComplete
		f.albums = append(f.albums, a)
	}
	for i, ago := range []time.Duration{1, 201, 401} {
		p := client.PlaylistRecord{ActivityAt: t0.Add(-ago * time.Minute)}
		p.ID, p.Name = fmt.Sprint("pl.", i), fmt.Sprint("Playlist ", i)
		f.playlists = append(f.playlists, p)
	}
	f.stations = []domain.Job{{ID: "st", Type: "station", Status: domain.JobCompleted, UpdatedAt: t0.Add(-3 * time.Minute)}}
	s, settle := live(t, f)
	q := &s.Queue
	s.OpenQueue(TabAll)
	settle(func() bool { return q.List(TabAll).Status == SearchReady && q.counted })

	rows, more := s.QueueRows(TabAll)
	keys := func(rows []QueueRow) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Key())
		}
		return out
	}
	got := keys(rows)
	if !more || len(rows) != 102 {
		t.Fatalf("first read: %d rows (more %v), want 102: the 100 albums, the playlist and the station within them", len(rows), more)
	}
	if want := []string{"album:a0", "playlist:pl.0", "album:a1", "run:st", "album:a2"}; fmt.Sprint(got[:5]) != fmt.Sprint(want) {
		t.Errorf("first rows = %v, want %v", got[:5], want)
	}
	for _, k := range got {
		if k == "playlist:pl.1" {
			t.Errorf("a playlist older than the albums read shows before their next page")
		}
	}

	s.MoreQueue(TabAll)
	settle(func() bool {
		return q.List(TabAll).More == PageIdle && len(q.tabs[TabAll].streams[streamAlbums].items) == 150
	})
	rows, more = s.QueueRows(TabAll)
	if more || len(rows) != 154 {
		t.Fatalf("after the next page: %d rows (more %v), want every one of 154", len(rows), more)
	}
	if got := keys(rows); got[103] != "playlist:pl.1" || got[153] != "playlist:pl.2" {
		t.Errorf("the playlists past the first page sit at %v and %v", got[103], got[153])
	}

	// A fresh read, as after an action, keeps both pages.
	view := q.tabs[TabAll].views[streamAlbums]
	s.recordsChanged()
	settle(func() bool { return q.tabs[TabAll].views[streamAlbums] > view && !q.tabs[TabAll].busy })
	if rows, _ := s.QueueRows(TabAll); len(rows) != 154 {
		t.Errorf("a fresh read shows %d rows, want the 154 read", len(rows))
	}

	// Two runs of one record are one row; one under way puts it in Up
	// next, out of the rows; the feed moves a record to its new activity.
	done := t0.Add(-time.Hour)
	s.ApplySnapshot(client.DownloadList{Downloads: []domain.Job{
		{ID: "r1", Type: "album", Status: domain.JobCompleted, ResourceKind: domain.ResourceAlbum, ResourceID: "a0", FinishedAt: &done},
		{ID: "r2", Type: "album", Status: domain.JobCompleted, ResourceKind: domain.ResourceAlbum, ResourceID: "a0", FinishedAt: &done},
		{ID: "r3", Type: "album", Status: domain.JobRunning, ResourceKind: domain.ResourceAlbum, ResourceID: "a1"},
	}})
	a1 := f.albums[1]
	a1.ActiveJobID, a1.ActivityAt = "r3", t0.Add(time.Minute)
	s.applyMessage(domain.DownloadFeedMessage{Type: "resource_updated", ResourceKind: domain.ResourceAlbum, ResourceID: "a1", Resource: a1})
	a120 := f.albums[120]
	a120.ActivityAt = t0.Add(2 * time.Minute)
	s.applyMessage(domain.DownloadFeedMessage{Type: "resource_updated", ResourceKind: domain.ResourceAlbum, ResourceID: "a120", Resource: a120})
	rows, _ = s.QueueRows(TabAll)
	got = keys(rows)
	if got[0] != "album:a120" || got[1] != "album:a0" {
		t.Errorf("after the feed, rows start %v, want a120 then a0", got[:3])
	}
	n := map[string]int{}
	for _, k := range got {
		n[k]++
	}
	if n["album:a0"] != 1 || n["album:a1"] != 0 {
		t.Errorf("a0 shows %d times, a1 (running) %d times", n["album:a0"], n["album:a1"])
	}

	// A run a download queued leaves the rows at once, before the feed
	// sends its record.
	s.upsert(domain.Job{ID: "r4", Type: "album", Status: domain.JobQueued, ResourceKind: domain.ResourceAlbum, ResourceID: "a2"})
	s.noteSubmitted(domain.BatchSubmitResponse{Results: []domain.SubmitResult{{Status: domain.SubmitAccepted,
		Resource: &domain.ResourceRef{Kind: domain.ResourceAlbum, ID: "a2"}, Job: &domain.Job{ID: "r4"}}}})
	rows, _ = s.QueueRows(TabAll)
	for _, r := range rows {
		if r.Key() == "album:a2" {
			t.Errorf("a2 shows in the rows while its run waits in Up next")
		}
	}
}

// The Failed tab lists the records with failed, given-up or missing tracks
// in one read and counts them with the failed adds; Done counts complete
// records and finished stations.
func TestTheQueueCountsDoneAndFailed(t *testing.T) {
	var flags []string
	var mu sync.Mutex
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v1/library/albums", "/api/v1/library/playlists":
			mu.Lock()
			flags = append(flags, strings.Join(q["flag"], ","))
			mu.Unlock()
			n := 0
			if q.Get("flag") != "" && r.URL.Path == "/api/v1/library/albums" {
				n = 4
			}
			reply(map[string]any{"items": []any{}, "total": n})
		case "/api/v1/downloads":
			n := 0
			if q.Get("status") == "completed" {
				n = 2
			}
			reply(client.DownloadList{Downloads: []domain.Job{}, Total: n})
		case "/api/v1/library/summary":
			sum := client.LibrarySummary{}
			sum.Albums.States = map[domain.RecordState]int{domain.RecordComplete: 7}
			sum.Playlists.States = map[domain.RecordState]int{domain.RecordComplete: 1}
			reply(sum)
		case "/api/v1/library/pending":
			reply(map[string]any{"items": []domain.PendingAdd{{ID: 1, State: domain.PendingAddFailed}, {ID: 2, State: domain.PendingAddPending}}})
		default:
			http.NotFound(w, r)
		}
	})
	s, settle := live(t, h)
	s.OpenQueue(TabAll)
	settle(func() bool { return s.Queue.counted })
	if d, f := s.QueueCount(TabDone), s.QueueCount(TabFailed); d != 10 || f != 5 {
		t.Errorf("Done · %d, Failed · %d; want 10 (8 records, 2 stations) and 5 (4 records, 1 add)", d, f)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, fl := range flags {
		if fl != "" && fl != "failed,given_up,missing" {
			t.Errorf("a record list read with flag %q, want the three in one read", fl)
		}
	}
}
