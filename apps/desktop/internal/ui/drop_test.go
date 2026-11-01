package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"

	"amdl/internal/client"
)

func TestADroppedLinkDownloadsAsAHitDoes(t *testing.T) {
	a, run := demoApp(t)
	// The feed's first snapshot replaces the list (store.Store.ApplySnapshot),
	// so a job started before it lands could drop out of it for a moment.
	run(func() bool { return a.Store.Loaded })
	// An album the demo has no download of.
	link := "https://music.apple.com/us/album/salt-static/920004"
	a.dropLinks(time.Now(), transfer.TextData("Listen: "+link+" — so good"))
	if a.dropping != link {
		t.Fatalf("the drop looks up %q", a.dropping)
	}
	// With nothing to ask, it starts at once, as a search hit would: the
	// notice is the backend's acceptance (store.Store.SubmitHit).
	run(func() bool { return strings.HasPrefix(a.Store.Notice.Text, "Downloading") })
	if a.dropping != "" || a.asking != nil || a.Store.Notice.Error {
		t.Errorf("after the drop: looking up %q, sheet %v, notice %+v", a.dropping, a.asking, a.Store.Notice)
	}
	// Dropped again while it downloads, it opens that download, as its
	// search hit does.
	a.dropLinks(time.Now(), transfer.TextData(link))
	run(func() bool { return a.Screen == ScreenAlbum && a.Store.Detail() != nil })
}

func TestDroppedLinksGoToTheField(t *testing.T) {
	a, _ := demoApp(t)
	a.Go(ScreenQueue)
	one, two := "https://music.apple.com/us/album/a/1700000001", "https://music.apple.com/us/album/b/1700000002"
	links, err := transfer.URLData(one)
	if err != nil {
		t.Fatal(err)
	}
	a.dropLinks(time.Now(), transfer.New(append(links.Items(), transfer.TextData(two+"\n"+one).Items()...)...))
	if a.Screen != ScreenHome || len(a.Store.Omni.Links) != 2 || a.Store.Omni.Links[0].URL != one || a.Store.Omni.Links[1].URL != two {
		t.Errorf("screen %v, field links %v", a.Screen, a.Store.Omni.Links)
	}
}

func TestADropWithoutAnAppleMusicLinkIsIgnored(t *testing.T) {
	a, _ := demoApp(t)
	a.Go(ScreenQueue)
	files, err := transfer.FileData("/tmp/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []transfer.Data{transfer.TextData("just some words"), transfer.TextData("https://example.com/album/1"), files} {
		a.dropLinks(time.Now(), d)
	}
	if a.dropping != "" || a.Screen != ScreenQueue || a.Store.Notice.Text != "" || len(a.Store.Omni.Links) != 0 {
		t.Errorf("a drop without links did something: looking up %q, screen %v, notice %q", a.dropping, a.Screen, a.Store.Notice.Text)
	}
}

func TestALookupIsTheHitItNames(t *testing.T) {
	h := lookupHit("https://music.apple.com/us/album/x/1?i=2", client.Lookup{Type: "song", ID: "1", TrackID: "2", Title: "Song"}, nil)
	if h.Type != "song" || h.ID != "2" || h.URL != "https://music.apple.com/us/album/x/1?i=2" {
		t.Errorf("hit %+v", h)
	}
}
