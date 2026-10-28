package applemusic

import (
	"log/slog"
	"net/http"

	"amdl/internal/config"
)

// Most catalog tests predate the startup request-pool settings and construct
// only the fields relevant to the behavior under test. Give those fixtures a
// fast gate explicitly; production still treats configured values <=0 as 1.
func newTestCatalogClient(cfg config.CatalogConfig, logger *slog.Logger) *CatalogClient {
	if cfg.MaxParallelRequests <= 0 {
		cfg.MaxParallelRequests = 64
	}
	if cfg.RequestsPerSecond <= 0 {
		cfg.RequestsPerSecond = 64
	}
	if cfg.RequestBurst <= 0 {
		cfg.RequestBurst = 64
	}
	return NewCatalogClient(cfg, logger)
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
