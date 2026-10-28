package logging

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

func testConfig() config.LoggingConfig {
	cfg := config.Default().Logging
	cfg.Console = false
	cfg.BufferSize = 10
	return cfg
}

func TestSystemCapturesFiltersAndRedacts(t *testing.T) {
	system, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer system.Close()

	logger := system.Logger.With("component", "jobs", "job_id", "job-1")
	logger.Debug("hidden")
	logger.Info("download ready", "password", "plaintext",
		slog.Group("account", "authorization", "Bearer secret", "name", "alice"),
		"payload", map[string]any{"access_token": "nested-secret", "items": []any{map[string]any{"api_key": "nested-key"}}},
		"error", errors.New("failure"),
	)

	page := system.Store.List(Filter{Component: "jobs", JobID: "job-1", Query: "alice", Limit: 20})
	if page.NextCursor != 1 || len(page.Entries) != 1 {
		t.Fatalf("entries/cursor = %d/%d, want 1/1", len(page.Entries), page.NextCursor)
	}
	if page.Entries[0].Attributes["password"] != "[REDACTED]" {
		t.Fatalf("password not redacted: %#v", page.Entries[0].Attributes)
	}
	if page.Entries[0].Attributes["error"] != "failure" {
		t.Fatalf("error attr = %#v", page.Entries[0].Attributes["error"])
	}
	account, ok := page.Entries[0].Attributes["account"].(map[string]any)
	if !ok || account["authorization"] != "[REDACTED]" || account["name"] != "alice" {
		t.Fatalf("group redaction = %#v", page.Entries[0].Attributes["account"])
	}
	payload, ok := page.Entries[0].Attributes["payload"].(map[string]any)
	if !ok || payload["access_token"] != "[REDACTED]" {
		t.Fatalf("nested redaction = %#v", page.Entries[0].Attributes["payload"])
	}
	items, itemsOK := payload["items"].([]any)
	if !itemsOK || len(items) != 1 {
		t.Fatalf("nested items = %#v", payload["items"])
	}
	item, itemOK := items[0].(map[string]any)
	if !itemOK || item["api_key"] != "[REDACTED]" {
		t.Fatalf("nested item redaction = %#v", items[0])
	}
}

func TestHandlerPreservesBoundAttributeGroups(t *testing.T) {
	system, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	logger := system.Logger.With("root", "value").WithGroup("nested").With("child", "value")
	logger.Info("grouped")
	page := system.Store.List(Filter{Limit: 1})
	if page.Entries[0].Attributes["root"] != "value" {
		t.Fatalf("root attr moved into group: %#v", page.Entries[0].Attributes)
	}
	nested, ok := page.Entries[0].Attributes["nested"].(map[string]any)
	if !ok || nested["child"] != "value" {
		t.Fatalf("nested attr missing: %#v", page.Entries[0].Attributes)
	}
}

func TestCaptureRedactsSensitiveGroups(t *testing.T) {
	system, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	system.Logger.WithGroup("authorization").Info("record", "value", "record-secret")
	system.Logger.WithGroup("authorization").With("value", "bound-secret").Info("bound")
	page := system.Store.List(Filter{Limit: 2})
	if len(page.Entries) != 2 {
		t.Fatalf("entries = %#v", page.Entries)
	}
	for _, entry := range page.Entries {
		group, ok := entry.Attributes["authorization"].(map[string]any)
		if !ok || group["value"] != "[REDACTED]" {
			t.Fatalf("sensitive group leaked to capture store in %q: %#v", entry.Message, entry.Attributes)
		}
	}
}

func TestSystemUpdatesLevel(t *testing.T) {
	system, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	system.Logger.Debug("before")
	if err := system.SetLevel("debug"); err != nil {
		t.Fatal(err)
	}
	system.Logger.Debug("after")
	page := system.Store.List(Filter{Limit: 10})
	if len(page.Entries) != 1 || page.Entries[0].Message != "after" {
		t.Fatalf("level update entries = %#v", page.Entries)
	}
	if err := system.SetLevel("trace"); err == nil {
		t.Fatal("invalid runtime level must fail")
	}
}

// A config write that commits while an older writer applies its level wins:
// FollowLevel re-reads after applying and settles on the newest value.
func TestFollowLevelSettlesOnTheNewestSnapshot(t *testing.T) {
	system, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	current := func() string {
		reads++
		if reads == 1 {
			return "debug" // the older writer's snapshot
		}
		return "warn" // a newer write committed after that read
	}
	if err := system.FollowLevel(current); err != nil {
		t.Fatal(err)
	}
	if got := system.level.Level(); got != slog.LevelWarn {
		t.Fatalf("level = %v, want warn from the newest snapshot", got)
	}
	if err := system.FollowLevel(func() string { return "trace" }); err == nil {
		t.Fatal("an invalid level must fail")
	}
}

