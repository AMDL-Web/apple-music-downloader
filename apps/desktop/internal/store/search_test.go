package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// These follow the web console's apps/console/jstest/search.test.mjs.

func hit(typ, id string) client.SearchHit {
	return client.SearchHit{Type: typ, ID: id, Name: typ + " " + id, URL: "https://music.apple.com/cn/" + typ + "/x/" + id}
}

func group(next int, hits ...client.SearchHit) *client.HitGroup {
	return &client.HitGroup{Items: hits, NextOffset: next}
}

func ids(hits []client.SearchHit) string {
	var out []string
	for _, h := range hits {
		out = append(out, h.ID)
	}
	return strings.Join(out, ",")
}

// offlineClient gives the store a client whose context has ended, so a
// search it starts posts nothing back: the test drives the answers itself.
func offlineClient(t *testing.T, s *Store) {
	t.Helper()
	c, err := client.New("http://127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.client, s.ctx = c, ctx
}

func TestSearchTermOnlyWithoutALink(t *testing.T) {
	if term, _ := SearchTerm("  yoru \n shika  ", false); term != "yoru shika" {
		t.Errorf("term = %q", term)
	}
	if term, _ := SearchTerm("yorushika https://music.apple.com/cn/album/1", true); term != "" {
		t.Errorf("a link previews instead, term = %q", term)
	}
	if term, long := SearchTerm(strings.Repeat("あ", SearchMaxTerm), false); term == "" || long {
		t.Error("200 characters should search")
	}
	if term, long := SearchTerm(strings.Repeat("あ", SearchMaxTerm+1), false); term != "" || !long {
		t.Error("201 characters should be too long")
	}
}

func TestSearchPageAppendsEachHitOnce(t *testing.T) {
	g := appendPage(group(5, hit("song", "1"), hit("song", "2")), group(0, hit("song", "2"), hit("song", "3")))
	if ids(g.Items) != "1,2,3" || g.NextOffset != 0 {
		t.Errorf("page = %s next %d", ids(g.Items), g.NextOffset)
	}
}

func TestSearchNewTermKeepsTheLastAnswerUntilItsOwnLands(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	s.WantSearch("yorushika")
	first := s.Search.gen
	s.applySearch(first, "yorushika", client.SearchResult{Term: "yorushika", Songs: group(0, hit("song", "1"))}, nil)
	s.WantSearch("yoasobi")
	q := &s.Search
	if q.Status != SearchLoading || !q.Stale || q.Result == nil || ids(q.ShownHits()) != "1" {
		t.Fatalf("while loading: status %v stale %v hits %s", q.Status, q.Stale, ids(q.ShownHits()))
	}
	// The old term's late answer never replaces the new one.
	s.applySearch(first, "yorushika", client.SearchResult{Songs: group(0, hit("song", "9"))}, nil)
	if q.Status != SearchLoading {
		t.Fatalf("a late answer landed: status %v", q.Status)
	}
	s.applySearch(q.gen, "yoasobi", client.SearchResult{Songs: group(0, hit("song", "2"))}, nil)
	if q.Status != SearchReady || q.Stale || ids(q.ShownHits()) != "2" {
		t.Fatalf("after its answer: status %v stale %v hits %s", q.Status, q.Stale, ids(q.ShownHits()))
	}
	// Back to the first term: its answer comes back at once.
	s.WantSearch("yorushika")
	if q.Status != SearchReady || ids(q.ShownHits()) != "1" {
		t.Errorf("cached term: status %v hits %s", q.Status, ids(q.ShownHits()))
	}
	s.WantSearch("")
	if q.Status != SearchIdle || q.Result != nil {
		t.Errorf("cleared: status %v result %v", q.Status, q.Result)
	}
}

func TestSearchErrorIsNotCached(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	s.WantSearch("x")
	q := &s.Search
	s.applySearch(q.gen, "x", client.SearchResult{}, errors.New("upstream_timeout"))
	if q.Status != SearchFailed || q.Err != "upstream_timeout" {
		t.Fatalf("status %v err %q", q.Status, q.Err)
	}
	if _, ok := q.cache.get("x"); ok {
		t.Error("an error was cached")
	}
	s.RetrySearch()
	if q.Status != SearchLoading {
		t.Errorf("retry: status %v", q.Status)
	}
}

func TestSearchMoreAppendsToOneGroup(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	s.WantSearch("t")
	q := &s.Search
	s.applySearch(q.gen, "t", client.SearchResult{Songs: group(5, hit("song", "1")), Albums: group(0, hit("album", "a"))}, nil)
	s.MoreSearch("songs")
	if q.More["songs"] != PageLoading {
		t.Fatalf("more state %v", q.More["songs"])
	}
	s.applyMore(q.gen, "t", "songs", nil, errors.New("boom"))
	if q.More["songs"] != PageFailed || ids(q.ShownHits()) != "a,1" {
		t.Fatalf("a failed page: state %v hits %s", q.More["songs"], ids(q.ShownHits()))
	}
	s.applyMore(q.gen, "t", "songs", group(0, hit("song", "1"), hit("song", "2")), nil)
	if ids(q.ShownHits()) != "a,1,2" || q.Result.Songs.NextOffset != 0 {
		t.Errorf("hits %s next %d", ids(q.ShownHits()), q.Result.Songs.NextOffset)
	}
	if r, _ := q.cache.get("t"); ids(r.Songs.Items) != "1,2" {
		t.Error("the cache missed the new page")
	}
}

func TestArtistPagesOpenPageOnAndCache(t *testing.T) {
	s, _ := newTestStore(t)
	offlineClient(t, s)
	a := &s.Search.Artist
	s.OpenArtist("7")
	if a.Status != SearchLoading {
		t.Fatalf("status %v", a.Status)
	}
	s.applyArtist(a.gen, "7", client.ArtistAlbums{Artist: hit("artist", "7"), Albums: *group(25, hit("album", "a"))}, nil, false)
	s.MoreArtist()
	s.applyArtist(a.gen, "7", client.ArtistAlbums{Albums: *group(0, hit("album", "b"))}, nil, true)
	if a.Status != SearchReady || ids(s.Search.ShownHits()) != "a,b" || a.Artist.ID != "7" {
		t.Fatalf("status %v albums %s artist %q", a.Status, ids(s.Search.ShownHits()), a.Artist.ID)
	}
	s.OpenArtist("")
	s.OpenArtist("7")
	if a.Status != SearchReady || ids(a.Albums.Items) != "a,b" {
		t.Errorf("cached artist: status %v albums %s", a.Status, ids(a.Albums.Items))
	}
	s.OpenArtist("8")
	s.applyArtist(a.gen, "8", client.ArtistAlbums{}, errors.New("upstream_error"), false)
	if a.Status != SearchFailed {
		t.Errorf("failed artist: status %v", a.Status)
	}
}

func TestHitMarkFollowsTheLiveJob(t *testing.T) {
	s, _ := newTestStore(t)
	h := hit("album", "1")
	h.PriorJob = &client.PriorJob{ID: "j", Status: domain.JobCompleted}
	if m, id := s.HitMark(h); m != MarkDone || id != "j" {
		t.Fatalf("prior job: mark %v %q", m, id)
	}
	s.applySnapshot(client.DownloadList{Downloads: []domain.Job{{ID: "j", Status: domain.JobRunning}}})
	if m, _ := s.HitMark(h); m != MarkActive {
		t.Errorf("live job running: mark %v", m)
	}
	s.upsert(domain.Job{ID: "j", Status: domain.JobFailed})
	if m, _ := s.HitMark(h); m != MarkNone {
		t.Errorf("live job failed: mark %v", m)
	}
	// A job started from the search wins over the prior one.
	s.Search.sent[HitKey(h)] = "k"
	s.upsert(domain.Job{ID: "k", Status: domain.JobQueued})
	if m, id := s.HitMark(h); m != MarkActive || id != "k" {
		t.Errorf("sent job: mark %v %q", m, id)
	}
	s.remove("k")
	if m, _ := s.HitMark(h); m != MarkNone {
		t.Errorf("deleted job: mark %v", m)
	}
}
