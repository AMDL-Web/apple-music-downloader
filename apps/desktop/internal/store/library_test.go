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
	s.SetLibraryTab(LibraryApple)
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
