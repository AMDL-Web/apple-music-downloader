package hooks

import (
	"strings"
	"testing"
)

func TestHookEnvDropsBackendVariablesAndAddsHookVariables(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/home/amdl",
		"AMDL_CATALOG_MEDIA_USER_TOKEN=secret",
		"AMDL_CONFIG=/app/configs/config.yaml",
		"AMDL_DONE_ITEMS=stale",
		"NOT_AMDL_X=kept",
	}
	payload := Payload{Event: "job_finished", Job: JobPayload{
		ID: "job_1", Type: "album", Status: "completed", TotalItems: 5,
		CompletedItems: 2, SkippedItems: 1, FailedItems: 1, CancelledItems: 1,
	}, Items: []ItemPayload{{ID: "a", OutputPath: "/music/a.m4a"}, {ID: "b"}, {ID: "c", OutputPath: "/music/c.m4a"}}}

	got := map[string]string{}
	for _, kv := range hookEnv(environ, payload) {
		name, value, _ := strings.Cut(kv, "=")
		if _, dup := got[name]; dup {
			t.Fatalf("variable %s set twice", name)
		}
		got[name] = value
	}
	for _, name := range []string{"AMDL_CATALOG_MEDIA_USER_TOKEN", "AMDL_CONFIG", "AMDL_DONE_ITEMS"} {
		if value, ok := got[name]; ok {
			t.Errorf("%s=%q reached the hook", name, value)
		}
	}
	want := map[string]string{
		"PATH": "/usr/bin:/bin", "HOME": "/home/amdl", "NOT_AMDL_X": "kept",
		"AMDL_EVENT": "job_finished", "AMDL_JOB_ID": "job_1", "AMDL_JOB_TYPE": "album", "AMDL_JOB_STATUS": "completed",
		"AMDL_TOTAL_ITEMS": "5", "AMDL_COMPLETED_ITEMS": "2", "AMDL_SKIPPED_ITEMS": "1",
		"AMDL_FAILED_ITEMS": "1", "AMDL_CANCELLED_ITEMS": "1",
		"AMDL_OUTPUT_PATHS": "/music/a.m4a\n/music/c.m4a",
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
}

func TestTailWriterKeepsTheEnd(t *testing.T) {
	tests := []struct {
		name   string
		writes []string
		limit  int
		want   string
	}{
		{name: "short output is whole", writes: []string{"line one\n", "line two\n"}, limit: 64, want: "line one\nline two"},
		{name: "one oversized write keeps its end", writes: []string{strings.Repeat("x", 20) + "END"}, limit: 8, want: "…xxxxxEND"},
		{name: "many writes keep the last bytes", writes: []string{"aaaa", "bbbb", "cccc"}, limit: 6, want: "…bbcccc"},
		{name: "a cut never splits a character", writes: []string{"ab", "é", "cd"}, limit: 3, want: "…cd"},
		{name: "exactly the limit is not truncated", writes: []string{"abcd"}, limit: 4, want: "abcd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &tailWriter{limit: tt.limit}
			for _, s := range tt.writes {
				if n, err := w.Write([]byte(s)); n != len(s) || err != nil {
					t.Fatalf("Write(%q) = %d, %v", s, n, err)
				}
			}
			if got := w.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
