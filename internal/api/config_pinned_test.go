package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"amdl/internal/config"
)

// Every ConfigResponse (GET, PUT, reload) lists the runtime keys an AMDL_*
// variable pins, and only those: a pinned startup key is not in config at all.
func TestConfigResponsesListPinnedRuntimeKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, config.Default())
	server := &Server{cfg: config.NewFileStore(config.Default(), path)}
	handler := server.Routes()

	pinnedOf := func(method, target, body string) []config.PinnedKey {
		t.Helper()
		recorder := requestJSON(t, handler, method, target, body)
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s: status = %d, body = %s", method, target, recorder.Code, recorder.Body.String())
		}
		var resp struct {
			Pinned *[]config.PinnedKey `json:"pinned"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.Pinned == nil {
			t.Fatalf("%s %s: pinned missing or null: %s", method, target, recorder.Body.String())
		}
		return *resp.Pinned
	}

	if got := pinnedOf(http.MethodGet, "/api/v1/config", ""); len(got) != 0 {
		t.Fatalf("nothing pinned: pinned = %v, want []", got)
	}

	t.Setenv("AMDL_DOWNLOAD_COVER_FORMAT", "png")
	t.Setenv("AMDL_WRAPPER_ADDRESS", "127.0.0.1:1")
	want := []config.PinnedKey{{Key: "download.cover_format", Env: "AMDL_DOWNLOAD_COVER_FORMAT"}}
	for _, call := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/v1/config", ""},
		{http.MethodPut, "/api/v1/config", `{"logging":{"level":"debug"}}`},
		{http.MethodPost, "/api/v1/config/reload", ""},
	} {
		if got := pinnedOf(call.method, call.target, call.body); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s %s: pinned = %v, want %v", call.method, call.target, got, want)
		}
	}
}
