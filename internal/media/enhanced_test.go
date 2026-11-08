package media

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

func TestSelectEnhancedMediaDoesNotDownloadEncryptedMedia(t *testing.T) {
	var masterHits atomic.Int32
	var mediaPlaylistHits atomic.Int32
	var encryptedMediaHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			masterHits.Add(1)
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"audio-alac-stereo-96000-24\",NAME=\"Lossless\",BIT-DEPTH=24,SAMPLE-RATE=96000\n" +
				"#EXT-X-STREAM-INF:BANDWIDTH=3000000,AVERAGE-BANDWIDTH=2500000,AUDIO=\"audio-alac-stereo-96000-24\",CODECS=\"alac\"\n" +
				"media.m3u8\n"))
		case "/media.m3u8":
			mediaPlaylistHits.Add(1)
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"skd://itunes.apple.com/P000000000/s1/e1/c23\"\n" +
				"#EXT-X-MAP:URI=\"encrypted.mp4\"\n"))
		case "/encrypted.mp4":
			encryptedMediaHits.Add(1)
			_, _ = w.Write([]byte("encrypted media bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Download.TempDir = t.TempDir()
	downloader := newTestDownloader(&Downloader{
		cfg:     cfg,
		http:    server.Client(),
		wrapper: fakeDownloaderWrapper{},
	})
	item := &domain.JobItem{ID: "item-1"}
	reporter := &recordingReporter{}
	selected, err := downloader.selectEnhancedMedia(
		context.Background(),
		domain.Job{ID: "job-1"},
		item,
		applemusic.Song{ID: "song-1", EnhancedHLS: server.URL + "/master.m3u8"},
		"alac",
		reporter,
		func(domain.ItemStatus, string, func(*domain.ItemProgress)) {},
	)
	if err != nil {
		t.Fatal(err)
	}
	if masterHits.Load() != 1 || mediaPlaylistHits.Load() != 1 {
		t.Fatalf("selection playlist requests = master:%d media:%d, want master:1 media:1", masterHits.Load(), mediaPlaylistHits.Load())
	}
	if encryptedMediaHits.Load() != 0 {
		t.Fatalf("selectEnhancedMedia downloaded encrypted media %d time(s), want 0", encryptedMediaHits.Load())
	}
	if selected.rawPath != "" {
		t.Fatalf("selected.rawPath = %q, want empty before download step", selected.rawPath)
	}
	if len(selected.raw) != 0 {
		t.Fatalf("selected.raw has %d bytes before download step, want 0", len(selected.raw))
	}
	if got, want := qualityLabel(selected.info), "24-bit/96 kHz"; got != want {
		t.Fatalf("qualityLabel = %q, want %q", got, want)
	}
	if item.BitDepth != 24 || item.SampleRate != 96000 || item.Bitrate != 2500000 {
		t.Fatalf("item quality = %+v, want bit_depth=24 sample_rate=96000 bitrate=2500000", item)
	}
	if len(reporter.items) == 0 {
		t.Fatal("selectEnhancedMedia did not persist the resolved quality via UpdateItem")
	}
	last := reporter.items[len(reporter.items)-1]
	if last.BitDepth != 24 || last.SampleRate != 96000 || last.Bitrate != 2500000 {
		t.Fatalf("persisted item quality = %+v, want bit_depth=24 sample_rate=96000 bitrate=2500000", last)
	}

	selected, err = downloader.downloadSelectedEnhancedMedia(context.Background(), selected, "alac", "job-1", filepath.Join(cfg.Download.DownloadsDir, "song.m4a"), &keyPrefetch{}, func(domain.ItemStatus, string, func(*domain.ItemProgress)) {})
	if err != nil {
		t.Fatal(err)
	}
	if encryptedMediaHits.Load() != 1 {
		t.Fatalf("downloadSelectedEnhancedMedia downloaded encrypted media %d time(s), want 1", encryptedMediaHits.Load())
	}
	if selected.rawPath == "" {
		t.Fatal("downloadSelectedEnhancedMedia did not set rawPath after download step")
	}
	gotRaw, err := os.ReadFile(selected.rawPath)
	if err != nil {
		t.Fatalf("read downloaded media file: %v", err)
	}
	if string(gotRaw) != "encrypted media bytes" {
		t.Fatalf("downloaded media = %q, want encrypted media bytes", string(gotRaw))
	}
	if len(selected.raw) != 0 {
		t.Fatalf("download retained %d bytes in memory, want 0", len(selected.raw))
	}
}
