package api

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"amdl/internal/db"
)

// The days query parameter of GET /stats: default and inclusive bounds.
const (
	statsDefaultDays = 14
	statsMinDays     = 1
	statsMaxDays     = 90
)

// statsCacheTTL is how long a GET /stats result is reused per days value.
// The queries scan every retained item, and dashboards poll the endpoint.
const statsCacheTTL = 30 * time.Second

// statsResponse is the Stats schema of openapi.yaml.
type statsResponse struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Days        int          `json:"days"`
	Totals      statsTotals  `json:"totals"`
	Daily       []statsDay   `json:"daily"`
	Codecs      []statsCodec `json:"codecs"`
	Hooks       []statsHook  `json:"hooks"`
}

type statsTotals struct {
	Jobs            int   `json:"jobs"`
	TracksCompleted int   `json:"tracks_completed"`
	TracksSkipped   int   `json:"tracks_skipped"`
	TracksFailed    int   `json:"tracks_failed"`
	BytesWritten    int64 `json:"bytes_written"`
	DurationMS      int64 `json:"duration_ms"`
}

type statsDay struct {
	Date            string `json:"date"`
	TracksCompleted int    `json:"tracks_completed"`
	BytesWritten    int64  `json:"bytes_written"`
	JobsCompleted   int    `json:"jobs_completed"`
	JobsFailed      int    `json:"jobs_failed"`
}

type statsCodec struct {
	Codec      string `json:"codec"`
	BitDepth   int    `json:"bit_depth"`
	SampleRate int    `json:"sample_rate"`
	Tracks     int    `json:"tracks"`
}

type statsHook struct {
	Name         string     `json:"name"`
	Succeeded    int        `json:"succeeded"`
	Failed       int        `json:"failed"`
	LastFailedAt *time.Time `json:"last_failed_at,omitempty"`
}

// statsCache holds the last GET /stats result per days value. The zero value
// is ready to use.
type statsCache struct {
	mu      sync.Mutex
	entries map[int]statsResponse
}

// get returns the entry for days if it was generated less than statsCacheTTL
// before now.
func (c *statsCache) get(days int, now time.Time) (statsResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[days]
	if !ok || now.Sub(entry.GeneratedAt) >= statsCacheTTL || now.Before(entry.GeneratedAt) {
		return statsResponse{}, false
	}
	return entry, true
}

func (c *statsCache) put(entry statsResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[int]statsResponse, 1)
	}
	c.entries[entry.Days] = entry
}

// stats answers GET /stats?days=N. Two requests that miss the cache at the
// same time both query the store; the later result replaces the earlier.
func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	days := statsDefaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < statsMinDays || n > statsMaxDays {
			writeError(w, http.StatusBadRequest, CodeInvalidRequest, "days must be an integer between 1 and 90")
			return
		}
		days = n
	}
	now := s.clock().UTC()
	if cached, ok := s.statsCache.get(days, now); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}
	summary, err := s.store.Stats(r.Context(), now, days)
	if err != nil {
		writeStoreError(w, "read stats", err)
		return
	}
	resp := newStatsResponse(now, days, summary)
	s.statsCache.put(resp)
	writeJSON(w, http.StatusOK, resp)
}

func newStatsResponse(now time.Time, days int, summary db.Stats) statsResponse {
	t := summary.Totals
	resp := statsResponse{
		GeneratedAt: now,
		Days:        days,
		Totals: statsTotals{
			Jobs: t.Jobs, TracksCompleted: t.TracksCompleted, TracksSkipped: t.TracksSkipped,
			TracksFailed: t.TracksFailed, BytesWritten: t.BytesWritten, DurationMS: t.DurationMS,
		},
		Daily:  make([]statsDay, 0, len(summary.Daily)),
		Codecs: make([]statsCodec, 0, len(summary.Codecs)),
		Hooks:  make([]statsHook, 0, len(summary.Hooks)),
	}
	for _, d := range summary.Daily {
		resp.Daily = append(resp.Daily, statsDay{
			Date: d.Date, TracksCompleted: d.TracksCompleted, BytesWritten: d.BytesWritten,
			JobsCompleted: d.JobsCompleted, JobsFailed: d.JobsFailed,
		})
	}
	for _, c := range summary.Codecs {
		resp.Codecs = append(resp.Codecs, statsCodec{Codec: c.Codec, BitDepth: c.BitDepth, SampleRate: c.SampleRate, Tracks: c.Tracks})
	}
	for _, h := range summary.Hooks {
		resp.Hooks = append(resp.Hooks, statsHook{Name: h.Name, Succeeded: h.Succeeded, Failed: h.Failed, LastFailedAt: h.LastFailedAt})
	}
	return resp
}
