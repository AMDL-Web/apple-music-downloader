package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
	"amdl/internal/progress"
)

func at(s int) time.Time { return time.Date(2026, 10, 30, 10, 0, s, 0, time.UTC) }

// sample is a running album with tracks in every state, plus a queued
// playlist, as the live region would hold them.
func sample(t *testing.T) *model {
	t.Helper()
	c, err := client.New("http://127.0.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newModel(ctx, cancel, Options{Client: c, Target: "nas (http://192.168.3.5:18570)", Version: "2.0.0"})
	started := at(0)
	job := domain.Job{ID: "job_a", Input: "x", Type: "album", Status: domain.JobRunning, Title: "IGOR", ArtistName: "Tyler, The Creator",
		ArtworkBgColor: "f1a8bf", TotalItems: 12, CompletedItems: 2, SkippedItems: 1, StartedAt: &started, CreatedAt: at(0)}
	full := domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 1, Remuxed: true, Verified: true, Tagged: true, Saved: true}
	items := []domain.JobItem{
		{ID: "i1", Index: 1, Title: "IGOR'S THEME", Status: domain.ItemCompleted, Progress: full, Codec: "alac", BitDepth: 24, SampleRate: 96000, UpdatedAt: at(10)},
		{ID: "i2", Index: 2, Title: "EARFQUAKE", Status: domain.ItemCompleted, Progress: full, UpdatedAt: at(30)},
		{ID: "i3", Index: 3, Title: "I THINK", Status: domain.ItemDownloading, Progress: domain.ItemProgress{Resolved: true, Download: 0.62}},
		{ID: "i4", Index: 4, Title: "EXACTLY WHAT YOU RUN FROM YOU END UP CHASING", Status: domain.ItemSkipped, Progress: domain.ItemProgress{Resolved: true}, UpdatedAt: at(20)},
		{ID: "i5", Index: 5, Title: "RUNNING OUT OF TIME", Status: domain.ItemDecrypting, Progress: domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 0.35}},
		{ID: "i6", Index: 6, Title: "NEW MAGIC WAND", Status: domain.ItemFailed, Error: "decrypt failed", Progress: domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 0.8}},
	}
	for i := 7; i <= 12; i++ {
		items = append(items, domain.JobItem{ID: fmt.Sprintf("i%d", i), Index: i, Title: fmt.Sprintf("TRACK %d", i), Status: domain.ItemQueued})
	}
	d := client.DownloadDetail{Job: job, Items: items}
	st := &jobState{job: job, detail: &d, index: map[string]int{}}
	st.rate.Add(time.Now().Add(-20*time.Second), 0.20)
	st.rate.Add(time.Now(), 0.35)
	m.jobs["job_a"] = st
	m.jobs["job_b"] = &jobState{job: domain.Job{ID: "job_b", Type: "playlist", Status: domain.JobQueued, Title: "City Pop Essentials", CreatedAt: at(5)}}
	m.wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true, Regions: []string{"cn"}}
	m.height = 40
	return m
}

func render(m *model, w int) string {
	m.width = w
	return m.View().Content
}

