package jobs

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"amdl/internal/db"
	"amdl/internal/domain"
	"amdl/internal/events"
)

type recoveryProcessor struct{}

func (recoveryProcessor) ValidateRequest(context.Context, string) (ValidationResult, error) {
	return ValidationResult{Type: "station", Storefront: "cn"}, nil
}

func (recoveryProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return nil
}

// keyedProcessor derives ValidationResult from a "type|storefront|id" test
// URL, or "song|storefront|id|albumID" for an album-track link, so adds can
// be exercised without real Apple Music parsing. URLs prefixed "bad:" are
// treated as invalid.
type keyedProcessor struct{}

func (keyedProcessor) ValidateRequest(_ context.Context, url string) (ValidationResult, error) {
	if strings.HasPrefix(url, "bad:") {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "bad test url"}
	}
	parts := strings.SplitN(url, "|", 4)
	if len(parts) < 3 {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	result := ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}
	if len(parts) == 4 {
		result.AlbumID = parts[3]
	}
	return result, nil
}

func (keyedProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return nil
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "amdl.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewManager(store, events.NewHub(), keyedProcessor{}, 1, slog.Default())
}

// isWaiting reports whether jobID is on m's wait list.
func isWaiting(m *Manager, jobID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.waiting.ids[jobID]
	return ok
}
