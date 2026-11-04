package store

import (
	"encoding/json"
	"net/http"
	"strconv"
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

// pagedSongs answers the search a page at a time, as amp-api does, with
// songs 900 to 955; once downloaded is set, song 910 holds its record's
// mark, and hidden drops it from the search, as a ranking that moved would.
type pagedSongs struct {
	mu         sync.Mutex
	downloaded bool
	hidden     bool
}

func (f *pagedSongs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v) // the test reads what arrives
	}
	mark := &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "91", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1, SongState: domain.AudioDone}
	switch r.Method + " " + r.URL.Path {
	case "GET /api/v1/search":
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit > 50 {
			http.Error(w, "limit above 50", http.StatusBadRequest)
			return
		}
		g := &client.HitGroup{}
		for i := offset; i < min(offset+limit, 56); i++ {
			h := client.SearchHit{Type: "song", ID: strconv.Itoa(900 + i), Name: "Song", URL: "https://music.apple.com/cn/song/x/" + strconv.Itoa(900+i)}
			if h.ID == "910" && f.downloaded {
				if f.hidden {
					continue
				}
				h.Resource = mark
			}
			g.Items = append(g.Items, h)
		}
		if offset+limit < 56 {
			g.NextOffset = offset + limit
		}
		reply(http.StatusOK, client.SearchResult{Term: "song", Songs: g})
	case "POST /api/v1/lookup":
		reply(http.StatusOK, client.Lookup{Type: "song", ID: "910", Resource: mark})
	case "POST /api/v1/downloads":
		reply(http.StatusAccepted, domain.BatchSubmitResponse{Accepted: 1, Results: []domain.SubmitResult{{
			URL: "https://music.apple.com/cn/song/x/910", Status: domain.SubmitPending}}})
	default:
		http.NotFound(w, r)
	}
}

// The Songs tab pages in more songs than one read of the search can ask
// for, and a song downloaded from there still reads downloaded when its
// record moves, whether or not the search ranks it as before.
func TestASongOfManyPagesReadsDownloadedWhenItsRecordMoves(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		f := &pagedSongs{hidden: hidden}
		s, settle := live(t, f)
		s.WantSearch("song")
		settle(func() bool { return s.Search.Status == SearchReady && s.Search.Result != nil })
		for len(s.Search.Result.Songs.Items) < 56 {
			n := len(s.Search.Result.Songs.Items)
			s.MoreSearch("songs")
			settle(func() bool { return len(s.Search.Result.Songs.Items) > n })
		}
		song := func() (h client.SearchHit) {
			for _, x := range s.Search.Result.Songs.Items {
				if x.ID == "910" {
					return x
				}
			}
			t.Fatal("song 910 left the songs shown")
			return
		}
		s.SubmitHit(song(), nil)
		settle(func() bool { _, on := s.Search.Starting["song:910"]; return !on })
		f.mu.Lock()
		f.downloaded = true
		f.mu.Unlock()
		s.putMark(domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "91", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1})
		settle(func() bool { mark, _ := s.HitMark(song()); return mark == MarkDone })
		if got := len(s.Search.Result.Songs.Items); got != 56 {
			t.Errorf("hidden=%v: %d songs shown after the read again, want 56", hidden, got)
		}
	}
}

// A song downloaded from here keeps the queue's mark while its record moves
// from running to complete and the songs read again have not landed: its
// card does not offer the download again for that moment.
func TestASongDownloadedFromHereDoesNotOfferTheDownloadBetweenRunAndRecord(t *testing.T) {
	s := New(func(fn func()) { fn() }, nil)
	h := client.SearchHit{Type: "song", ID: "900", Name: "Song", URL: "https://music.apple.com/cn/song/x/900",
		Resource: &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "90", State: domain.RecordNone, TracksWanted: 1, ActiveJobID: "j", SongState: domain.AudioPending}}
	s.Search.Status = SearchReady
	s.Search.Result = &client.SearchResult{Songs: &client.HitGroup{Items: []client.SearchHit{h}}}
	s.watchSong(h)
	s.putMark(domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "90", State: domain.RecordComplete, TracksDone: 1, TracksWanted: 1})
	// The record's move sends the read of the songs; without a client the
	// test stands in for it.
	s.Search.songReading = 1
	if mark, _ := s.HitMark(h); mark != MarkActive {
		t.Errorf("between the run's end and the record's songs the card reads %v, want the queue's mark", mark)
	}
	other := h
	other.ID = "901"
	if mark, _ := s.HitMark(other); mark != MarkNone {
		t.Errorf("a song not downloaded from here reads %v", mark)
	}
	s.Search.songReading = 0
	if mark, _ := s.HitMark(h); mark != MarkNone {
		t.Errorf("once the read of the songs landed with the song still waiting, the card reads %v", mark)
	}
	h.Resource.SongState = domain.AudioDone
	if mark, _ := s.HitMark(h); mark != MarkDone {
		t.Errorf("once the songs read again say done the card reads %v", mark)
	}
}
