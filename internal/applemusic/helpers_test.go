package applemusic

import (
	"log/slog"
	"net/http"

	"amdl/internal/config"
	"amdl/internal/limits"
)

// Most catalog tests construct only the fields relevant to the behavior under
// test. Give those fixtures a fast gate explicitly (64 in parallel, 64 per
// second) instead of the production rate; production still treats a
// max_parallel_requests <= 0 as 1.
func newTestCatalogClient(cfg config.CatalogConfig, logger *slog.Logger) *CatalogClient {
	if cfg.MaxParallelRequests <= 0 {
		cfg.MaxParallelRequests = 64
	}
	c := NewCatalogClient(cfg, logger)
	c.gate = limits.NewRequestGate(cfg.MaxParallelRequests, 64, 64)
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func trackIDs(tracks []Song) []string {
	out := make([]string, 0, len(tracks))
	for _, track := range tracks {
		out = append(out, track.ID)
	}
	return out
}

func albumIDs(albums []Collection) []string {
	out := make([]string, 0, len(albums))
	for _, album := range albums {
		out = append(out, album.ID)
	}
	return out
}
