package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"amdl/internal/db"
	"amdl/internal/domain"
)

var (
	allowedListStatuses = map[domain.JobStatus]struct{}{
		domain.JobQueued: {}, domain.JobRunning: {}, domain.JobCompleted: {}, domain.JobFailed: {}, domain.JobCancelled: {},
	}
	allowedListTypes = map[string]struct{}{
		"song": {}, "album": {}, "playlist": {}, "artist": {}, "station": {},
	}
)

func parseJobListFilter(r *http.Request) (db.JobListFilter, error) {
	q := r.URL.Query()
	filter := db.JobListFilter{
		Query: strings.TrimSpace(q.Get("q")),
		Sort:  strings.TrimSpace(q.Get("sort")),
		Order: strings.TrimSpace(strings.ToLower(q.Get("order"))),
	}

	if raw := strings.TrimSpace(q.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > 200 {
			return filter, fmt.Errorf("limit must be an integer between 1 and 200")
		}
		filter.Limit = limit
	}
	if raw := strings.TrimSpace(q.Get("offset")); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return filter, fmt.Errorf("offset must be a non-negative integer")
		}
		filter.Offset = offset
	}

	for _, st := range parseCSVQuery(q, "status") {
		js := domain.JobStatus(st)
		if _, ok := allowedListStatuses[js]; !ok {
			return filter, fmt.Errorf("status %q is not supported; allowed: queued, running, completed, failed, cancelled", st)
		}
		filter.Statuses = append(filter.Statuses, js)
	}

	for _, t := range parseCSVQuery(q, "type") {
		if _, ok := allowedListTypes[t]; !ok {
			return filter, fmt.Errorf("type %q is not supported; allowed: song, album, playlist, artist, station", t)
		}
		filter.Types = append(filter.Types, t)
	}

	if filter.Sort != "" && filter.Sort != db.JobListSortCreatedAt && filter.Sort != db.JobListSortUpdatedAt {
		return filter, fmt.Errorf("sort must be created_at or updated_at")
	}
	if filter.Order != "" && filter.Order != db.JobListOrderAsc && filter.Order != db.JobListOrderDesc {
		return filter, fmt.Errorf("order must be asc or desc")
	}

	filter.Normalize()
	return filter, nil
}

// parseCSVQuery collects values for key from both repeated query params and
// comma-separated entries (status=a&status=b and status=a,b are equivalent).
func parseCSVQuery(q url.Values, key string) []string {
	raw := q[key]
	if len(raw) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, part := range raw {
		for _, item := range strings.Split(part, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			out = append(out, item)
		}
	}
	return out
}
