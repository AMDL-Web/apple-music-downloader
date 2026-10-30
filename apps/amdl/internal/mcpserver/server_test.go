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

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"amdl/apps/amdl/skill"
	"amdl/internal/capability"
	"amdl/internal/client"
)

type session struct {
	cs       *mcp.ClientSession
	mu       sync.Mutex
	progress []float64
}

type progressTransport struct {
	mcp.Transport
	s *session
}

func (t *progressTransport) Connect(ctx context.Context) (mcp.Connection, error) {
	c, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &progressConnection{Connection: c, s: t.s}, nil
}

type progressConnection struct {
	mcp.Connection
	s *session
}

func (c *progressConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	if err != nil {
		return msg, err
	}
	// Record progress in wire order: the SDK dispatches notification handlers
	// on a separate queue that may still be pending when CallTool returns.
	if r, ok := msg.(*jsonrpc.Request); ok && r.Method == "notifications/progress" {
		var p mcp.ProgressNotificationParams
		if err := json.Unmarshal(r.Params, &p); err != nil {
			return nil, err
		}
		c.s.mu.Lock()
		c.s.progress = append(c.s.progress, p.Progress)
		c.s.mu.Unlock()
	}
	return msg, nil
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
	cl := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	if s.cs, err = cl.Connect(ctx, &progressTransport{Transport: ct, s: s}, nil); err != nil {
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
	if !isErr || out["error"].(map[string]any)["code"] != "invalid_request" {
		t.Errorf("lookup_link without url = %v", out)
	}
}

// batchBackend answers POST /downloads with status and body.
func batchBackend(status int, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/downloads" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
}

func errorOf(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	e, ok := out["error"].(map[string]any)
	if !ok {
		t.Fatalf("not an error result: %v", out)
	}
	return e
}

func TestStartDownloadWithNothingAcceptedIsAnError(t *testing.T) {
	s := connect(t, batchBackend(http.StatusUnprocessableEntity, `{"error":{"code":"no_urls_accepted","message":"no URL was accepted",
		"details":{"accepted":0,"rejected":1,"results":[{"url":"https://example.com/x","status":"invalid",
		"error":{"code":"invalid_link","message":"not an Apple Music link"}}]}}}`))
	out, isErr := s.call(t, "start_download", map[string]any{"urls": []string{"https://example.com/x"}})
	e := errorOf(t, out)
	if !isErr || e["code"] != "no_urls_accepted" {
		t.Fatalf("start_download = %v, isError %v", out, isErr)
	}
	rejected := e["details"].(map[string]any)["rejected"].([]any)
	if len(rejected) != 1 || rejected[0].(map[string]any)["code"] != "invalid_link" {
		t.Errorf("details = %v", e["details"])
	}
}

func TestStartDownloadOfRunningLinksIsNotAnError(t *testing.T) {
	s := connect(t, batchBackend(http.StatusUnprocessableEntity, `{"error":{"code":"no_urls_accepted","message":"no URL was accepted",
		"details":{"accepted":0,"rejected":1,"results":[{"url":"https://music.apple.com/us/album/igor/1","status":"duplicate_active",
		"existing_job_id":"job_a","error":{"code":"duplicate_active","message":"already running"}}]}}}`))
	out, isErr := s.call(t, "start_download", map[string]any{"urls": []string{"https://music.apple.com/us/album/igor/1"}})
	rejected, _ := out["rejected"].([]any)
	if isErr || len(rejected) != 1 || rejected[0].(map[string]any)["existing_job_id"] != "job_a" {
		t.Errorf("start_download = %v, isError %v", out, isErr)
	}
}

func TestBadArgumentsNameTheField(t *testing.T) {
	s := connect(t, http.NotFoundHandler())
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"start_download", map[string]any{"urls": "https://music.apple.com/us/album/igor/1"}, "urls must be an array of strings"},
		{"start_download", map[string]any{"urls": []any{1}}, "urls must be an array of strings"},
		{"start_download", map[string]any{"urls": []string{"x"}, "bogus": true}, `unknown argument "bogus"`},
		{"get_status", map[string]any{"verbose": true}, "takes no arguments"},
		{"list_jobs", map[string]any{"limit": "5"}, "limit must be a whole number"},
		{"list_jobs", map[string]any{"limit": 1.5}, "limit must be a whole number"},
		{"set_config", map[string]any{"changes": []string{"a"}}, "changes must be an object"},
		{"get_job", map[string]any{"job_id": 7}, "job_id must be a string"},
		{"start_download", map[string]any{"urls": []string{"x"}, "overwrite": "yes"}, "overwrite must be true or false"},
	} {
		out, isErr := s.call(t, tc.tool, tc.args)
		e := errorOf(t, out)
		if !isErr || e["code"] != "invalid_request" || !strings.Contains(e["message"].(string), tc.want) {
			t.Errorf("%s %v = %v, want invalid_request %q", tc.tool, tc.args, out, tc.want)
		}
	}
	out, isErr := s.call(t, "start_download", map[string]any{})
	if e := errorOf(t, out); !isErr || e["code"] != "invalid_request" || e["message"] != "urls is required" {
		t.Errorf("start_download without urls = %v", out)
	}
}

