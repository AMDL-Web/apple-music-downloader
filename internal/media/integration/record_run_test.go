package integration_test

import (
	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/jobs"
	"amdl/internal/usertoken"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Eyevinn/mp4ff/mp4"
)

// Every kind of add ends with its records downloaded: an album link, an
// album-track link (that one track of its album), a song link the resolver
// places, a catalog playlist with its own copies, an artist link whose albums
// become records, and a station, which stays a one-off run.
func TestRecordAddsSimulateEndToEnd(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Back in Black", "AC/DC", 3, 4, 1000, "")
		catalog.addAlbum("200", "Highway", "AC/DC", 2, 2, 1000, "")
		catalog.addAlbum("300", "Powerage", "AC/DC", 2, 2, 1000, "")
		catalog.addAlbum("400", "Let There Be Rock", "AC/DC", 1, 1, 1000, "")
		catalog.addAlbum("410", "Flick of the Switch", "AC/DC", 1, 1, 1000, "")
		catalog.artists["900"] = applemusic.ArtistAlbums{ID: "900", Name: "AC/DC", Albums: []applemusic.Collection{
			{ID: "400", Name: "Let There Be Rock"}, {ID: "410", Name: "Flick of the Switch"},
		}}
		catalog.playlists["pl.mix"] = applemusic.Collection{ID: "pl.mix", Type: applemusic.TypePlaylist, Name: "Mix", Artist: "Curator", Tracks: []applemusic.Song{
			catalog.songs["1002"], catalog.songs["1002"], catalog.songs["3001"],
		}}
		catalog.stations["ra.rock"] = applemusic.Collection{ID: "ra.rock", Type: applemusic.TypeStation, Name: "Rock Radio", Tracks: []applemusic.Song{
			catalog.songs["3002"],
		}}
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()

		h.submit(
			"https://music.apple.com/cn/album/back-in-black/100",
			"https://music.apple.com/cn/album/highway/200?i=2002",
			"https://music.apple.com/cn/song/song-1/3001",
			"https://music.apple.com/cn/playlist/mix/pl.mix",
			"https://music.apple.com/cn/artist/acdc/900",
			"https://music.apple.com/cn/station/rock-radio/ra.rock",
		)
		h.waitFor("every add to finish", func() bool {
			return h.albumComplete("100")() && h.albumComplete("200")() && h.albumComplete("300")() &&
				h.albumComplete("400")() && h.albumComplete("410")() && h.idle()()
		})

		back := h.album("100")
		if back.Name != "Back in Black" || back.ReleaseTrackCount != 4 || back.WithheldCount != 1 || back.TracksDone != 3 || !back.WantedAll {
			t.Fatalf("album 100 = %+v, want resolved, 3 of 4 tracks listed and done", back)
		}
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			n := strings.TrimPrefix(f.SongID, "100")
			want := filepath.Join("library", "AC_DC", "Back in Black", "0"+n+". Song "+n+".m4a")
			if f.AudioState != domain.AudioDone || f.Path != want || f.Codec != "aac-lc" || f.QualityLabel != "256Kbps" {
				t.Fatalf("file %+v, want done at %s as aac-lc", f, want)
			}
		}
		// An album-track link wants that one track of its album.
		if highway := h.album("200"); highway.WantedAll || highway.TracksWanted != 1 || highway.TracksDone != 1 {
			t.Fatalf("album 200 = %+v, want one wanted track, done", highway)
		}
		if files := h.files(domain.ResourceAlbum, "200"); len(files) != 1 || files[0].SongID != "2002" {
			t.Fatalf("album 200 files = %+v, want only 200-2", files)
		}
		// A song link becomes its album's record through the resolver.
		if files := h.files(domain.ResourceAlbum, "300"); len(files) != 1 || files[0].SongID != "3001" || files[0].AudioState != domain.AudioDone {
			t.Fatalf("album 300 files = %+v, want 300-1 done", files)
		}
		// An artist link adds each album as a whole-album record.
		for _, id := range []string{"400", "410"} {
			if album := h.album(id); !album.WantedAll || album.TracksDone != 1 {
				t.Fatalf("artist album %s = %+v, want added whole and done", id, album)
			}
		}
		// A playlist keeps its own copies, numbered by their place in Apple's
		// list: a song listed twice is one copy and does not renumber the next.
		playlist, err := h.store.GetPlaylist(context.Background(), "pl.mix")
		if err != nil {
			t.Fatal(err)
		}
		if playlist.State != domain.RecordComplete || playlist.TracksDone != 2 {
			t.Fatalf("playlist = %+v, want both copies done", playlist)
		}
		var playlistPaths []string
		for _, f := range h.files(domain.ResourcePlaylist, "pl.mix") {
			playlistPaths = append(playlistPaths, f.Path)
		}
		slices.Sort(playlistPaths)
		if want := []string{filepath.Join("playlists", "Mix", "01. Song 2.m4a"), filepath.Join("playlists", "Mix", "03. Song 1.m4a")}; !slices.Equal(playlistPaths, want) {
			t.Fatalf("playlist paths = %v, want %v", playlistPaths, want)
		}
		// A station is a one-off run with no record and no slots.
		stations, _, err := h.store.ListJobs(context.Background(), db.JobListFilter{Limit: 10, Types: []string{"station"}})
		if err != nil || len(stations) != 1 || stations[0].Status != domain.JobCompleted || stations[0].ResourceID != "" || stations[0].CompletedItems != 1 {
			t.Fatalf("station runs = %+v (err %v), want one completed one-off run", stations, err)
		}
		// One run per record, each completed with one item per slot.
		for _, id := range []string{"100", "200", "300", "400", "410"} {
			runs := h.runsOf(domain.ResourceAlbum, id)
			if len(runs) != 1 || runs[0].Status != domain.JobCompleted || runs[0].CompletedItems != runs[0].TotalItems || runs[0].TotalItems == 0 {
				t.Fatalf("runs of album %s = %+v, want one completed run", id, runs)
			}
		}

		// Adding a downloaded album again queues nothing: records, not files,
		// answer.
		resp := h.submit("https://music.apple.com/us/album/back-in-black/100")
		if resp.Results[0].Job != nil {
			t.Fatalf("re-add of a complete album queued run %s", resp.Results[0].Job.ID)
		}
	})
}

