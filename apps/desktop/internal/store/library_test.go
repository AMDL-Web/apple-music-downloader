package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

func TestTheAppleTabShowsTheDefaultAccountsCopyUntilAnotherIsChosen(t *testing.T) {
	st := client.AppleLibraryStatus{Accounts: []client.AppleLibraryAccountStatus{
		{AccountID: "a1", Default: true, Enabled: false},
		{AccountID: "a2", Enabled: true},
		{AccountID: "a3", Enabled: true},
	}}
	for _, tc := range []struct{ was, want string }{
		{"", "a2"}, {"a3", "a3"}, {"a1", "a2"}, {"gone", "a2"},
	} {
		if got := appleAccountOf(st, tc.was); got != tc.want {
			t.Errorf("showing %q: %q; want %q", tc.was, got, tc.want)
		}
	}
	st.Accounts[0].Enabled = true
	if got := appleAccountOf(st, ""); got != "a1" {
		t.Errorf("the default's copy on: %q", got)
	}
	if got := appleAccountOf(client.AppleLibraryStatus{}, ""); got != "" {
		t.Errorf("no account: %q", got)
	}
}

// appleCopy serves an Apple library copy of n albums and m songs the way
// GET /apple-library/{kind} pages it, at most 200 items a page, and counts
// the list reads.
type appleCopy struct {
	albums, songs int
	mu            sync.Mutex
	reads         int
}

func (f *appleCopy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/apple-library/status":
		_ = json.NewEncoder(w).Encode(client.AppleLibraryStatus{Accounts: []client.AppleLibraryAccountStatus{
			{AccountID: "cn", Default: true, Enabled: true, Available: true}}})
	case "/api/v1/apple-library/albums", "/api/v1/apple-library/songs":
		f.mu.Lock()
		f.reads++
		f.mu.Unlock()
		kind, total := "albums", f.albums
		if r.URL.Path == "/api/v1/apple-library/songs" {
			kind, total = "songs", f.songs
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if limit < 1 || limit > 200 {
			http.Error(w, `{"error":{"code":"invalid_request"}}`, http.StatusBadRequest)
			return
		}
		items := []domain.AppleLibraryItem{}
		for i := offset; i < min(offset+limit, total); i++ {
			items = append(items, domain.AppleLibraryItem{Kind: kind, ID: fmt.Sprintf("l.%s%d", kind, i)})
		}
		_ = json.NewEncoder(w).Encode(client.AppleLibraryPage{Kind: kind, Items: items, Total: total, Offset: offset})
	default:
		http.NotFound(w, r)
	}
}

func (f *appleCopy) listReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// inOrder reports whether items are the list's first len(items), each once.
func inOrder(items []domain.AppleLibraryItem, kind string) error {
	for i, it := range items {
		if want := fmt.Sprintf("l.%s%d", kind, i); it.ID != want {
			return fmt.Errorf("item %d is %s; want %s", i, it.ID, want)
		}
	}
	return nil
}

func TestTheAppleTabReachesEveryItemOfALongLibrary(t *testing.T) {
	f := &appleCopy{albums: 343, songs: 747}
	s, settle := live(t, f)
	l := &s.Library
	s.OpenLibrary()
	settle(func() bool { return l.AppleStatus == SearchReady })
	if err := inOrder(l.Apple.Items, "albums"); err != nil || len(l.Apple.Items) != 343 || l.Apple.Total != 343 {
		t.Fatalf("all albums: %d of %d, %v", len(l.Apple.Items), l.Apple.Total, err)
	}

	// A fresh read, as after an add, still reads every item.
	l.Apple.Items = []domain.AppleLibraryItem{}
	l.refresh(s)
	settle(func() bool { return len(l.Apple.Items) > 0 })
	if err := inOrder(l.Apple.Items, "albums"); err != nil || len(l.Apple.Items) != 343 {
		t.Errorf("after a fresh read: %d items, %v", len(l.Apple.Items), err)
	}

	s.SetAppleKind("songs")
	settle(func() bool { return l.AppleStatus == SearchReady && l.Apple.Kind == "songs" })
	if err := inOrder(l.Apple.Items, "songs"); err != nil || len(l.Apple.Items) != 747 {
		t.Errorf("all songs: %d, %v", len(l.Apple.Items), err)
	}
}

// appleKeep serves an Apple library of one album, 1490555698, and no record
// of it, and answers an add of it as the backend answers one whose record
// needs no run: accepted, with no job. The read of the records can be held.
type appleKeep struct {
	hold chan struct{}
	mu   sync.Mutex
	kept bool
	// part serves the album's record as one wanting only the track added
	// by hand, done: the record of an album a 1.x install downloaded one
	// song of.
	part bool
}

