package mode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadSettings(dir)
	if err != nil || s != DefaultSettings() {
		t.Fatalf("first launch: %+v, %v", s, err)
	}
	s.Mode, s.RemoteURL = Remote, "http://192.168.3.5:18570"
	if err := SaveSettings(dir, s); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(dir)
	if err != nil || got != s {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestWrapperTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			fmt.Fprint(w, `{"code":0,"msg":"ok","data":{"regions":["cn","us"]}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	checks := TestWrapper(context.Background(), srv.URL)
	if len(checks) != 2 || !checks[0].OK || !checks[1].OK || !strings.Contains(checks[1].Text, "cn, us") || checks[1].Note != "wrapper-lite" {
		t.Fatalf("checks %v", checks)
	}
	// People often write a wrapper's address without a scheme.
	if bare := TestWrapper(context.Background(), strings.TrimPrefix(srv.URL, "http://")); len(bare) != 2 || !bare[0].OK {
		t.Errorf("address without a scheme: %v", bare)
	}
	if bad := TestWrapper(context.Background(), "http://127.0.0.1:1"); len(bad) != 1 || bad[0].OK {
		t.Errorf("unreachable wrapper: %v", bad)
	}
}