// A file already at a slot's path is adopted as the slot's file at the
// write-time stat: no download, and the file is left as it was.
func TestRecordRunAdoptsAFileAtTheTarget(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
		existing := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1.m4a")
		if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(existing, []byte("hand placed"), 0o644); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the run to end", h.idle())

		files := h.files(domain.ResourceAlbum, "100")
		if files[0].AudioState != domain.AudioDone || files[0].FileSize != int64(len("hand placed")) || files[0].Codec != "" {
			t.Fatalf("adopted slot = %+v, want done with the file's size and no codec claimed", files[0])
		}
		if got, err := os.ReadFile(existing); err != nil || string(got) != "hand placed" {
			t.Fatalf("the adopted file changed: %q, %v", got, err)
		}
		items, err := h.store.ListItems(context.Background(), h.runsOf(domain.ResourceAlbum, "100")[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		statuses := map[string]domain.ItemStatus{}
		for _, item := range items {
			statuses[item.AdamID] = item.Status
		}
		if statuses["1001"] != domain.ItemSkipped || statuses["1002"] != domain.ItemCompleted {
			t.Fatalf("item statuses = %v, want 100-1 skipped and 100-2 completed", statuses)
		}
	})
}

// A file adopted at a track's path keeps the codec its boxes name, so
// another quality of the track can still be added, and the same one cannot.
func TestRecordRunRecordsTheCodecOfAnAdoptedFile(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
		existing := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1.m4a")
		if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
			t.Fatal(err)
		}
		alac := alacInit(t)
		if err := os.WriteFile(existing, alac, 0o644); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the run to end", h.idle())

		adopted := h.files(domain.ResourceAlbum, "100")[0]
		if adopted.AudioState != domain.AudioDone || adopted.Codec != "alac" || adopted.FileSize != int64(len(alac)) {
			t.Fatalf("adopted slot = %+v, want done, alac, with the file's size", adopted)
		}
		if got, err := os.ReadFile(existing); err != nil || string(got) != string(alac) {
			t.Fatalf("the adopted file changed: %v", err)
		}
		key := adopted.FileKey
		key.Slot = "aac-lc"
		if err := h.store.RequestSlot(context.Background(), key, time.Now()); err != nil {
			t.Errorf("another quality of the adopted ALAC track: %v, want it added", err)
		}
		key.Slot = "alac"
		if err := h.store.RequestSlot(context.Background(), key, time.Now()); !errors.Is(err, db.ErrQualityPresent) {
			t.Errorf("ALAC again for the adopted ALAC track: %v, want ErrQualityPresent", err)
		}
	})
}

