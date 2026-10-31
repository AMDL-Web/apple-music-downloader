package setup

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeServer answers the reads Diagnose makes with the given bodies.
func fakeServer(t *testing.T, system, wrapper, config string) string {
	t.Helper()
	mux := http.NewServeMux()
	answer := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			if body == "" {
				http.Error(w, `{"error":{"code":"unavailable","message":"down"}}`, http.StatusServiceUnavailable)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/v1/health", answer(`{"status":"ok"}`))
	mux.HandleFunc("/api/v1/system", answer(system))
	mux.HandleFunc("/api/v1/wrapper/status", answer(wrapper))
	mux.HandleFunc("/api/v1/config", answer(config))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func system(version string, ffmpeg bool, free int64) string {
	raw, _ := json.Marshal(map[string]any{
		"version": version,
		"tools":   []map[string]any{{"name": "ffmpeg", "available": ffmpeg, "path": "/usr/bin/ffmpeg"}},
		"storage": []map[string]any{{"name": "downloads_dir", "path": "/volume1/music", "free_bytes": free, "total_bytes": int64(8) << 40}},
	})
	return string(raw)
}

func TestDiagnoseAHealthyServer(t *testing.T) {
	url := fakeServer(t, system("2.0.0", true, 2<<40),
		`{"ready":true,"logged_in":true,"regions":["us","cn"],"accounts":["a@b.c"],"login_supported":true,"media_user_token":"...kg=="}`,
		`{"config":{"catalog":{"media_user_token":""}}}`)
	sv := Diagnose(context.Background(), url, "2.1.0")
	for name, l := range map[string]Line{"reach": sv.Reach, "account": sv.Account, "token": sv.Token, "ffmpeg": sv.FFmpeg, "folder": sv.Folder} {
		if l.State != Ready {
			t.Errorf("%s: %+v", name, l)
		}
	}
	if !strings.HasPrefix(sv.Reach.Text, "amdl 2.0.0 · ") || sv.Account.Text != "a@b.c · us, cn" ||
		sv.Token.Text != "From the wrapper · ends …kg==" || sv.FFmpeg.Text != "/usr/bin/ffmpeg" || sv.Folder.Text != "/volume1/music · 2.2 TB free" {
		t.Errorf("texts: %+v", sv)
	}
}

func TestDiagnoseNamesWhatTheServerLacks(t *testing.T) {
	url := fakeServer(t, system("1.9.2", false, 1<<30),
		`{"ready":true,"logged_in":false,"regions":[],"accounts":[],"login_supported":false,"media_user_token":""}`,
		`{"config":{"catalog":{"media_user_token":"...wxyz"}}}`)
	sv := Diagnose(context.Background(), url, "2.0.0")
	if sv.Reach.State != Failed || sv.Reach.Text != "amdl 1.9.2 · this app needs amdl 2" {
		t.Errorf("an older major version: %+v", sv.Reach)
	}
	if sv.Account.State != Missing || sv.FFmpeg.State != Failed || sv.Folder.State != Missing {
		t.Errorf("account %+v, ffmpeg %+v, folder %+v", sv.Account, sv.FFmpeg, sv.Folder)
	}
	if sv.Token.State != Ready || sv.Token.Text != "Saved · ends …wxyz" {
		t.Errorf("a saved token: %+v", sv.Token)
	}

	if sv := Diagnose(context.Background(), "http://127.0.0.1:1", "2.0.0"); sv.State != Failed || sv.Reach.State != Failed || sv.Account.State != Unchecked {
		t.Errorf("nothing listening: %+v", sv)
	}
}

func TestCompatible(t *testing.T) {
	for _, c := range []struct {
		theirs, mine string
		want         bool
	}{
		{"2.0.0", "2.3.1", true},
		{"v2.0.0-rc.1", "2.0.0", true},
		{"1.9.0", "2.0.0", false},
		{"dev", "2.0.0", true},
		{"2.0.0", "0.0.0-dev.b8e7ed3", true},
	} {
		if got := Compatible(c.theirs, c.mine); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v, want %v", c.theirs, c.mine, got, c.want)
		}
	}
}
