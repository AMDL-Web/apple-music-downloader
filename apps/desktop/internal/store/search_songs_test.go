package store

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// songBackend answers the search with one song and, once downloaded is set,
// with the album record that holds it and the song done, as the API does;
// a submit of the song is answered pending, with no run, as for a record
// not yet made.
type songBackend struct {
	mu         sync.Mutex
	searches   int
	downloaded bool
}

func (f *songBackend) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v) // the test reads what arrives
	}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/search":
		f.searches++
		h := client.SearchHit{Type: "song", ID: "900", Name: "Song", URL: "https://music.apple.com/cn/song/x/900"}
		if f.downloaded {
			h.Resource = &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "90", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1, SongState: domain.AudioDone}
		}
		reply(http.StatusOK, client.SearchResult{Term: "song", Songs: &client.HitGroup{Items: []client.SearchHit{h}}})
	case "POST /api/v1/downloads":
		reply(http.StatusAccepted, domain.BatchSubmitResponse{Accepted: 1, Results: []domain.SubmitResult{{
			URL: "https://music.apple.com/cn/song/x/900", Status: domain.SubmitPending}}})
	default:
		http.NotFound(w, r)
	}
}

// The feed's record cannot say which of its songs a run finished, and a song
// nothing held names no record: a song downloaded from the search's card
// reads as downloaded once its record moves, not only for another term.
func TestASongDownloadedFromHereReadsDownloadedWhenItsRecordMoves(t *testing.T) {
	f := &songBackend{}
	s, settle := live(t, f)
	s.WantSearch("song")
	settle(func() bool { return s.Search.Status == SearchReady && s.Search.Result != nil })
	song := func() client.SearchHit { return s.Search.Result.Songs.Items[0] }
	if mark, _ := s.HitMark(song()); mark != MarkNone {
		t.Fatalf("before the download the song reads %v", mark)
	}
	s.SubmitHit(song(), nil)
	settle(func() bool { _, on := s.Search.Starting["song:900"]; return !on })
	f.mu.Lock()
	f.downloaded = true
	f.mu.Unlock()
	// The feed brings the album's record, as the run finishes.
	s.putMark(domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "90", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1})
	settle(func() bool { mark, _ := s.HitMark(song()); return mark == MarkDone })
}

// A song's card for words searched before another client downloaded it is
// not served from the cache once its record moved.
func TestARecordMovingDropsTheCachedAnswers(t *testing.T) {
	f := &songBackend{}
	s, settle := live(t, f)
	s.WantSearch("song")
	settle(func() bool { return s.Search.Status == SearchReady })
	s.WantSearch("")
	s.putMark(domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "90", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1})
	f.mu.Lock()
	f.downloaded = true
	f.mu.Unlock()
	s.WantSearch("song")
	settle(func() bool { return s.Search.Status == SearchReady && s.Search.Result != nil })
	if mark, _ := s.HitMark(s.Search.Result.Songs.Items[0]); mark != MarkDone {
		t.Errorf("the same words again read %v from the cache", mark)
	}
}