// Simulate mode adopts what it committed itself as a real run adopts a file:
// an album removed and added again takes its tracks as already downloaded,
// with the quality the first run recorded.
func TestASimulatedAlbumAddedAgainKeepsItsQuality(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the run to end", h.idle())
		first := h.files(domain.ResourceAlbum, "100")
		if len(first) != 2 || first[0].Codec == "" {
			t.Fatalf("first run's slots = %+v, want two with a codec", first)
		}

		// The run ends in the store a moment after the manager goes idle.
		h.waitFor("the record removed", func() bool {
			return h.m.DeleteRecord(context.Background(), domain.ResourceAlbum, "100") == nil
		})
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album again", h.albumComplete("100"))
		h.waitFor("the second run to end", h.idle())
		if got := h.itemStatuses(domain.ResourceAlbum, "100"); got["1001"] != domain.ItemSkipped || got["1002"] != domain.ItemSkipped {
			t.Fatalf("second run's items = %v, want both adopted", got)
		}
		again := h.files(domain.ResourceAlbum, "100")
		for i, f := range again {
			if f.AudioState != domain.AudioDone || f.Codec != first[i].Codec || f.QualityLabel != first[i].QualityLabel {
				t.Errorf("adopted slot %s = %s %q %q, want done %q %q as the first run", f.SongID, f.AudioState, f.Codec, f.QualityLabel, first[i].Codec, first[i].QualityLabel)
			}
		}
	})
}

// Two editions whose template gives one path: the second fails with
// path_taken before it downloads and is given up at once, and the first
// edition's file is untouched.
func TestRecordRunPathTakenFailsTheSecondEdition(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		catalog.addAlbum("101", "Album", "Artist", 1, 1, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the first edition", h.albumComplete("100"))
		h.waitFor("the first run to end", h.idle())
		h.submit("https://music.apple.com/cn/album/album/101")
		h.waitFor("the second edition's run", h.lastRunEnded(domain.ResourceAlbum, "101", 1))
		f := h.files(domain.ResourceAlbum, "101")[0]
		if f.AudioState != domain.AudioGivenUp || !strings.Contains(f.LastError, "path_taken") || f.Path != "" {
			t.Fatalf("second edition's slot = %+v, want given up with path_taken and no path", f)
		}
		if first := h.files(domain.ResourceAlbum, "100")[0]; first.AudioState != domain.AudioDone {
			t.Fatalf("first edition's slot = %+v, want still done", first)
		}
	})
}

