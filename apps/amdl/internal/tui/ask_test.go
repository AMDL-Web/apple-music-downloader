package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// A download from search asks only where there is a choice, as the desktop
// app's sheet does, and sends the answers as the job's overrides.
func TestADownloadAsksOnlyWhenThereIsAChoice(t *testing.T) {
	m := sample(t)
	both := client.SearchHit{Type: "album", Name: "Glass Weather", URL: "https://music.apple.com/us/album/glass-weather/920001", AudioTraits: []string{"atmos", "lossless"}}
	plain := client.SearchHit{Type: "album", Name: "Tidal Rooms", URL: "https://music.apple.com/us/album/tidal-rooms/920002", AudioTraits: []string{"lossless"}}
	owned := client.SearchHit{Type: "album", Name: "Late Bloom", URL: "https://music.apple.com/us/album/late-bloom/920006", AudioTraits: []string{"lossless"},
		Resource: &domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "920006", State: domain.RecordComplete, TracksDone: 9, TracksWanted: 9}}
	priority := []string{"alac", "ec3", "aac"}

	if cmd := m.choose([]client.SearchHit{plain}, []string{plain.Name}, priority); cmd == nil || m.panel == panelAsk {
		t.Fatalf("one quality and not in the library: panel %v, want the download sent", m.panel)
	}

	withResults(m)
	m.choose([]client.SearchHit{both}, []string{both.Name}, priority)
	if m.panel != panelAsk || !slices.Equal(m.ask.codecs, []string{"alac", "ec3"}) || m.ask.again {
		t.Fatalf("lossless and Dolby: ask %+v", m.ask)
	}
	if out := stripANSI(m.askView(100)); !strings.Contains(out, "Lossless") || !strings.Contains(out, "Dolby Atmos") || strings.Contains(out, "Replace") {
		t.Errorf("the panel offers the two qualities only:\n%s", out)
	}
	m.askKey("right")
	if o := m.ask.overrides(both); o == nil || !slices.Equal(*o.QualityPriority, []string{"ec3", "alac", "aac"}) {
		t.Errorf("Dolby chosen: %+v", o)
	}
	m.askKey("esc")
	if m.panel != panelResults || m.ask != nil {
		t.Errorf("esc goes back to the hits: panel %v", m.panel)
	}

	m.choose([]client.SearchHit{owned, plain}, []string{owned.Name, plain.Name}, priority)
	if m.panel != panelAsk || m.ask.codecs != nil || !m.ask.again {
		t.Fatalf("in the library: ask %+v", m.ask)
	}
	// Downloading it again is a reset of its record, not an override.
	if o := m.ask.overrides(owned); o != nil {
		t.Errorf("a hit in the library carries no overrides: %+v", o)
	}
	m.askKey("right")
	if o := m.ask.overrides(plain); o != nil {
		t.Errorf("a hit not in the library has nothing to override: %+v", o)
	}
	for _, w := range []int{120, 46, 40} {
		out := stripANSI(m.askView(w))
		for _, l := range strings.Split(out, "\n") {
			if lipgloss.Width(l) > w {
				t.Errorf("%d columns: a %d-column line", w, lipgloss.Width(l))
			}
		}
		if !strings.Contains(out, "Keep") || !strings.Contains(out, "gain") {
			t.Errorf("%d columns: both choices show:\n%s", w, out)
		}
	}

	// Hits with different overrides go in separate submits.
	a := &ask{hits: []client.SearchHit{both, plain, owned}, priority: priority, codecs: []string{"alac", "ec3"}, codec: 1, again: true, replace: 1}
	// Downloading the hit in the library again is a reset of its record,
	// not an add.
	if bs := a.batches(); len(bs) != 2 {
		t.Errorf("download again: %d submits, want the two hits not in the library", len(bs))
	}
	a.replace = 0
	bs := a.batches()
	if len(bs) != 3 {
		t.Fatalf("%d submits, want one per set of overrides", len(bs))
	}
	if b := bs[0]; !slices.Equal(b.urls, []string{both.URL}) || !slices.Equal(*b.o.QualityPriority, []string{"ec3", "alac", "aac"}) {
		t.Errorf("Dolby for the hit that has it: %+v", b)
	}
	if b := bs[1]; !slices.Equal(b.urls, []string{plain.URL}) || b.o != nil {
		t.Errorf("the server's settings for the rest: %+v", b)
	}
	if b := bs[2]; !slices.Equal(b.urls, []string{owned.URL}) || b.o != nil {
		t.Errorf("the hit in the library goes on its own: %+v", b)
	}
}

// A filter's tab reads its group a page at a time: the first page when the
// tab opens, the next as the cursor nears its end; the all tab keeps the
// first hits of each group.
func TestTabsReadTheirGroupAPageAtATime(t *testing.T) {
	m := sample(t)
	withResults(m)
	r := m.res
	page := func(from, n, next int) *client.HitGroup {
		g := &client.HitGroup{NextOffset: next}
		for i := from; i < from+n; i++ {
			g.Items = append(g.Items, client.SearchHit{Type: "album", Name: fmt.Sprintf("Album %d", i), URL: fmt.Sprintf("https://music.apple.com/us/album/a/%d", i)})
		}
		return g
	}
	if cmd, _ := m.resultsKey("tab"); cmd == nil || filters[r.filter] != "album" || !r.loading {
		t.Fatalf("opening the album tab reads its first page: filter %s, loading %v", filters[r.filter], r.loading)
	}
	if n := len(r.visible()); n != 2 {
		t.Errorf("until the page comes the tab shows the albums it has: %d", n)
	}
	m.update(pageMsg{r: r, typ: "album", offset: 0, group: page(0, pageSize, pageSize)})
	if n := len(r.visible()); n != pageSize || r.loading {
		t.Fatalf("after the first page: %d hits, loading %v", n, r.loading)
	}
	r.cursor = pageSize - 5
	if cmd, _ := m.resultsKey("down"); cmd != nil {
		t.Error("a page read too early")
	}
	if cmd, _ := m.resultsKey("down"); cmd == nil || !r.loading {
		t.Fatal("the next page is read as the cursor nears the end")
	}
	m.update(pageMsg{r: r, typ: "album", offset: pageSize, group: page(pageSize, 10, 0)})
	if n := len(r.visible()); n != pageSize+10 || r.next["album"] != 0 {
		t.Fatalf("after the last page: %d hits, next %d", n, r.next["album"])
	}
	r.cursor = len(r.visible()) - 2
	if cmd, _ := m.resultsKey("down"); cmd != nil {
		t.Error("no page is read past the last")
	}
	r.filter = 0
	if n := len(r.visible()); n != 5 {
		t.Errorf("the all tab shows %d hits, want the first five", n)
	}
}

