package store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"amdl/internal/client"
)

func accountsIn(regions ...string) []client.Account {
	var out []client.Account
	for i, r := range regions {
		out = append(out, client.Account{ID: "a" + r, Region: r, Position: i, Default: i == 0, State: client.AccountActive})
	}
	return out
}

// requestsTo gives the store a client over a backend that records the
// query of each request it gets, and answers with an empty object.
func requestsTo(t *testing.T, s *Store) <-chan string {
	t.Helper()
	got := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.URL.Path + "?" + r.URL.RawQuery
		_, _ = w.Write([]byte(`{}`))
	}))
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); srv.Close() })
	s.client, s.ctx = c, ctx
	return got
}

func nextRequest(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no request reached the backend")
		return ""
	}
}

func TestSearchRegionsAreTheLoggedInAccountsRegionsDefaultFirst(t *testing.T) {
	s, _ := newTestStore(t)
	if r := s.SearchRegions(); len(r) != 0 || s.SearchRegion() != "" {
		t.Errorf("no accounts: %v %q", r, s.SearchRegion())
	}
	s.Server.Accounts = []client.Account{
		{ID: "b", Region: "JP", State: client.AccountActive},
		{ID: "c", Region: "gb", State: client.AccountMissing},
		{ID: "d", Region: "jp", State: client.AccountActive},
		{ID: "e", Region: "", State: client.AccountActive},
		{ID: "a", Region: "us", Default: true, State: client.AccountActive},
	}
	if got := s.SearchRegions(); !slices.Equal(got, []string{"us", "jp"}) {
		t.Errorf("regions = %v, want the default's us then jp once", got)
	}
	if s.SearchRegion() != "us" {
		t.Errorf("shown %q before a choice, want the default account's", s.SearchRegion())
	}
	s.Search.Region = "gb"
	if s.SearchRegion() != "us" || s.searchStorefront() != "" {
		t.Errorf("a region no account serves chosen: %q, sends %q", s.SearchRegion(), s.searchStorefront())
	}
}

func TestSearchSendsTheRegionChosenAndRerunsTheWords(t *testing.T) {
	s, _ := newTestStore(t)
	s.Server.Accounts = accountsIn("us", "jp")
	got := requestsTo(t, s)
	s.WantSearch("yorushika")
	s.Search.gen++ // the typing pause is not what this tests
	s.startSearch("yorushika")
	if r := nextRequest(t, got); r != "/api/v1/search?limit=6&term=yorushika" {
		t.Errorf("before a choice the backend's default answers: %s", r)
	}
	s.SetSearchRegion("jp")
	if r := nextRequest(t, got); r != "/api/v1/search?limit=6&storefront=jp&term=yorushika" {
		t.Errorf("after choosing jp: %s", r)
	}
	if s.Search.Status != SearchLoading {
		t.Errorf("status %v, want the new answer loading", s.Search.Status)
	}
	// The choice outlives clearing the field.
	s.WantSearch("")
	if s.SearchRegion() != "jp" {
		t.Errorf("clearing the field dropped the region: %q", s.SearchRegion())
	}
}

func TestSearchKeepsEachRegionsAnswer(t *testing.T) {
	s, _ := newTestStore(t)
	s.Server.Accounts = accountsIn("us", "jp")
	offlineClient(t, s)
	s.WantSearch("t")
	q := &s.Search
	s.applySearch(q.gen, "t", client.SearchResult{Songs: group(0, hit("song", "us1"))}, nil)
	s.SetSearchRegion("jp")
	s.applySearch(q.gen, "t", client.SearchResult{Songs: group(0, hit("song", "jp1"))}, nil)
	if ids(q.ShownHits()) != "jp1" {
		t.Fatalf("jp shows %s", ids(q.ShownHits()))
	}
	s.SetSearchRegion("us")
	if q.Status != SearchReady || ids(q.ShownHits()) != "us1" {
		t.Errorf("back to us: status %v hits %s, want its own answer from the cache", q.Status, ids(q.ShownHits()))
	}
}

func TestArtistAndAlbumPagesReadInTheRegionOfTheSearch(t *testing.T) {
	s, _ := newTestStore(t)
	s.Server.Accounts = accountsIn("us", "jp")
	got := requestsTo(t, s)
	s.SetSearchRegion("jp")
	s.OpenArtist("7")
	if r := nextRequest(t, got); r != "/api/v1/artists/7/albums?limit=25&storefront=jp" {
		t.Errorf("artist: %s", r)
	}
	s.SetSearchRegion("us")
	if s.Search.Artist.ID != "" {
		t.Errorf("the artist of jp stays open under us: %q", s.Search.Artist.ID)
	}
	h := hit("album", "42")
	h.URL = "https://music.apple.com/jp/album/x/42"
	s.OpenAlbum(h)
	if r := nextRequest(t, got); r != "/api/v1/albums/42?storefront=jp" {
		t.Errorf("album: %s", r)
	}
}

func TestLinkStorefront(t *testing.T) {
	for link, want := range map[string]string{
		"https://music.apple.com/jp/album/x/42": "jp",
		"https://music.apple.com/US/song/1":     "us",
		"https://music.apple.com/album/x/42":    "",
		"https://example.com/jp/album/42":       "",
		"":                                      "",
	} {
		if got := LinkStorefront(link); got != want {
			t.Errorf("LinkStorefront(%q) = %q, want %q", link, got, want)
		}
	}
}