// A restart in the middle of a record run: the run stays running in the
// store, the next process recovers it under the same id, the slots it left
// running are planned again on their own items, and the album completes.
func TestRecordRunRecoversAfterARestart(t *testing.T) {
	bubble(t, func(t *testing.T) {
		slow := testConfig(t)
		slow.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 50, MaxSpeedKBps: 50}
		catalog := newCatalog()
		// About 40 s per track at 50 KB/s, so the shutdown lands mid-transfer.
		catalog.addAlbum("100", "Album", "Artist", 3, 3, 60_000, "")
		first := newHarness(t, slow, nil, catalog, nil)
		first.m.Start(context.Background())
		resp := first.submit("https://music.apple.com/cn/album/album/100")
		runID := resp.Results[0].Job.ID
		first.waitFor("a slot to be downloading", first.slotRunning(domain.ResourceAlbum, "100"))
		first.stop()
		run, err := first.store.GetJob(context.Background(), runID)
		if err != nil || run.Status != domain.JobRunning {
			t.Fatalf("after shutdown the run = %+v (err %v), want still running", run, err)
		}

		fast := slow
		fast.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 1_000_000, MaxSpeedKBps: 1_000_000}
		second := newHarness(t, fast, first.store, catalog, nil)
		second.start()
		second.waitFor("the album after the restart", second.albumComplete("100"))
		second.waitFor("the run to end", second.idle())

		runs := second.runsOf(domain.ResourceAlbum, "100")
		if len(runs) != 1 || runs[0].ID != runID || runs[0].Status != domain.JobCompleted {
			t.Fatalf("runs = %+v, want the one run %s recovered and completed", runs, runID)
		}
		items, err := second.store.ListItems(context.Background(), runID)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 3 {
			t.Fatalf("run items = %d, want one per slot (3)", len(items))
		}
		for _, f := range second.files(domain.ResourceAlbum, "100") {
			if f.AudioState != domain.AudioDone || f.Failures != 0 || f.LastRunID != runID {
				t.Fatalf("slot %+v, want done by run %s without a failure", f, runID)
			}
		}
	})
}

// Another quality of a downloaded track lands next to it with a quality
// suffix; a codec the track does not offer leaves that slot unavailable, and
// the run still completes.
func TestRecordRunExtraSlotsAreSuffixedOrUnavailable(t *testing.T) {
	skipShort(t)
	manifest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio-stereo-256\",NAME=\"AAC\"\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=281000,AVERAGE-BANDWIDTH=256000,CODECS=\"mp4a.40.2\",AUDIO=\"audio-stereo-256\"\n" +
				"media.m3u8\n"))
		case "/media.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/P000000000/s1/e1/c23\"\n" +
				"#EXT-X-MAP:URI=\"encrypted.mp4\"\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer manifest.Close()
	cfg := testConfig(t)
	catalog := newCatalog()
	catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, manifest.URL+"/master.m3u8")
	h := newHarness(t, cfg, nil, catalog, nil)
	h.start()
	h.submit("https://music.apple.com/cn/album/album/100")
	h.waitFor("the main quality", h.albumComplete("100"))
	h.waitFor("the first run to end", h.idle())

	ctx := context.Background()
	for _, codec := range []string{"aac", "alac"} {
		key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "100", SongID: "1001", Slot: codec}
		if err := h.store.RequestSlot(ctx, key, time.Now().UTC()); err != nil {
			t.Fatalf("request %s: %v", codec, err)
		}
	}
	// A requested slot can start a run of its own; the reset waits for it.
	h.waitFor("the reset", func() bool {
		_, err := h.m.Reset(ctx, jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio, Slot: "aac"})
		if errors.Is(err, db.ErrRunActive) {
			return false
		}
		if err != nil {
			t.Fatal(err)
		}
		return true
	})
	h.waitFor("the extra slots", func() bool {
		states := map[string]domain.AudioState{}
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			states[f.Slot] = f.AudioState
		}
		return states["aac"] == domain.AudioDone && states["alac"] == domain.AudioUnavailable && h.idle()()
	})
	byslot := map[string]domain.File{}
	for _, f := range h.files(domain.ResourceAlbum, "100") {
		byslot[f.Slot] = f
	}
	dir := filepath.Join("library", "Artist", "Album")
	if got := byslot[domain.SlotMain].Path; got != filepath.Join(dir, "01. Song 1.m4a") {
		t.Fatalf("main path = %q", got)
	}
	if got := byslot["aac"].Path; got != filepath.Join(dir, "01. Song 1 [AAC 256Kbps].m4a") {
		t.Fatalf("aac path = %q, want the main name with the quality suffix", got)
	}
	runs := h.runsOf(domain.ResourceAlbum, "100")
	if runs[0].Status != domain.JobCompleted {
		t.Fatalf("the extra-slot run = %s (%s), want completed: unavailable is not a failure", runs[0].Status, runs[0].Error)
	}
}