func TestEveryLineFitsTheTerminal(t *testing.T) {
	m := sample(t)
	m.res = &results{term: "igor", items: []pick{
		{hit: client.SearchHit{Type: "album", Name: "IGOR", ArtistName: "Tyler, The Creator", ReleaseDate: "2019-05-17", TrackCount: 12, PriorJob: &client.PriorJob{Status: domain.JobCompleted}}},
		{hit: client.SearchHit{Type: "album", Name: "CALL ME IF YOU GET LOST", ArtistName: "Tyler, The Creator", ReleaseDate: "2021-06-25", TrackCount: 16}, selected: true},
		{hit: client.SearchHit{Type: "song", Name: "EARFQUAKE", ArtistName: "Tyler, The Creator", DurationMS: 190000}},
		{hit: client.SearchHit{Type: "artist", Name: "Tyler, The Creator"}},
	}}
	m.panel = panelResults
	dir := os.Getenv("AMDL_TUI_SNAPSHOTS")
	for _, w := range []int{120, 100, 72, 46, 30} {
		out := render(m, w)
		for i, line := range strings.Split(out, "\n") {
			if lw := lipgloss.Width(line); lw > w {
				t.Errorf("width %d: line %d is %d columns: %q", w, i, lw, line)
			}
		}
		if dir != "" {
			full := m.header() + "\n\n" + m.you("tyler igor") + "\n" + out
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("tui-%d.ansi", w)), []byte(full), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSmallTerminalsDropDetail(t *testing.T) {
	m := sample(t)
	if out := render(m, 100); !strings.Contains(out, "Download 62%") || !strings.Contains(out, "Tyler, The Creator") {
		t.Errorf("a wide terminal shows track states and the artist:\n%s", out)
	}
	if out := render(m, 46); strings.Contains(out, "I THINK") || strings.Contains(out, "Tyler") {
		t.Errorf("a narrow terminal drops track rows and the artist:\n%s", out)
	}
	m.height = 10
	if out := render(m, 100); strings.Contains(out, "IGOR") {
		t.Errorf("a short terminal keeps only the prompt and status:\n%s", out)
	}
}

func TestBackendAddressShowsOnlyInTheHeader(t *testing.T) {
	m := sample(t)
	for _, w := range []int{46, 72, 100, 140} {
		if s := m.status(w); strings.Contains(s, "192.168.3.5") || strings.Contains(s, "nas") {
			t.Errorf("the footer at %d columns repeats the backend: %q", w, s)
		}
	}
	if s := m.status(100); !strings.Contains(s, "1 running · 1 queued") {
		t.Errorf("the footer keeps the job counts: %q", s)
	}
	if h := m.header(); !strings.Contains(h, "nas (http://192.168.3.5:18570)") {
		t.Errorf("the header names the backend: %q", h)
	}
}

func TestPromptSaysSearchNeedsEnter(t *testing.T) {
	m := sample(t)
	m.width = 100
	m.prompt(100)
	if ph := m.input.Placeholder; !strings.Contains(ph, "press enter to search") || strings.Contains(ph, "type to search") {
		t.Errorf("placeholder %q promises search as you type", ph)
	}
	if h := m.header(); !strings.Contains(h, "press enter to search") || strings.Contains(h, "type to search") {
		t.Errorf("tip %q promises search as you type", h)
	}
}

func TestAlbumBarCells(t *testing.T) {
	th := newTheme(true)
	a := progress.Album{Total: 4, Lit: 1, Runs: []progress.Run{{Status: domain.ItemCompleted, Share: 0.25}}, Done: 0.25, Live: 0.5, Ticks: 4}
	bar := albumBar(th, 23, a, th.clay)
	if lipgloss.Width(bar) != 23 {
		t.Fatalf("width = %d", lipgloss.Width(bar))
	}
	plain := stripANSI(bar)
	if strings.Count(plain, " ") != 3 {
		t.Errorf("4 cells need 3 gaps: %q", plain)
	}
	a.Ticks = 0
	if plain := stripANSI(albumBar(th, 23, a, th.clay)); strings.Contains(plain, " ") {
		t.Errorf("a continuous bar has no gaps: %q", plain)
	}
	a.Ticks = 24
	if plain := stripANSI(albumBar(th, 30, a, th.clay)); strings.Contains(plain, " ") {
		t.Errorf("cells narrower than 2 columns merge into one bar: %q", plain)
	}
}

func TestStageBarWidth(t *testing.T) {
	th := newTheme(false)
	for _, w := range []int{12, 20, 35, 36, 60} {
		it := domain.JobItem{Status: domain.ItemDecrypting, Progress: domain.ItemProgress{Resolved: true, Download: 1, Decrypt: 0.4}}
		if got := lipgloss.Width(stageBar(th, w, it, th.clay)); got != w {
			t.Errorf("stage bar for %d columns is %d wide", w, got)
		}
	}
}

func TestStageWidthsKeepEveryStage(t *testing.T) {
	for w := 7; w <= 80; w++ {
		sum := 0
		for i, cw := range stageWidths(w) {
			if cw < 1 {
				t.Errorf("width %d: stage %d is %d cells", w, i, cw)
			}
			sum += cw
		}
		if sum != w {
			t.Errorf("width %d: stages add up to %d", w, sum)
		}
	}
}

func TestAccentKeepsContrast(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := newTheme(dark)
		for _, bg := range []string{"000000", "ffffff", "f1a8bf", "1a1a1a", "", "zzz"} {
			if c := contrast(th.accent(bg), th.bg); c < 3 {
				t.Errorf("dark=%v accent for %q has contrast %.2f", dark, bg, c)
			}
		}
	}
}

func TestLinksAreTakenFromALine(t *testing.T) {
	got := linksIn("https://music.apple.com/us/album/a/1, https://music.apple.com/cn/album/b/2 igor")
	if len(got) != 2 || got[1] != "https://music.apple.com/cn/album/b/2" {
		t.Errorf("links = %v", got)
	}
	if linksIn("tyler igor") != nil {
		t.Error("plain text has no links")
	}
}

func TestFinishedJobGoesToScrollbackOnce(t *testing.T) {
	m := sample(t)
	fin := at(90)
	j := m.jobs["job_a"].job
	j.Status, j.FinishedAt, j.CompletedItems = domain.JobCompleted, &fin, 11
	if cmd := m.onFeed(domain.DownloadFeedMessage{Type: "download_upserted", Job: &j}); cmd == nil {
		t.Fatal("a finished job should print its line")
	}
	if _, ok := m.jobs["job_a"]; ok {
		t.Error("a finished job leaves the live region")
	}
	if cmd := m.onFeed(domain.DownloadFeedMessage{Type: "download_upserted", Job: &j}); cmd != nil {
		t.Error("a resent message prints nothing")
	}
	line := stripANSI(m.finishedLine(j, nil))
	if !strings.HasPrefix(line, "✓ IGOR — Tyler, The Creator · 12/12 saved · 1 already on disk · 1:30") {
		t.Errorf("finished line = %q", line)
	}
}

func TestRetriedJobFinishesAgain(t *testing.T) {
	m := sample(t)
	j := m.jobs["job_a"].job
	j.Status = domain.JobFailed
	m.onFeed(domain.DownloadFeedMessage{Type: "download_upserted", Job: &j})
	// POST /downloads/{id}/retry puts the same job back in the queue.
	j.Status = domain.JobQueued
	m.onFeed(domain.DownloadFeedMessage{Type: "download_upserted", Job: &j})
	if _, ok := m.jobs["job_a"]; !ok {
		t.Fatal("a retried job is live again")
	}
	j.Status = domain.JobCompleted
	if cmd := m.onFeed(domain.DownloadFeedMessage{Type: "download_upserted", Job: &j}); cmd == nil {
		t.Error("the retry's finish prints its own line")
	}
	if _, ok := m.jobs["job_a"]; ok {
		t.Error("the retried job's card stays after it finishes")
	}
}

func TestQuitNeedsCtrlCTwice(t *testing.T) {
	m := sample(t)
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m.onKey(ctrlC)
	if m.quitHint.IsZero() || m.ctx.Err() != nil {
		t.Fatal("the first ctrl+c only warns")
	}
	m.onKey(ctrlC)
	if m.ctx.Err() == nil {
		t.Error("the second ctrl+c quits")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestTrackCountIsSingularForOne(t *testing.T) {
	for n, want := range map[int]string{1: "1 track", 2: "2 tracks", 14: "14 tracks"} {
		if got := trackCount(n); got != want {
			t.Errorf("trackCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestAMistypedCodeKeepsTheLogin(t *testing.T) {
	m := sample(t)
	m.panel = panelLogin
	m.loginSt.step, m.loginSt.loginID = "waiting", "login_1"
	m.onLogin(loginMsg{err: &client.APIError{Status: 400, Code: "invalid_request", Message: "a two-step code is six digits"}})
	if m.loginSt.step != "code" || m.loginSt.loginID != "login_1" || m.panel != panelLogin {
		t.Fatalf("after a mistyped code: step %q, login %q, panel %v", m.loginSt.step, m.loginSt.loginID, m.panel)
	}
	m.onLogin(loginMsg{err: &client.APIError{Status: 401, Code: "authentication_failed", Message: "wrong code"}})
	if m.loginSt.loginID != "" || m.panel == panelLogin {
		t.Fatal("a refused code still waits for another")
	}
}

func TestHeaderNamesTheManagedWrappersState(t *testing.T) {
	m := sample(t)
	m.wrapper = &client.WrapperStatus{Managed: &client.ManagedWrapper{Mode: "stopped"}}
	if h := m.header(); !strings.Contains(h, "the local wrapper is stopped") || strings.Contains(h, "no Apple ID") {
		t.Fatalf("header = %q", h)
	}
}
