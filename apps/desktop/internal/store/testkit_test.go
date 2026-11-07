package store

import (
	"net/http"
	"testing"

	"amdl/apps/desktop/internal/testkit"
)

// live connects a store to h on a local listener. Its posts wait on the
// test's thread until settle runs them, as the UI thread runs them, and
// its pauses (SetAfter) are over at once.
func live(t *testing.T, h http.Handler) (*Store, func(until func() bool)) {
	t.Helper()
	th := testkit.NewThread(t)
	s := New(th.Post, nil)
	s.SetAfter(testkit.Soon)
	s.Connect(testkit.Serve(t, h))
	t.Cleanup(s.Close)
	return s, th.Until
}
