package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"amdl/internal/config"
)

// A restart key saved through PUT lands in the file and in pending_restart,
// GET shows the saved value while the running one stays, and a start from
// the file (a new store) has nothing pending.
func TestPutRestartKeyIsSavedForTheNextStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfigFile(t, path, config.Default())
	server := newServer(t, Deps{Config: config.NewFileStore(config.Default(), path)})
	routes := server.Routes()

	recorder := requestJSON(t, routes, http.MethodPut, "/api/v1/config",
		`{"wrapper":{"address":"http://wrapper-manager:8080"},"catalog":{"default_storefront":"jp"}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", recorder.Code, recorder.Body.String())
	}
	var resp struct {
		Config struct {
			Wrapper struct {
				Address string `json:"address"`
			} `json:"wrapper"`
		} `json:"config"`
		PendingRestart []string `json:"pending_restart"`
	}
	decode := func() {
		t.Helper()
		resp.PendingRestart = nil
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
	}
	decode()
	if resp.Config.Wrapper.Address != "http://wrapper-manager:8080" || !reflect.DeepEqual(resp.PendingRestart, []string{"wrapper.address"}) {
		t.Fatalf("PUT response = %s", recorder.Body.String())
	}
	if got := server.cfg.Get(); got.Wrapper.Address != config.Default().Wrapper.Address || got.Catalog.DefaultStorefront != "jp" {
		t.Fatalf("running config = %q, %q; want the old wrapper and the new storefront", got.Wrapper.Address, got.Catalog.DefaultStorefront)
	}
	recorder = requestJSON(t, routes, http.MethodGet, "/api/v1/config", "")
	decode()
	if resp.Config.Wrapper.Address != "http://wrapper-manager:8080" || !reflect.DeepEqual(resp.PendingRestart, []string{"wrapper.address"}) {
		t.Fatalf("GET after PUT = %s", recorder.Body.String())
	}
	recorder = requestJSON(t, routes, http.MethodGet, "/api/v1/system", "")
	if !strings.Contains(recorder.Body.String(), `"pending_restart":["wrapper.address"]`) {
		t.Fatalf("GET /system = %s", recorder.Body.String())
	}

	next, err := config.Load(path)
	if err != nil || next.Wrapper.Address != "http://wrapper-manager:8080" {
		t.Fatalf("next start loads %q, %v", next.Wrapper.Address, err)
	}
	restarted := newServer(t, Deps{Config: config.NewFileStore(next, path)})
	recorder = requestJSON(t, restarted.Routes(), http.MethodGet, "/api/v1/config", "")
	decode()
	if len(resp.PendingRestart) != 0 || !strings.Contains(recorder.Body.String(), `"pending_restart":[]`) {
		t.Fatalf("after the restart = %s", recorder.Body.String())
	}
}

// PUT names the same pending keys GET does, a hand edit of a file-only key
// included, though PUT cannot change that key itself.
func TestPutReportsHandEditedFileOnlyKeysAsPending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	running := config.Default()
	writeConfigFile(t, path, running)
	routes := newServer(t, Deps{Config: config.NewFileStore(running, path)}).Routes()

	edited := running
	edited.Server.Listen = "127.0.0.1:19999"
	writeConfigFile(t, path, edited)

	pending := func(recorder *httptest.ResponseRecorder) []string {
		t.Helper()
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d %s", recorder.Code, recorder.Body.String())
		}
		var resp struct {
			PendingRestart []string `json:"pending_restart"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.PendingRestart
	}
	put := pending(requestJSON(t, routes, http.MethodPut, "/api/v1/config", `{"catalog":{"default_storefront":"jp"}}`))
	get := pending(requestJSON(t, routes, http.MethodGet, "/api/v1/config", ""))
	if want := []string{"server.listen"}; !reflect.DeepEqual(put, want) || !reflect.DeepEqual(get, want) {
		t.Fatalf("pending_restart: PUT %v, GET %v; want %v from both", put, get, want)
	}
}

func TestPutWrapperAddressIsRefusedWhileTheWrapperIsManaged(t *testing.T) {
	server := newServer(t, Deps{WrapperManaged: true})
	recorder := requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config", `{"wrapper":{"address":"http://elsewhere:12340"}}`)
	assertErrorCode(t, recorder, http.StatusUnprocessableEntity, "config_key_not_mutable")
	if body := recorder.Body.String(); !strings.Contains(body, `"reason":"managed_wrapper"`) || !strings.Contains(body, `"keys":["wrapper.address"]`) {
		t.Fatalf("body = %s", body)
	}
	// Other wrapper keys, and writing the address back unchanged, are fine.
	recorder = requestJSON(t, server.Routes(), http.MethodPut, "/api/v1/config",
		`{"wrapper":{"address":"http://127.0.0.1:12340","timeout_seconds":60}}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRestartSystem(t *testing.T) {
	asked := 0
	server := newServer(t, Deps{Restart: func() { asked++ }})
	recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/system/restart", "")
	if recorder.Code != http.StatusAccepted || !strings.Contains(recorder.Body.String(), `"restarting":true`) || asked != 1 {
		t.Fatalf("POST /system/restart = %d %s, asked %d", recorder.Code, recorder.Body.String(), asked)
	}
	server = newServer(t, Deps{})
	recorder = requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/system/restart", "")
	assertErrorCode(t, recorder, http.StatusServiceUnavailable, "feature_unavailable")
}

// RuntimeConfig marks exactly the restart keys with x-applies: restart, so a
// client can tell which saved values wait for a restart before it saves.
func TestRuntimeConfigMarksRestartKeys(t *testing.T) {
	type property struct {
		Applies    string              `yaml:"x-applies"`
		Properties map[string]property `yaml:"properties"`
	}
	var spec struct {
		Components struct {
			Schemas map[string]property `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	marked := 0
	for section, body := range spec.Components.Schemas["RuntimeConfig"].Properties {
		for leaf, prop := range body.Properties {
			key := section + "." + leaf
			if want := config.AppliesOnRestart(key); (prop.Applies == "restart") != want {
				t.Errorf("%s: x-applies %q, restart key %v", key, prop.Applies, want)
			}
			if prop.Applies == "restart" {
				marked++
			}
		}
	}
	if marked != 10 {
		t.Fatalf("%d keys marked x-applies: restart, want 10", marked)
	}
}
