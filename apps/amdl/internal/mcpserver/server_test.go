package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"amdl/internal/capability"
	"amdl/internal/client"
)

type session struct {
	cs       *mcp.ClientSession
	mu       sync.Mutex
	progress []float64
}

func connect(t *testing.T, backend http.Handler) *session {
	t.Helper()
	srv := httptest.NewServer(backend)
	t.Cleanup(srv.Close)
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c.RetryMin, c.RetryMax = time.Millisecond, 5*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(c, "test").Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	s := &session{}
	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, r *mcp.ProgressNotificationClientRequest) {
			s.mu.Lock()
			s.progress = append(s.progress, r.Params.Progress)
			s.mu.Unlock()
		},
	})
	if s.cs, err = cl.Connect(ctx, ct, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.cs.Close() })
	return s
}

func (s *session) call(t *testing.T, name string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	p := &mcp.CallToolParams{Name: name, Arguments: args}
	p.SetProgressToken("p1")
	res, err := s.cs.CallTool(context.Background(), p)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("%s answered %q", name, text)
	}
	return out, res.IsError
}

func TestToolsComeFromTheCapabilityTable(t *testing.T) {
	s := connect(t, http.NotFoundHandler())
	res, err := s.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*mcp.Tool{}
	for _, tl := range res.Tools {
		got[tl.Name] = tl
	}
	for _, c := range capability.All {
		if c.Tool == "" {
			continue
		}
		tl, ok := got[c.Tool]
		if !ok {
			t.Errorf("tool %s is missing", c.Tool)
			continue
		}
		if tl.Annotations.ReadOnlyHint != (c.Safety == capability.Read) {
			t.Errorf("%s readOnlyHint = %v", c.Tool, tl.Annotations.ReadOnlyHint)
		}
	}
	if len(got) != 15 {
		t.Errorf("%d tools, want 15", len(got))
	}
	if d := got["delete_job"].Annotations.DestructiveHint; d == nil || !*d {
		t.Error("delete_job should be destructive")
	}
	schema, _ := json.Marshal(got["search_catalog"].InputSchema)
	for _, want := range []string{`"term"`, `"songs"`, `"required":["term"]`} {
		if !strings.Contains(string(schema), want) {
			t.Errorf("search_catalog schema lacks %s: %s", want, schema)
		}
	}
}

func TestSearchThenDownloadInTwoCalls(t *testing.T) {
	var submitted atomic.Value
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			if r.URL.Query().Get("types") != "albums" || r.URL.Query().Get("limit") != "1" {
				t.Errorf("search query = %s", r.URL.RawQuery)
			}
			fmt.Fprint(w, `{"term":"igor","storefront":"us","albums":{"items":[{"id":"1","type":"album","name":"IGOR","url":"https://music.apple.com/us/album/igor/1"}]}}`)
		case "/api/v1/downloads":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			submitted.Store(body)
			w.WriteHeader(http.StatusAccepted)
			fmt.Fprint(w, `{"accepted":1,"rejected":0,"results":[{"url":"https://music.apple.com/us/album/igor/1","status":"accepted",
				"job":{"id":"job_a","input":"x","type":"album","status":"queued","title":"IGOR","total_items":0,"completed_items":0,"skipped_items":0,"failed_items":0,"cancelled_items":0,"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:00:00Z"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	hits, isErr := s.call(t, "search_catalog", map[string]any{"term": "igor", "types": []string{"albums"}, "limit": 1})
	url := hits["albums"].(map[string]any)["items"].([]any)[0].(map[string]any)["url"].(string)
	if isErr || url == "" {
		t.Fatalf("search = %v", hits)
	}
	out, isErr := s.call(t, "start_download", map[string]any{"urls": []string{url}, "codec_priority": []string{"alac"}})
	acc := out["accepted"].([]any)
	if isErr || len(acc) != 1 || acc[0].(map[string]any)["job_id"] != "job_a" {
		t.Fatalf("start_download = %v", out)
	}
	body := submitted.Load().(map[string]any)
	if body["overrides"].(map[string]any)["quality_priority"].([]any)[0] != "alac" {
		t.Errorf("submitted = %v", body)
	}
}

func TestErrorsAreToolErrors(t *testing.T) {
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"job_not_found","message":"job job_x not found"}}`)
	}))
	out, isErr := s.call(t, "retry_job", map[string]any{"job_id": "job_x"})
	if !isErr || out["error"].(map[string]any)["code"] != "job_not_found" {
		t.Errorf("retry_job = %v, isError %v", out, isErr)
	}
	out, isErr = s.call(t, "lookup_link", map[string]any{})
	if !isErr || out["error"].(map[string]any)["code"] != "invalid_arguments" {
		t.Errorf("lookup_link without url = %v", out)
	}
}