// A record the account's storefront answers 404 for is marked
// not_in_storefront, and its run fails without counting a failure against
// any slot.
func TestRecordRunNotInStorefront(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the first run to end", h.idle())
		catalog.mu.Lock()
		delete(catalog.albums, "100")
		catalog.mu.Unlock()
		if _, err := h.m.Reset(context.Background(), jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio}); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the reset run", h.lastRunEnded(domain.ResourceAlbum, "100", 2))
		if album := h.album("100"); album.CatalogState != domain.CatalogNotInStorefront {
			t.Fatalf("album = %+v, want not_in_storefront", album)
		}
		if f := h.files(domain.ResourceAlbum, "100")[0]; f.Failures != 0 {
			t.Fatalf("slot = %+v, want no failure counted", f)
		}
	})
}

// A record its storefront does not have is found in another logged-in
// account's storefront: a Retry (an audio reset) reads it there and moves
// its pin, and so does an add of a link of that storefront.
func TestARecordNotInItsStorefrontIsFoundInAnotherAccounts(t *testing.T) {
	for _, how := range []string{"retry", "link"} {
		t.Run(how, func(t *testing.T) {
			bubble(t, func(t *testing.T) {
				cfg := testConfig(t)
				catalog := newCatalog()
				catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
				catalog.only = map[string]string{"100": "jp"}
				tokens := usertoken.New()
				tokens.SetAccounts([]usertoken.Account{{ID: "a-cn", Region: "cn", Position: 0, Active: true}})
				h := newHarness(t, cfg, nil, catalog, tokens)
				h.start()
				h.submit("https://music.apple.com/cn/album/album/100")
				h.waitFor("the first run to end", h.lastRunEnded(domain.ResourceAlbum, "100", 1))
				if album := h.album("100"); album.CatalogState != domain.CatalogNotInStorefront || album.PinnedStorefront != "cn" {
					t.Fatalf("album = %s pinned %q, want not_in_storefront pinned cn", album.CatalogState, album.PinnedStorefront)
				}

				// The jp account logs in.
				tokens.SetAccounts([]usertoken.Account{{ID: "a-cn", Region: "cn", Position: 0, Active: true}, {ID: "a-jp", Region: "jp", Position: 1, Active: true}})
				if how == "retry" {
					if _, err := h.m.Reset(context.Background(), jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio,
						States: []domain.AudioState{domain.AudioFailed, domain.AudioGivenUp, domain.AudioMissing}}); err != nil {
						t.Fatal(err)
					}
				} else {
					h.submit("https://music.apple.com/jp/album/album/100")
				}
				h.waitFor("the album", h.albumComplete("100"))
				h.waitFor("the run to end", h.idle())
				if album := h.album("100"); album.CatalogState != domain.CatalogOK || album.PinnedStorefront != "jp" {
					t.Errorf("album = %s pinned %q, want ok pinned jp", album.CatalogState, album.PinnedStorefront)
				}
			})
		})
	}
}

