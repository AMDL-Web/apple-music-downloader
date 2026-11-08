package ui

import (
	"net/http"
	"testing"
	"time"

	"amdl/apps/desktop/internal/artwork"
	"amdl/apps/desktop/internal/mode"
	"amdl/apps/desktop/internal/store"
	"amdl/apps/desktop/internal/testkit"
	"amdl/internal/client"
	"amdl/internal/demo"
)

// refresh is one display frame at 60 Hz.
const refresh = time.Second / 60

// threadApp is an App whose store reads c and, when covers is set, whose
// covers come through it. What they post waits on the test's thread until
// run applies it, in order, as the UI thread would (testkit.Thread), and
// the store's pauses are over at once.
func threadApp(t *testing.T, s mode.Settings, c *client.Client, covers http.RoundTripper) (*App, func(until func() bool)) {
	t.Helper()
	a, th := threadAppOn(t, s, c, covers)
	return a, th.Until
}

// threadAppOn is threadApp returning the thread itself.
func threadAppOn(t *testing.T, s mode.Settings, c *client.Client, covers http.RoundTripper) (*App, *testkit.Thread) {
	t.Helper()
	a, _ := newTestApp(s)
	th := testkit.NewThread(t)
	a.Store = store.New(th.Post, nil)
	a.Store.SetAfter(testkit.Soon)
	if covers != nil {
		a.Art = artwork.NewWith(th.Post, covers)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	return a, th
}

// demoApp is an App on the demo's in-memory backend (threadApp), covers
// included.
func demoApp(t *testing.T) (*App, func(until func() bool)) {
	t.Helper()
	b, c := testkit.Demo(t, time.Now)
	return threadApp(t, remoteSettings(), c, b.Transport())
}

// demoOver is demoApp with routes answered by the test (testkit.Override).
func demoOver(t *testing.T, routes map[string]http.HandlerFunc) (*App, func(until func() bool)) {
	t.Helper()
	rt := testkit.Override(demo.New(time.Now).Transport(), routes)
	return threadApp(t, remoteSettings(), testkit.Client(t, demo.Address, rt), rt)
}

// liveApp is an App on fakeAmdl, on a local listener (threadApp). A flash
// of the backdrop's light counts as one of anything else.
func liveApp(t *testing.T, s mode.Settings) (*App, func(until func() bool)) {
	t.Helper()
	a, th := threadAppOn(t, s, testkit.Serve(t, http.HandlerFunc(fakeAmdl)), nil)
	a.FlatBackdrop = false
	// The flicker tests look at the frames between a request and its
	// answer, so run stops at the first state where its condition holds.
	return a, th.UntilExact
}
