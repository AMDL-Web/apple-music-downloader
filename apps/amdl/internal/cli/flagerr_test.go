package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlagParseErrorsAreReportedOnce(t *testing.T) {
	r := run(t, nil, "", "jobs", "wait", "job_a", "--timeout", "30")
	var e struct {
		Error struct{ Code, Message, Hint string }
	}
	if r.code != exitInput || json.Unmarshal([]byte(r.stderr), &e) != nil || e.Error.Code != "usage_error" {
		t.Fatalf("stderr must hold one error object: %+v", r)
	}
	if !strings.Contains(e.Error.Hint, "30s") || !strings.Contains(e.Error.Hint, "2h") {
		t.Errorf("hint = %q, want unit examples", e.Error.Hint)
	}
	r = run(t, nil, "", "jobs", "list", "--no-such-flag")
	if r.code != exitInput || errorCode(t, r.stderr) != "usage_error" {
		t.Errorf("an unknown flag: %+v", r)
	}
	r = run(t, nil, "", "jobs", "list", "--limit", "x")
	if errorCode(t, r.stderr) != "usage_error" || strings.Contains(r.stderr, "unit") {
		t.Errorf("a bad integer must not get the unit hint: %+v", r)
	}
}