// Pausing a record cancels its run; the slots it was downloading go back to
// pending without a failure, and Resume downloads the rest in a new run.
func TestRecordRunPauseAndResume(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Simulate = config.SimulateConfig{Enabled: true, MinSpeedKBps: 100, MaxSpeedKBps: 100}
		catalog := newCatalog()
		// About 3 s per track at 100 KB/s.
		catalog.addAlbum("100", "Album", "Artist", 2, 2, 10_000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		resp := h.submit("https://music.apple.com/cn/album/album/100")
		first := resp.Results[0].Job.ID
		h.waitFor("a slot to be downloading", h.slotRunning(domain.ResourceAlbum, "100"))
		ctx := context.Background()
		if err := h.m.Pause(ctx, domain.ResourceAlbum, "100"); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the paused run to end", func() bool {
			run, err := h.store.GetJob(ctx, first)
			return err == nil && run.Status.IsTerminal()
		})
		if run, _ := h.store.GetJob(ctx, first); run.Status != domain.JobCancelled || run.Error != "paused" {
			t.Fatalf("paused run = %s %q, want cancelled as paused", run.Status, run.Error)
		}
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			if f.AudioState != domain.AudioPending || f.Failures != 0 {
				t.Fatalf("slot after pause = %+v, want pending without a failure", f)
			}
		}
		if err := h.m.Resume(ctx, domain.ResourceAlbum, "100"); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the resumed album", h.albumComplete("100"))
		h.waitFor("the resumed run to end", h.idle())
		runs := h.runsOf(domain.ResourceAlbum, "100")
		if len(runs) != 2 || runs[0].ID == first || runs[0].Status != domain.JobCompleted {
			t.Fatalf("runs = %+v, want a second, completed run", runs)
		}
	})
}

// A lyrics reset refreshes the lyrics of a done slot without downloading its
// audio again, and a covers reset writes the covers again; both leave the
// slot's file and audio state as they are.
func TestRecordLyricsAndCoversResets(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Download.SaveAlbumCover = true
		cfg.Download.EmbedLyrics = true
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the first run to end", h.idle())
		before := h.files(domain.ResourceAlbum, "100")[0]
		if before.LyricsState != domain.LyricsNone || h.album("100").CoverState != domain.CoverDone {
			t.Fatalf("after the download: lyrics %q, cover %q; want none and done", before.LyricsState, h.album("100").CoverState)
		}

		catalog.mu.Lock()
		song := catalog.songs["1001"]
		song.HasLyrics = true
		catalog.songs["1001"] = song
		catalog.mu.Unlock()
		ctx := context.Background()
		if _, err := h.m.Reset(ctx, jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetLyrics}); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the lyrics run", h.lastRunEnded(domain.ResourceAlbum, "100", 2))
		after := h.files(domain.ResourceAlbum, "100")[0]
		if after.LyricsState != domain.LyricsFetched || after.AudioState != domain.AudioDone || after.Path != before.Path ||
			after.Codec != before.Codec || after.LastRunID != before.LastRunID {
			t.Fatalf("after the lyrics run: %+v, want lyrics fetched and the audio slot unchanged", after)
		}
		if run := h.runsOf(domain.ResourceAlbum, "100")[0]; run.Status != domain.JobCompleted || run.CompletedItems != 1 {
			t.Fatalf("lyrics run = %s, %d completed (%s)", run.Status, run.CompletedItems, run.Error)
		}

		if _, err := h.m.Reset(ctx, jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetCovers}); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the covers run", h.lastRunEnded(domain.ResourceAlbum, "100", 3))
		if run := h.runsOf(domain.ResourceAlbum, "100")[0]; run.Status != domain.JobCompleted {
			t.Fatalf("covers run = %s (%s)", run.Status, run.Error)
		}
		if album := h.album("100"); album.CoverState != domain.CoverDone || album.State != domain.RecordComplete {
			t.Fatalf("after the covers run: %+v", album)
		}
	})
}

