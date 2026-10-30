package capability

import (
	"strings"
	"testing"
)

func TestEveryOperationIsExposed(t *testing.T) {
	ops, err := Operations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ops) < 20 {
		t.Fatalf("parsed only %d operations from openapi.yaml", len(ops))
	}
	used := map[string]bool{}
	for _, c := range All {
		for _, id := range c.Ops {
			if _, ok := ops[id]; !ok {
				t.Errorf("%s calls %s, which openapi.yaml does not have", c.ID(), id)
			}
			used[id] = true
		}
	}
	for id := range ops {
		if _, skipped := NotExposed[id]; !used[id] && !skipped {
			t.Errorf("operation %s has no capability; add one or list it in NotExposed with a reason", id)
		}
	}
}

func TestNamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range All {
		for _, name := range []string{c.Command, c.Tool} {
			if name == "" {
				continue
			}
			if seen[name] {
				t.Errorf("%q is used twice", name)
			}
			seen[name] = true
		}
		if c.Summary == "" {
			t.Errorf("%s has no summary", c.ID())
		}
		if c.Tool != "" && strings.ContainsAny(c.Tool, " -.") {
			t.Errorf("tool %q is not snake_case", c.Tool)
		}
	}
}

func TestPositionalArgsExist(t *testing.T) {
	// Arguments that a command assembles itself rather than passing through.
	assembled := map[string]bool{"urls": true, "keys": true, "changes": true}
	for _, c := range All {
		if len(c.Ops) == 0 {
			continue
		}
		op := Lookup(c.Ops[0])
		for _, a := range c.Args {
			if assembled[a] {
				continue
			}
			found := false
			for _, p := range op.Params {
				found = found || p.Name == a
			}
			for _, f := range op.BodyFields() {
				found = found || f.Name == a
			}
			if !found {
				t.Errorf("%s takes %q, which %s has neither as a parameter nor a body field", c.ID(), a, op.ID)
			}
		}
	}
}

func TestBuild(t *testing.T) {
	op := Lookup("listArtistAlbums")
	req, err := op.Build(map[string]any{"artist_id": "420368335", "limit": 50})
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != "GET" || req.Path != "/api/v1/artists/420368335/albums" || req.Query.Get("limit") != "50" {
		t.Errorf("request = %+v", req)
	}
	if _, err := op.Build(map[string]any{}); err == nil {
		t.Error("a missing path parameter should fail")
	}
	if _, err := op.Build(map[string]any{"artist_id": "1", "nope": "x"}); err == nil {
		t.Error("an unknown value should fail")
	}
	lookup := Lookup("lookupLink")
	req, err = lookup.Build(map[string]any{"url": "https://music.apple.com/us/album/x/1"})
	if err != nil || req.Body["url"] != "https://music.apple.com/us/album/x/1" || req.Method != "POST" {
		t.Errorf("lookup request = %+v, %v", req, err)
	}
	if out := Lookup("getDownload"); out.Output != "DownloadDetail" || out.OutputSchema["properties"] == nil {
		t.Errorf("getDownload output = %q", out.Output)
	}
	if !Lookup("streamLogs").Stream {
		t.Error("streamLogs should be a stream")
	}
}