const jobJSON = `{"id":"job_a","input":"x","type":"album","status":%q,"title":"IGOR","total_items":2,
	"completed_items":%d,"skipped_items":0,"failed_items":0,"cancelled_items":0,"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:00:00Z"}`

func itemJSON(id string, index int, status string) string {
	return fmt.Sprintf(`{"id":%q,"job_id":"job_a","adam_id":"1","kind":"song","index":%d,"has_lyrics":false,"status":%q,
		"progress":{"download":0,"decrypt":0,"resolved":false,"remuxed":false,"verified":false,"tagged":false,"saved":false},
		"created_at":"2026-10-30T10:00:00Z","updated_at":"2026-10-30T10:00:0%dZ"}`, id, index, status, index)
}

func TestWaitForJobReportsProgressAndTheEnd(t *testing.T) {
	progressEvery = 0
	t.Cleanup(func() { progressEvery = time.Second })
	var details atomic.Int32
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/downloads/job_a":
			if details.Add(1) == 1 {
				fmt.Fprintf(w, `{"job":`+jobJSON+`,"items":[%s,%s],"hooks":[],"event_cursor":5}`, "running", 0,
					itemJSON("i1", 1, "queued"), itemJSON("i2", 2, "queued"))
				return
			}
			fmt.Fprintf(w, `{"job":`+jobJSON+`,"items":[%s,%s],"hooks":[],"event_cursor":9}`, "completed", 2,
				itemJSON("i1", 1, "completed"), itemJSON("i2", 2, "completed"))
		case "/api/v1/downloads/job_a/events":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "id: 6\nevent: item_completed\ndata: {\"id\":6,\"job_id\":\"job_a\",\"item_id\":\"i1\",\"type\":\"item_completed\",\"payload\":%s,\"created_at\":\"2026-10-30T10:00:01Z\"}\n\n", itemJSON("i1", 1, "completed"))
			fmt.Fprintf(w, "id: 7\nevent: item_completed\ndata: {\"id\":7,\"job_id\":\"job_a\",\"item_id\":\"i2\",\"type\":\"item_completed\",\"payload\":%s,\"created_at\":\"2026-10-30T10:00:02Z\"}\n\n", itemJSON("i2", 2, "completed"))
		default:
			http.NotFound(w, r)
		}
	}))
	out, isErr := s.call(t, "wait_for_job", map[string]any{"job_id": "job_a", "timeout_s": 10})
	if isErr || out["done"] != true {
		t.Fatalf("wait_for_job = %v", out)
	}
	job := out["job"].(map[string]any)
	if job["status"] != "completed" || job["album_progress"].(map[string]any)["percent"].(float64) != 100 {
		t.Errorf("job = %v", job)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.progress) < 2 || s.progress[0] != 0 {
		t.Errorf("progress notifications = %v, want 0 then higher", s.progress)
	}
	for i := 1; i < len(s.progress); i++ {
		if s.progress[i] <= s.progress[i-1] {
			t.Errorf("progress went %v", s.progress)
		}
	}
}
