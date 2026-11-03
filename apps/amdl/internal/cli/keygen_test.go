package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// wrapper keygen prints a fresh 32-byte base64url key without asking a
// server; a gateway that refuses the key is exit 6 with a hint.
func TestWrapperKeygenAndARefusedKey(t *testing.T) {
	keys := map[string]bool{}
	for range 2 {
		r := run(t, nil, "", "wrapper", "keygen")
		var out struct {
			Key string `json:"key"`
		}
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil {
			t.Fatalf("keygen: %d %q %q", r.code, r.stdout, r.stderr)
		}
		raw, err := base64.RawURLEncoding.DecodeString(out.Key)
		if err != nil || len(raw) != 32 {
			t.Fatalf("key %q: %v", out.Key, err)
		}
		keys[out.Key] = true
	}
	if len(keys) != 2 {
		t.Fatal("two runs printed the same key")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"code":"wrapper_unauthorized","message":"wrapper /status: the wrapper refused the key (HTTP 401); check wrapper.token"}}`))
	}))
	defer srv.Close()
	r := run(t, srv, "", "wrapper", "status")
	if r.code != exitAuth || errorCode(t, r.stderr) != "wrapper_unauthorized" {
		t.Fatalf("refused key: %d %q", r.code, r.stderr)
	}
}