func TestGetConfigOfAnUnknownKeyIsConfigKeyNotFound(t *testing.T) {
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"config":{"logging":{"level":"info"}}}`)
	}))
	out, isErr := s.call(t, "get_config", map[string]any{"keys": []string{"logging.level"}})
	if isErr || out["logging.level"] != "info" {
		t.Errorf("get_config = %v", out)
	}
	out, isErr = s.call(t, "get_config", map[string]any{"keys": []string{"logging.nope"}})
	if e := errorOf(t, out); !isErr || e["code"] != "config_key_not_found" {
		t.Errorf("get_config of an unknown key = %v", out)
	}
}

func TestResultsAreTextOnlyAndSearchDropsTheArtworkPalette(t *testing.T) {
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"term":"igor","albums":{"items":[{"id":"1","name":"IGOR","adam_id":12345678901234567,
			"artwork_bg_color":"ffffff","artwork_text_color1":"000000","artwork_text_color4":"111111","audio_traits":["lossless"]}]}}`)
	}))
	for _, keep := range []bool{false, true} {
		args := map[string]any{"term": "igor"}
		if keep {
			args["artwork_colors"] = true
		}
		res, err := s.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_catalog", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.StructuredContent != nil || len(res.Content) != 1 {
			t.Errorf("result repeats itself: structured %v, %d content blocks", res.StructuredContent, len(res.Content))
		}
		text := res.Content[0].(*mcp.TextContent).Text
		hasPalette := strings.Contains(text, "artwork_bg_color") || strings.Contains(text, "artwork_text_color")
		if hasPalette != keep {
			t.Errorf("artwork_colors=%v: palette present = %v in %s", keep, hasPalette, text)
		}
		if !strings.Contains(text, `"audio_traits"`) || !strings.Contains(text, "12345678901234567") {
			t.Errorf("the rest of the hit changed: %s", text)
		}
	}
}

func TestListJobsDefaultsToFiftyRows(t *testing.T) {
	var limit atomic.Value
	s := connect(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit.Store(r.URL.Query().Get("limit"))
		fmt.Fprint(w, `{"downloads":[],"total":0,"offset":0,"limit":50}`)
	}))
	if out, isErr := s.call(t, "list_jobs", map[string]any{}); isErr {
		t.Fatalf("list_jobs = %v", out)
	}
	if limit.Load() != "50" {
		t.Errorf("limit = %v, want 50, the CLI's default", limit.Load())
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

func TestInstructionsKeepTheSkillsRules(t *testing.T) {
	for _, r := range rules {
		if !strings.Contains(skill.Text, r) {
			t.Errorf("apps/amdl/skill/SKILL.md lacks the rule %q", r)
		}
		if !strings.Contains(instructions, r) {
			t.Errorf("the instructions lack the rule %q", r)
		}
	}
}

func TestEveryToolAndParameterIsDescribed(t *testing.T) {
	for _, c := range capability.All {
		if c.Tool == "" {
			continue
		}
		tl := tool(c)
		if strings.TrimSpace(tl.Description) == "" {
			t.Errorf("%s has no description", c.Tool)
		}
		props := tl.InputSchema.(map[string]any)["properties"].(map[string]any)
		for name, p := range props {
			if d, _ := p.(map[string]any)["description"].(string); strings.TrimSpace(d) == "" {
				t.Errorf("%s: parameter %s has no description", c.Tool, name)
			}
		}
	}
}

func TestChangingToolsAreMarkedConservatively(t *testing.T) {
	for _, c := range capability.All {
		if c.Tool == "" {
			continue
		}
		ann := tool(c).Annotations
		if ann.ReadOnlyHint != (c.Safety == capability.Read) {
			t.Errorf("%s: readOnlyHint %v for safety %v", c.Tool, ann.ReadOnlyHint, c.Safety)
		}
		wantDestructive := c.Safety == capability.Destructive || c.Tool == "set_config" || c.Tool == "cancel_job"
		if c.Safety != capability.Read && (ann.DestructiveHint == nil || *ann.DestructiveHint != wantDestructive) {
			t.Errorf("%s: destructiveHint %v, want %v", c.Tool, ann.DestructiveHint, wantDestructive)
		}
	}
}
