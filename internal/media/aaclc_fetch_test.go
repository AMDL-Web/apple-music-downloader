package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/domain"
)

// fakeAACLCWrapper overrides WebPlayback to point at a test playlist server,
// keeping every other method delegated to fakeDownloaderWrapper's no-ops.
type fakeAACLCWrapper struct {
	fakeDownloaderWrapper
	webPlaybackURL string
}

func (f fakeAACLCWrapper) WebPlayback(context.Context, string) (string, error) {
	return f.webPlaybackURL, nil
}

// TestFetchAACLCMediaCodecSelectedCarriesRESTItemSnapshot guards against the
// real (non-simulate) AAC-LC codec_selected event regressing back to the
// bare codec/attempt payload used before MarshalEventPayload was introduced;
// simulate.go's aac-lc branch and selectEnhancedMedia already cover the
// REST-snapshot fields, this covers the real download path.
func TestFetchAACLCMediaCodecSelectedCarriesRESTItemSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/track.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				"#EXT-X-KEY:METHOD=SAMPLE-AES-CTR,URI=\"data:;base64,MDEyMzQ1Njc4OWFiY2RlZg==\"\n" +
				"#EXT-X-MAP:URI=\"audio.m4a\"\n"))
		case "/audio.m4a":
			_, _ = w.Write([]byte("encrypted aac-lc bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := config.Default()
	cfg.Download.TempDir = t.TempDir()
	downloader := &Downloader{
		cfg:     cfg,
		http:    server.Client(),
		wrapper: fakeAACLCWrapper{webPlaybackURL: server.URL + "/track.m3u8"},
	}
	createdAt := time.Now().UTC().Add(-time.Minute)
	item := &domain.JobItem{
		ID: "item-1", ArtworkURL: "https://example.com/track/{w}x{h}bb.jpg",
		CreatedAt: createdAt, UpdatedAt: createdAt, Attempt: 2, MaxAttempts: 5,
	}
	reporter := &recordingReporter{}

	if _, _, release, err := downloader.fetchAACLCMedia(
		context.Background(),
		domain.Job{ID: "job-1"},
		item,
		applemusic.Song{ID: "song-1"},
		reporter,
		func(domain.ItemStatus, string, func(*domain.ItemProgress)) {},
	); err != nil {
		t.Fatal(err)
	} else if release != nil {
		defer release()
	}

	var found bool
	for _, ev := range reporter.events {
		if ev.Type != "codec_selected" || ev.Phase != "aac-lc" {
			continue
		}
		found = true
		var snapshot domain.JobItem
		if err := json.Unmarshal(ev.Payload, &snapshot); err != nil {
			t.Fatalf("decode codec_selected payload: %v", err)
		}
		if snapshot.ID != item.ID || snapshot.ArtworkURL != item.ArtworkURL || !snapshot.CreatedAt.Equal(createdAt) {
			t.Fatalf("codec_selected snapshot = %+v, want REST item identity/artwork/timestamps", snapshot)
		}
		var raw map[string]any
		if err := json.Unmarshal(ev.Payload, &raw); err != nil {
			t.Fatalf("decode codec_selected payload as map: %v", err)
		}
		if raw["codec_id"] != "aac-lc" || raw["attempt"] != float64(2) || raw["max_attempts"] != float64(5) {
			t.Fatalf("codec_selected payload = %+v, want codec_id/attempt/max_attempts merged in", raw)
		}
	}
	if !found {
		t.Fatal("missing codec_selected event for real AAC-LC download path")
	}
}
