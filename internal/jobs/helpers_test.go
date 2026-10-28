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
	return ValidationResult{Type: "song", Storefront: "cn"}, nil
}

func (recoveryProcessor) ProcessJob(context.Context, domain.Job, Reporter) error {
	return nil
}

// keyedProcessor derives ValidationResult from a "type|storefront|id" test
// URL so canonical-key dedup can be exercised without real Apple Music
// parsing. URLs prefixed "bad:" are treated as invalid.
type keyedProcessor struct{}

func (keyedProcessor) ValidateRequest(_ context.Context, url string) (ValidationResult, error) {
	if strings.HasPrefix(url, "bad:") {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "bad test url"}
	}
	parts := strings.SplitN(url, "|", 3)
	if len(parts) != 3 {
		return ValidationResult{}, &RequestError{Code: "invalid_url", Message: "malformed test url"}
	}
	return ValidationResult{Type: parts[0], Storefront: parts[1], ID: parts[2]}, nil
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
