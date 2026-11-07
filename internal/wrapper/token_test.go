package wrapper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

var musicTok = strings.Repeat("M", 244)

func TestMusicTokenReply(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/token" || r.URL.RawQuery != "" {
			t.Errorf("request %s %s?%s, want GET /token", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		writeReply(w, 0, "SUCCESS", map[string]any{
			"region": "cn", "music_token": musicTok, "refreshed_at": 1793566788, "next_refresh_at": 1793568588,
		})
	}))
	got, err := c.MusicToken(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	want := TokenReply{Region: "cn", MusicToken: musicTok, RefreshedAt: 1793566788, NextRefreshAt: 1793568588}
	if got != want {
		t.Fatalf("reply %+v, want the documented fields", TokenReply{Region: got.Region, RefreshedAt: got.RefreshedAt, NextRefreshAt: got.NextRefreshAt})
	}
}

func TestMusicTokenErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    error
	}{
		{"logged out", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":503,"msg":"music token not available"}`))
		}, ErrNoToken},
		{"upstream lite has no route", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}, ErrTokenUnsupported},
		{"a manager's 404 envelope", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":404,"msg":"not found"}`))
		}, ErrTokenUnsupported},
		{"a proxy's 503 is not a logout", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream down", http.StatusServiceUnavailable)
		}, ErrWrapperResponse},
		{"envelope error", func(w http.ResponseWriter, r *http.Request) {
			writeReply(w, 500, "boom", nil)
		}, ErrWrapperResponse},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.handler)
			_, err := c.MusicToken(context.Background(), "")
			if !errors.Is(err, tt.want) {
				t.Fatalf("error %v, want %v", err, tt.want)
			}
			if errors.Is(tt.want, ErrWrapperResponse) && (errors.Is(err, ErrNoToken) || errors.Is(err, ErrTokenUnsupported)) {
				t.Fatalf("error %v classified as a logout or a missing route", err)
			}
		})
	}
}

// A reply that is not the documented shape can still hold the token; the
// error must not repeat it.
func TestMusicTokenErrorsNeverQuoteTheBody(t *testing.T) {
	bodies := map[string]string{
		"truncated JSON":   `{"code":0,"msg":"SUCCESS","data":{"music_token":"` + musicTok,
		"plain text":       musicTok,
		"wrong data shape": `{"code":0,"msg":"SUCCESS","data":{"music_token":["` + musicTok + `"]}}`,
		"data not object":  `{"code":0,"msg":"SUCCESS","data":"` + musicTok + `"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			_, err := c.MusicToken(context.Background(), "")
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(err.Error(), musicTok[:32]) {
				t.Fatalf("error quotes the token: %v", err)
			}
		})
	}
}

func TestMusicTokenTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.timeout = 50 * time.Millisecond
	_, err = c.MusicToken(context.Background(), "")
	var timeout *timeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("error %v, want a timeout", err)
	}
	if timeout.after != c.timeout {
		t.Fatalf("timed out after %v, want the request timeout %v", timeout.after, c.timeout)
	}
}