func TestStoreEvictionAndFilteredSubscription(t *testing.T) {
	store := NewStore(2)
	store.append(Entry{Time: time.Now(), Level: "info", Message: "one", Attributes: map[string]any{"component": "api"}})
	store.append(Entry{Time: time.Now(), Level: "warn", Message: "two", Attributes: map[string]any{"component": "jobs"}})
	store.append(Entry{Time: time.Now(), Level: "error", Message: "three", Attributes: map[string]any{"component": "jobs"}})
	page := store.List(Filter{Limit: 10})
	if page.NextCursor != 3 || len(page.Entries) != 2 || page.Entries[0].Message != "two" {
		t.Fatalf("evicted entries = %#v, cursor=%d", page.Entries, page.NextCursor)
	}
	page = store.List(Filter{After: 1, Limit: 1})
	if page.Truncated || page.OldestSequence != 2 || page.NextCursor != 2 || len(page.Entries) != 1 {
		t.Fatalf("cursor page = %+v", page)
	}
	gapStore := NewStore(2)
	for i := 0; i < 4; i++ {
		gapStore.append(Entry{Time: time.Now(), Level: "info", Message: "entry"})
	}
	if gap := gapStore.List(Filter{After: 1, Limit: 10}); !gap.Truncated || gap.OldestSequence != 3 {
		t.Fatalf("eviction gap = %+v", gap)
	}
	backlog, live, stop := store.Subscribe(Filter{After: 2, Levels: []string{"error"}, Limit: 10})
	defer stop()
	if len(backlog) != 1 || backlog[0].Message != "three" {
		t.Fatalf("backlog = %#v", backlog)
	}
	store.append(Entry{Time: time.Now(), Level: "info", Message: "ignored"})
	store.append(Entry{Time: time.Now(), Level: "error", Message: "live"})
	select {
	case entry := <-live:
		if entry.Message != "live" {
			t.Fatalf("live entry = %#v", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for filtered live entry")
	}
}

func TestStoreDisconnectsSlowSubscriberAndMarksDisabledHistory(t *testing.T) {
	store := NewStore(0)
	_, live, stop := store.Subscribe(Filter{})
	defer stop()
	for i := 0; i < 257; i++ {
		store.append(Entry{Time: time.Now(), Level: "info", Message: "entry"})
	}
	for i := 0; i < 256; i++ {
		if _, ok := <-live; !ok {
			t.Fatalf("subscriber closed after %d buffered records", i)
		}
	}
	if _, ok := <-live; ok {
		t.Fatal("slow subscriber must be closed after its buffer fills")
	}
	page := store.List(Filter{After: 1, Limit: 10})
	if !page.Truncated || page.OldestSequence != 0 || page.NextCursor != 257 {
		t.Fatalf("disabled history page = %+v", page)
	}
}

func TestFileOutput(t *testing.T) {
	cfg := testConfig()
	cfg.FileEnabled = true
	cfg.FilePath = filepath.Join(t.TempDir(), "nested", "amdl.log")
	cfg.Format = "json"
	system, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	system.Logger.InfoContext(context.Background(), "written", "component", "test", "payload", map[string]string{"token": "must-not-leak"})
	if err := system.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"msg":"written"`) {
		t.Fatalf("file output = %s", raw)
	}
	if strings.Contains(string(raw), "must-not-leak") || !strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("file output was not redacted: %s", raw)
	}
}

func TestFileOutputRedactsSensitiveGroup(t *testing.T) {
	cfg := testConfig()
	cfg.FileEnabled = true
	cfg.FilePath = filepath.Join(t.TempDir(), "amdl.log")
	cfg.Format = "json"
	system, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	system.Logger.Info("written", slog.Group("authorization", "value", "group-secret"))
	if err := system.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfg.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "group-secret") || !strings.Contains(string(raw), `"authorization":{"value":"[REDACTED]"}`) {
		t.Fatalf("sensitive group leaked to file output: %s", raw)
	}
}

func TestFileOutputFailsFastForUnwritableTarget(t *testing.T) {
	cfg := testConfig()
	cfg.FileEnabled = true
	cfg.FilePath = t.TempDir()
	if _, err := New(cfg); err == nil || !strings.Contains(err.Error(), "open log file") {
		t.Fatalf("New() error = %v, want log-file probe failure", err)
	}
}

func appendMessages(store *Store, messages ...string) {
	for _, message := range messages {
		store.append(Entry{Time: time.Now(), Level: "info", Message: message})
	}
}

func sequences(entries []Entry) []uint64 {
	out := []uint64{}
	for _, entry := range entries {
		out = append(out, entry.Sequence)
	}
	return out
}

// After the ring wraps, List and Subscribe still see the newest records in
// order, and cursors, truncation and paging behave as on an unwrapped ring.
func TestStoreRingWraparound(t *testing.T) {
	store := NewStore(3)
	appendMessages(store, "one", "two", "three", "four", "five", "six", "seven")

	page := store.List(Filter{})
	if got := sequences(page.Entries); !slices.Equal(got, []uint64{5, 6, 7}) || page.OldestSequence != 5 || page.NextCursor != 7 || page.Truncated {
		t.Fatalf("full page = %v oldest=%d next=%d truncated=%v, want [5 6 7] 5 7 false", got, page.OldestSequence, page.NextCursor, page.Truncated)
	}
	if page.Entries[0].Message != "five" || page.Entries[2].Message != "seven" {
		t.Fatalf("messages = %q..%q, want five..seven", page.Entries[0].Message, page.Entries[2].Message)
	}
	if page := store.List(Filter{Limit: 2}); !slices.Equal(sequences(page.Entries), []uint64{6, 7}) {
		t.Fatalf("tail page = %v, want [6 7]", sequences(page.Entries))
	}
	if page := store.List(Filter{After: 2, Limit: 10}); !page.Truncated || !slices.Equal(sequences(page.Entries), []uint64{5, 6, 7}) {
		t.Fatalf("cursor before the ring = %+v, want truncated [5 6 7]", page)
	}
	if page := store.List(Filter{After: 4, Limit: 10}); page.Truncated {
		t.Fatalf("cursor just before the oldest record must not be truncated: %+v", page)
	}
	page = store.List(Filter{After: 5, Limit: 1})
	if got := sequences(page.Entries); !slices.Equal(got, []uint64{6}) || page.NextCursor != 6 {
		t.Fatalf("cursor page = %v next=%d, want [6] next=6", got, page.NextCursor)
	}
	page = store.List(Filter{After: page.NextCursor, Limit: 1})
	if got := sequences(page.Entries); !slices.Equal(got, []uint64{7}) || page.NextCursor != 7 {
		t.Fatalf("last cursor page = %v next=%d, want [7] next=7", got, page.NextCursor)
	}
	if page := store.List(Filter{After: 7, Limit: 5}); len(page.Entries) != 0 || page.Entries == nil || page.NextCursor != 7 {
		t.Fatalf("caught-up page = %+v, want an empty non-nil page at 7", page)
	}

	backlog, _, stop := store.Subscribe(Filter{Limit: 2})
	stop()
	if got := sequences(backlog); !slices.Equal(got, []uint64{6, 7}) {
		t.Fatalf("subscribe tail backlog = %v, want [6 7]", got)
	}
	backlog, _, stop = store.Subscribe(Filter{After: 5, Limit: 1})
	stop()
	if got := sequences(backlog); !slices.Equal(got, []uint64{6, 7}) {
		t.Fatalf("subscribe cursor backlog = %v, want every match after 5: [6 7]", got)
	}
}

// Query matches the message and the attribute JSON case-insensitively, for
// records that wrapped around the ring as well as live ones.
func TestStoreQueryMatchesMessageAndAttributes(t *testing.T) {
	store := NewStore(2)
	store.append(Entry{Time: time.Now(), Level: "info", Message: "evicted", Attributes: map[string]any{"user": "Alice"}})
	store.append(Entry{Time: time.Now(), Level: "info", Message: "Login", Attributes: map[string]any{"user": "Alice"}})
	store.append(Entry{Time: time.Now(), Level: "info", Message: "logout", Attributes: map[string]any{"user": "bob"}})
	if page := store.List(Filter{Query: "ALICE"}); len(page.Entries) != 1 || page.Entries[0].Message != "Login" {
		t.Fatalf("attribute query = %#v, want only the retained Login record", page.Entries)
	}
	if page := store.List(Filter{Query: "log"}); len(page.Entries) != 2 {
		t.Fatalf("message query = %#v, want Login and logout", page.Entries)
	}
	_, live, stop := store.Subscribe(Filter{After: 3, Query: "Carol"})
	defer stop()
	store.append(Entry{Time: time.Now(), Level: "info", Message: "skip", Attributes: map[string]any{"user": "dave"}})
	store.append(Entry{Time: time.Now(), Level: "info", Message: "hit", Attributes: map[string]any{"user": "carol"}})
	select {
	case entry := <-live:
		if entry.Message != "hit" || entry.Sequence != 5 {
			t.Fatalf("live entry = %#v, want hit with sequence 5", entry)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the matching live entry")
	}
}