// Esc while the server's quality priority is read drops the download: the
// panel does not open after the person has moved on.
func TestEscDropsADownloadWaitingForItsChoices(t *testing.T) {
	m := sample(t)
	withResults(m)
	both := client.SearchHit{Type: "album", Name: "Glass Weather", URL: "https://music.apple.com/us/album/glass-weather/920001", AudioTraits: []string{"atmos", "lossless"}}
	if cmd := m.download([]client.SearchHit{both}, []string{both.Name}); cmd == nil {
		t.Fatal("a hit with Dolby reads the server's priority first")
	}
	gen := m.reqGen
	m.onKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	m.update(choicesMsg{gen: gen, hits: []client.SearchHit{both}, names: []string{both.Name}, priority: []string{"alac", "ec3"}})
	if m.panel == panelAsk {
		t.Error("the choices opened after esc")
	}
}

// /quality orders the qualities the server tries and turns them on or off,
// keeping one on, as the desktop app's codec priority does.
func TestQualityPriorityIsEditedFromTheKeyboard(t *testing.T) {
	m := sample(t)
	m.update(priorityMsg{gen: m.read("reading"), priority: []string{"alac", "nonsense"}})
	if m.panel != panelPriority {
		t.Fatalf("panel %v", m.panel)
	}
	p := m.priority
	if want := []string{"alac", "ec3", "ac3", "aac", "aac-binaural", "aac-downmix"}; !slices.Equal(p.rows(), want) {
		t.Fatalf("rows %v, want the one on first, then the rest", p.rows())
	}
	m.priorityKey("space")
	if !p.last || !slices.Equal(p.on, []string{"alac"}) {
		t.Errorf("the last quality on was turned off: %v", p.on)
	}
	m.priorityKey("down")
	m.priorityKey("space") // Dolby Atmos on, after ALAC
	m.priorityKey("shift+up")
	if !slices.Equal(p.on, []string{"ec3", "alac"}) || p.cursor != 0 {
		t.Errorf("moved up: %v, cursor %d", p.on, p.cursor)
	}
	m.priorityKey("down")
	m.priorityKey("down")
	m.priorityKey("shift+up") // AC-3 is off: it does not move
	if !slices.Equal(p.on, []string{"ec3", "alac"}) {
		t.Errorf("a quality that is off moved: %v", p.on)
	}
	m.priorityKey("up")
	m.priorityKey("shift+home")
	if !slices.Equal(p.on, []string{"alac", "ec3"}) || p.cursor != 0 {
		t.Errorf("shift+home: %v, cursor %d", p.on, p.cursor)
	}
	m.priorityKey("shift+end")
	if !slices.Equal(p.on, []string{"ec3", "alac"}) || p.cursor != 1 {
		t.Errorf("shift+end: %v, cursor %d", p.on, p.cursor)
	}
	for _, w := range []int{100, 40} {
		out := stripANSI(m.priorityView(w, 10))
		if !strings.Contains(out, "1  Dolby Atmos (E-AC-3)") || !strings.Contains(out, "2  ALAC (lossless)") {
			t.Errorf("%d columns: the order shows:\n%s", w, out)
		}
	}
	if cmd, _ := m.priorityKey("enter"); cmd == nil || m.panel != panelNone {
		t.Error("enter saves and closes")
	}
}

// A result row says a run is under way first, then a pause, then how much
// of the record is downloaded; no record says nothing.
func TestHitMarkFollowsTheRecord(t *testing.T) {
	partial := domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "1", State: domain.RecordPartial, TracksDone: 9, TracksWanted: 12}
	running, paused := partial, partial
	running.ActiveJobID, paused.Paused = "j", true
	complete := domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "1", State: domain.RecordComplete, TracksDone: 12, TracksWanted: 12}
	song := domain.ResourceMark{Kind: domain.ResourceAlbum, ID: "1", State: domain.RecordPartial, SongState: domain.AudioDone}
	for _, tt := range []struct {
		hit  client.SearchHit
		want string
	}{
		{client.SearchHit{Type: "album", Resource: &running}, "↻ downloading"},
		{client.SearchHit{Type: "album", Resource: &paused}, "‖ paused"},
		{client.SearchHit{Type: "album", Resource: &partial}, "9/12"},
		{client.SearchHit{Type: "album", Resource: &complete}, "✓ in library"},
		{client.SearchHit{Type: "song", Resource: &song}, "✓ in library"},
		{client.SearchHit{Type: "album"}, ""},
	} {
		if got := hitMark(tt.hit); got != tt.want {
			t.Errorf("hitMark(%+v) = %q, want %q", tt.hit, got, tt.want)
		}
	}
}
