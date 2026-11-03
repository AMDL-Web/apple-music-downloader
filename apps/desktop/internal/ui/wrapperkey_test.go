package ui

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/internal/client"
)

// Connection has the wrapper's key: empty, Generate fills it with a strong
// random key and copies it; set, it shows only that it is set. The guest
// this computer runs has no key.
func TestWrapperKeyField(t *testing.T) {
	setup := func(token string, managed bool) (*App, *fakeActions, *ui.Tester) {
		a, act := newTestApp(remoteSettings())
		a.GoSettings("connection")
		sv := &a.Store.Server
		sv.Read = time.Now()
		sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true, TokenSet: token != ""}
		if managed {
			sv.Wrapper.Managed = &client.ManagedWrapper{Mode: "serving"}
		}
		a.Store.Config.Values = map[string]any{"wrapper": map[string]any{"address": "https://wrapper.example.com", "token": token}}
		return a, act, ui.NewTester(a.View, 1280, 800)
	}

	a, act, tt := setup("", false)
	for _, w := range []string{"WRAPPER KEY", "Key", "Generate", "Save key"} {
		if !tt.HasText(w) {
			t.Fatalf("no %q: %q", w, tt.Texts())
		}
	}
	if err := tt.Click("Generate"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	key := a.set.edit.text["wrapper.token"]
	raw, err := base64.RawURLEncoding.DecodeString(key)
	if err != nil || len(raw) != 32 {
		t.Fatalf("generated %d characters (%v), want 32 bytes base64url", len(key), err)
	}
	if act.copied != key {
		t.Fatal("the generated key was not copied")
	}

	_, _, tt = setup("...", false)
	if !tt.HasText("Set") || !tt.HasText("Replace") {
		t.Fatalf("a set key: %q", tt.Texts())
	}
	if _, ok := tt.Find("Generate"); ok {
		t.Fatalf("Generate shown beside a set key: %q", tt.Texts())
	}

	_, _, tt = setup("", true)
	if tt.HasText("WRAPPER KEY") {
		t.Fatalf("the guest this computer runs shows a key: %q", tt.Texts())
	}
}
