package ui

import (
	"testing"
	"time"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
	"amdl/internal/domain"
)

// The words kept for the frames after say what the functions they keep
// say, for each kind of input the key has to tell apart.
func TestKeptWordsSayWhatTheyKeep(t *testing.T) {
	a, _ := newTestApp(remoteSettings())
	done := time.Date(2026, 11, 2, 9, 16, 0, 0, time.Local)
	jobs := []*store.Job{
		{Job: domain.Job{Type: "album", Title: "Glass Weather - EP", ArtistName: "Mira Solenne", Status: domain.JobCompleted, FinishedAt: &done, TotalItems: 6, CompletedItems: 6}},
		{Job: domain.Job{Type: "playlist", Title: "Chill", CuratorName: "Apple Music", Status: domain.JobRunning, TotalItems: 40, CompletedItems: 12}, Progress: 0.374},
		{Job: domain.Job{Type: "song", Title: "Elma", Status: domain.JobQueued}},
		{Job: domain.Job{Type: "album", Title: "Late Bloom", ArtistName: "The Velvet Orchard", Status: domain.JobFailed, TotalItems: 5, CompletedItems: 2, SkippedItems: 1},
			Items: []domain.JobItem{{Status: domain.ItemFailed}, {Status: domain.ItemCompleted, Codec: "alac", BitDepth: 24, SampleRate: 96000}}},
	}
	for _, j := range jobs {
		for range 2 {
			if got, want := a.jobSubtitle(j), rowSubtitle(j); got != want {
				t.Errorf("%s: subtitle %q, want %q", j.Title, got, want)
			}
			if got, want := a.jobCount(j), rowCount(j); got != want {
				t.Errorf("%s: count %q, want %q", j.Title, got, want)
			}
			if got, want := a.jobQualityText(j), jobQuality(j); got != want {
				t.Errorf("%s: quality %q, want %q", j.Title, got, want)
			}
		}
	}
	hits := []client.SearchHit{
		{Type: "album", ID: "1", Name: "Glass Weather - EP", ArtistName: "Mira Solenne", ReleaseDate: "2025-03-01", AudioTraits: []string{"lossless", "hi-res-lossless", "atmos"}},
		{Type: "album", ID: "2", Name: "Tidal Rooms", ArtistName: "Mira Solenne", ReleaseDate: "2022", AudioTraits: []string{"lossless"}},
		{Type: "artist", ID: "3", Name: "MyGO!!!!!", Genre: "Anime"},
		{Type: "song", ID: "4", Name: "Elma", ArtistName: "Yorushika", AlbumName: "Elma - Single", DurationMS: 245000},
	}
	for _, h := range hits {
		for _, size := range []float32{150, 220} {
			if got, want := a.tileHints(h, size), shortHints(h.AudioTraits, size); got != want {
				t.Errorf("%s at %v: hints %q, want %q", h.Name, size, got, want)
			}
		}
		if got, want := a.tileLine(h), joinDots(tileFacts(h)); got != want {
			t.Errorf("%s: facts %q, want %q", h.Name, got, want)
		}
		if got, want := a.songLine(h), joinDots(hitFacts(h)); got != want {
			t.Errorf("%s: song facts %q, want %q", h.Name, got, want)
		}
		if got, want := a.hitKey(h), store.HitKey(h); got != want {
			t.Errorf("%s: key %q, want %q", h.Name, got, want)
		}
	}
	for _, it := range []domain.JobItem{
		{Status: domain.ItemQueued},
		{Status: domain.ItemDownloading, Attempt: 2, MaxAttempts: 3, Codec: "alac", BitDepth: 16, SampleRate: 44100},
		{Status: domain.ItemCompleted, Codec: "aac", Bitrate: 256, FileSize: 8_400_000},
		{Status: domain.ItemFailed, StatusMessage: "license refused"},
	} {
		if got, want := a.trackSubText(&it), trackSub(&it); got != want {
			t.Errorf("%+v: %q, want %q", it, got, want)
		}
	}
}

func joinDots(parts []string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += " · "
		}
		s += p
	}
	return s
}