const keepAlbum = "1490555698"

func (f *appleKeep) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch {
	case r.URL.Path == "/api/v1/apple-library/status":
		reply(client.AppleLibraryStatus{Accounts: []client.AppleLibraryAccountStatus{{AccountID: "jp", Default: true, Enabled: true, Available: true}}})
	case r.URL.Path == "/api/v1/apple-library/albums":
		reply(client.AppleLibraryPage{Kind: "albums", Total: 1, Items: []domain.AppleLibraryItem{
			{Kind: "albums", ID: "l.1", CatalogID: keepAlbum, URL: "https://music.apple.com/jp/album/x/" + keepAlbum, Name: "nameless story"}}})
	case r.URL.Path == "/api/v1/library/albums" || r.URL.Path == "/api/v1/library/playlists":
		if f.hold != nil && r.URL.Path == "/api/v1/library/albums" {
			select {
			case <-f.hold:
			case <-r.Context().Done():
			}
		}
		if f.part && r.URL.Path == "/api/v1/library/albums" {
			reply(map[string]any{"items": []any{map[string]any{"id": keepAlbum, "name": "nameless story", "state": "complete",
				"tracks_wanted": 1, "tracks_done": 1, "release_track_count": 6, "wanted_all": false}}, "total": 1})
			return
		}
		reply(map[string]any{"items": []any{}, "total": 0})
	case r.URL.Path == "/api/v1/downloads" && r.Method == http.MethodPost:
		w.WriteHeader(http.StatusAccepted)
		reply(domain.BatchSubmitResponse{Accepted: 1, Results: []domain.SubmitResult{{
			URL: "https://music.apple.com/jp/album/x/" + keepAlbum, Status: domain.SubmitAccepted,
			Resource: &domain.ResourceRef{Kind: domain.ResourceAlbum, ID: keepAlbum}}}})
	default:
		http.NotFound(w, r)
	}
}

// An add the backend takes without queueing a run, for a record it already
// keeps, marks the album kept at once: nothing else tells the Library, and
// its button would stay at "in the queue" for good.
func TestAnAddThatQueuesNoRunMarksTheAlbumKept(t *testing.T) {
	s, settle := live(t, &appleKeep{})
	l := &s.Library
	s.OpenLibrary()
	settle(func() bool { return l.AppleStatus == SearchReady })
	if l.Kept[keepAlbum] {
		t.Fatal("kept before any add")
	}
	url := l.Apple.Items[0].URL
	s.AddApple(url, "nameless story")
	settle(func() bool { return l.Added[url] != "" })
	if !l.Kept[keepAlbum] {
		t.Errorf("the add was %q and the album is not kept: %v", l.Added[url], l.Kept)
	}
}

// A read of the Apple library that was out when a record appeared never
// takes the record's keep away, or the album would show as not kept until
// the record changed again.
func TestAReadOutBeforeARecordAppearedKeepsItKept(t *testing.T) {
	f := &appleKeep{hold: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(f.hold) }) }
	s, settle := live(t, f)
	t.Cleanup(release)
	l := &s.Library
	s.OpenLibrary()
	settle(func() bool { return l.Apple.Items == nil && l.AppleStatus == SearchLoading })
	// The feed names the record while the read of the records is out.
	s.putMark(domain.ResourceMark{Kind: domain.ResourceAlbum, ID: keepAlbum, State: domain.RecordComplete, TracksDone: 6, TracksWanted: 6})
	release()
	settle(func() bool { return l.AppleStatus == SearchReady })
	if !l.Kept[keepAlbum] {
		t.Errorf("the read took the keep away: %v", l.Kept)
	}
}

// The Library keeps the mark of each record it read, so an Apple item's
// tile reads the record's state, the part of the album a record that wants
// only some tracks holds included.
func TestTheLibraryKeepsTheMarksOfTheRecordsItRead(t *testing.T) {
	s, settle := live(t, &appleKeep{part: true})
	l := &s.Library
	s.OpenLibrary()
	settle(func() bool { return l.AppleStatus == SearchReady && l.Kept[keepAlbum] })
	m := l.Mark(domain.ResourceAlbum, keepAlbum)
	if m == nil {
		t.Fatalf("no mark of the record read: %v", l.Marks)
	}
	hit := client.SearchHit{Type: "album", ID: keepAlbum, TrackCount: 6, Resource: m}
	if mark, _ := s.HitMark(hit); mark != MarkPartial {
		t.Errorf("1 of the 6 tracks, the record wanting only that one, reads %v, want partial", mark)
	}
	if got := l.Mark(domain.ResourceAlbum, "nothing"); got != nil {
		t.Errorf("an item with no record has the mark %+v", got)
	}
}
