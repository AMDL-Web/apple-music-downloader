package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/config"
)

// A wrapper-manager lists its accounts in /status instances; a bare
// wrapper-lite does not, and may name its one account.
func TestStatusInstances(t *testing.T) {
	for _, tt := range []struct {
		name   string
		body   map[string]any
		listed bool
		want   []Instance
		who    string
	}{
		{"manager", map[string]any{"regions": []string{"jp", "us"}, "clientCount": 3, "ready": true, "instances": []map[string]any{
			{"id": "a", "region": "jp", "name": "a@icloud.com", "ready": true},
			{"id": "b", "region": "", "name": "", "ready": false, "degraded": true},
			{"id": "", "region": "us"},
		}}, true, []Instance{{ID: "a", Region: "jp", Name: "a@icloud.com", Ready: true}, {ID: "b", Degraded: true}}, ""},
		{"manager without accounts", map[string]any{"regions": []string{}, "clientCount": 0, "instances": []any{}}, true, []Instance{}, ""},
		{"older manager", map[string]any{"regions": []string{"us"}, "clientCount": 1}, false, nil, ""},
		{"bare lite with a name", map[string]any{"regions": []string{"us"}, "name": "me@icloud.com"}, false, nil, "me@icloud.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeReply(w, 0, "SUCCESS", tt.body)
			}))
			st, err := c.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if st.Listed != tt.listed || !reflect.DeepEqual(st.Instances, tt.want) || st.Name != tt.who {
				t.Fatalf("status = %+v", st)
			}
			raw, _ := json.Marshal(st)
			if strings.Contains(string(raw), "instances") || strings.Contains(string(raw), "@") {
				t.Fatalf("the API status carries the accounts: %s", raw)
			}
		})
	}
}

// GET /token names the instance asked for; an unpaired client sends no
// Authorization header.
func TestTokenInstance(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/token":
			if got := r.URL.Query().Get("instance"); got != "jp-id" {
				t.Errorf("instance = %q", got)
			}
			writeReply(w, 0, "SUCCESS", map[string]any{"region": "jp", "music_token": musicTok})
		default:
			writeReply(w, 0, "SUCCESS", map[string]any{"regions": []string{"jp"}})
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := c.MusicToken(context.Background(), "jp-id"); err != nil || got.Region != "jp" {
		t.Fatalf("token = %+v, %v", got.Region, err)
	}
	if len(auths) != 2 || auths[0] != "" || auths[1] != "" {
		t.Fatalf("Authorization = %q", auths)
	}
}

// Logout by account id sends the instance id; a full manager refuses a login.
func TestLogoutAccountAndAccountLimit(t *testing.T) {
	var logoutBody map[string]string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			writeReply(w, 0, "SUCCESS", map[string]any{"regions": []string{}, "clientCount": 10})
		case "/logout":
			_ = json.NewDecoder(r.Body).Decode(&logoutBody)
			writeReply(w, 0, "SUCCESS", nil)
		case "/login":
			writeReply(w, -1, "account limit reached: this manager holds at most 10 accounts, log one out first", nil)
		}
	}))
	if err := c.LogoutAccount(context.Background(), "jp-id"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(logoutBody, map[string]string{"id": "jp-id"}) {
		t.Fatalf("logout body = %v", logoutBody)
	}
	if _, err := c.StartLogin(context.Background(), "x@icloud.com", "pw"); !errors.Is(err, ErrAccountLimit) {
		t.Fatalf("login error = %v, want ErrAccountLimit", err)
	}
}