// A lyrics reset after Apple dropped one of an album's downloaded tracks
// refreshes the tracks still listed and leaves the dropped one's file alone,
// instead of failing on a planned song the read no longer lists.
func TestRecordLyricsResetSkipsATrackNoLongerListed(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Download.EmbedLyrics = true
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 2, 2, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the first run to end", h.idle())

		catalog.mu.Lock()
		album := catalog.albums["100"]
		album.Tracks = album.Tracks[:1]
		catalog.albums["100"] = album
		song := catalog.songs["1001"]
		song.HasLyrics = true
		catalog.songs["1001"] = song
		catalog.mu.Unlock()
		if _, err := h.m.Reset(context.Background(), jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetLyrics}); err != nil {
			t.Fatal(err)
		}
		h.waitFor("the lyrics run", h.lastRunEnded(domain.ResourceAlbum, "100", 2))
		if run := h.runsOf(domain.ResourceAlbum, "100")[0]; run.Status != domain.JobCompleted || run.TotalItems != 1 {
			t.Fatalf("lyrics run = %s with %d items (%s), want completed over the listed track", run.Status, run.TotalItems, run.Error)
		}
		for _, f := range h.files(domain.ResourceAlbum, "100") {
			want := domain.LyricsFetched
			if f.SongID == "1002" {
				want = domain.LyricsNone
			}
			if f.AudioState != domain.AudioDone || f.LyricsState != want {
				t.Fatalf("slot %s = %s, lyrics %q; want done with lyrics %q", f.SongID, f.AudioState, f.LyricsState, want)
			}
		}
	})
}

// A slot that only claimed a path before, and never completed, is named by
// the template again: with {Codec} in the file name, a run that lands
// AAC-LC after an ALAC attempt was released names the file AAC-LC.
func TestRecordRunRenamesASlotThatNeverCompleted(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Download.LibraryPathFormat = "library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName} [{Codec} {Quality}]"
		catalog := newCatalog()
		album := catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		ctx := context.Background()
		now := time.Now().UTC()
		if _, err := h.store.AddAlbum(ctx, db.AlbumAdd{ID: "100", WantedAll: true, Source: domain.SourceManual, Now: now}); err != nil {
			t.Fatal(err)
		}
		if err := h.store.ApplyAlbumRead(ctx, albumRead(album, "cn")); err != nil {
			t.Fatal(err)
		}
		key := domain.FileKey{OwnerKind: domain.ResourceAlbum, OwnerID: "100", SongID: "1001", Slot: domain.SlotMain}
		alac := filepath.Join(cfg.Download.DownloadsDir, "library", "Artist", "Album", "01. Song 1 [ALAC 24-bit_96 kHz].m4a")
		if err := h.store.ClaimPath(ctx, key, alac, "run-earlier", now); err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.ReleaseRunSlots(ctx, "run-earlier", now); err != nil {
			t.Fatal(err)
		}
		// The record is already added, so the picker queues its run; a submit
		// here would race it and could answer duplicate_active.
		h.start()
		h.waitFor("the album", h.albumComplete("100"))
		h.waitFor("the run to end", h.idle())
		f := h.files(domain.ResourceAlbum, "100")[0]
		if want := "library/Artist/Album/01. Song 1 [AAC-LC 256Kbps].m4a"; f.Codec != "aac-lc" || f.Path != want {
			t.Fatalf("slot = %s at %q, want aac-lc at %q", f.Codec, f.Path, want)
		}
	})
}

// The animated cover the backfill finds is stored on the album record.
func TestRecordRunStoresMotionArtworkOnTheAlbum(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		cfg.Catalog.MotionArtworkEnabled = true
		catalog := newCatalog()
		catalog.motion = applemusic.MotionArtwork{Square: "square.m3u8", Tall: "tall.m3u8", SquareColors: applemusic.ArtworkColors{BgColor: "112233"}}
		catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		h := newHarness(t, cfg, nil, catalog, nil)
		h.start()
		h.submit("https://music.apple.com/cn/album/album/100")
		h.waitFor("the motion artwork", func() bool {
			album, err := h.store.GetAlbum(context.Background(), "100")
			return err == nil && album.Artwork.MotionSquareURL == "square.m3u8"
		})
		if art := h.album("100").Artwork; art.MotionTallURL != "tall.m3u8" || art.MotionSquareColors.Bg != "112233" {
			t.Fatalf("artwork = %+v", art)
		}
		h.waitFor("the run to end", h.idle())
	})
}

// A reset downloads a done slot again to its stored path, though the
// template changed since, and replaces the file there.
func TestRecordResetKeepsTheStoredPath(t *testing.T) {
	bubble(t, func(t *testing.T) {
		cfg := testConfig(t)
		catalog := newCatalog()
		catalog.addAlbum("100", "Album", "Artist", 1, 1, 1000, "")
		first := newHarness(t, cfg, nil, catalog, nil)
		first.start()
		first.submit("https://music.apple.com/cn/album/album/100")
		first.waitFor("the album", first.albumComplete("100"))
		first.waitFor("the first run to end", first.idle())
		before := first.files(domain.ResourceAlbum, "100")[0]
		first.stop()

		moved := cfg
		moved.Download.LibraryPathFormat = "new/{AlbumArtist}/{AlbumName} [{AlbumId}]/{SongNumber:02d} {SongName}"
		second := newHarness(t, moved, first.store, catalog, nil)
		second.start()
		result, err := second.m.Reset(context.Background(), jobs.ResetRequest{Kind: domain.ResourceAlbum, ID: "100", What: jobs.ResetAudio})
		if err != nil || result.Run == nil {
			t.Fatalf("reset = %+v, %v; want a run", result, err)
		}
		second.waitFor("the reset run", func() bool {
			run, err := second.store.GetJob(context.Background(), result.Run.ID)
			return err == nil && run.Status.IsTerminal()
		})
		after := second.files(domain.ResourceAlbum, "100")[0]
		if after.AudioState != domain.AudioDone || after.Path != before.Path || after.LastRunID != result.Run.ID {
			t.Fatalf("after the reset: %+v, want done again at %q by run %s", after, before.Path, result.Run.ID)
		}
		items, err := second.store.ListItems(context.Background(), result.Run.ID)
		if err != nil || len(items) != 1 || items[0].Status != domain.ItemCompleted {
			t.Fatalf("reset run items = %+v (err %v), want one completed, not adopted", items, err)
		}
	})
}

// albumRead is the store's read of album in storefront, as a run applies it.
func albumRead(album applemusic.Collection, storefront string) db.AlbumRead {
	read := db.AlbumRead{
		Album:      domain.Album{ID: album.ID, Name: album.Name, ArtistName: album.Artist, ReleaseTrackCount: album.TrackCount},
		Storefront: storefront,
	}
	for i, t := range album.Tracks {
		read.Tracks = append(read.Tracks, domain.Track{
			AlbumID: album.ID, SongID: t.ID, DiscNumber: t.DiscNumber, TrackNumber: t.TrackNumber, SongNumber: i + 1,
			Name: t.Name, ArtistName: t.ArtistName, DurationMS: t.DurationInMillis,
		})
	}
	return read
}

// alacInit is the init segment of an ALAC track: what a file at a slot's
// path needs for its codec to be read.
func alacInit(t *testing.T) []byte {
	t.Helper()
	init := mp4.CreateEmptyInit()
	trak := init.AddEmptyTrack(48000, "audio", "und")
	cookie := &mp4.AlacBox{FrameLength: 4096, BitDepth: 24, Pb: 40, Mb: 10, Kb: 14, NumChannels: 2, MaxRun: 255, SampleRate: 48000}
	trak.Mdia.Minf.Stbl.Stsd.AddChild(mp4.CreateAudioSampleEntryBox("alac", 2, 24, 0, cookie))
	var buf bytes.Buffer
	if err := init.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
