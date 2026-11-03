package wrapperhost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"amdl/internal/wrapper"
)

func managerHome(t *testing.T, handler http.HandlerFunc) Home {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	_, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	h := Home{Dir: t.TempDir()}
	if err := writeJSON(h.path("installed.json"), Installed{Kind: ManagerGuest, ID: "manager", Port: port}); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestManagerDiscoversAccountsAndSelectsTokenByID(t *testing.T) {
	h := managerHome(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			fmt.Fprint(w, `{"code":0,"data":{"instances":[{"id":"b","region":"us","name":"second@example.com","ready":false,"degraded":true},{"id":"a","region":"cn","name":"first@example.com","ready":true,"degraded":false}]}}`)
		case "/token":
			if r.URL.Query().Get("instance") != "a" {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"code":503}`)
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"region":"cn","music_token":"synthetic-token","refreshed_at":123,"next_refresh_at":456}}`)
		}
	})
	instances, err := h.Instances(context.Background())
	if err != nil || len(instances) != 2 || instances[0].ID != "a" || !instances[1].Degraded {
		t.Fatalf("instances: %+v, %v", instances, err)
	}
	reply, err := h.InstanceToken(context.Background(), instances[0].ID)
	if err != nil || reply.Region != "cn" || reply.RefreshedAt != 123 || reply.NextRefreshAt != 456 || reply.MusicToken == "" {
		t.Fatal("manager token reply was not decoded")
	}
	if _, err := h.InstanceToken(context.Background(), "unknown"); !errors.Is(err, wrapper.ErrNoToken) {
		t.Fatalf("unknown instance: %v", err)
	}
}

func TestManagerErrorsDoNotQuoteSecrets(t *testing.T) {
	for _, reply := range []string{`{"code":0,"data":{"music_token":"synthetic-secret","region":123}}`, `{"code":0,"data":{"music_token":"synthetic-secret"`, `{"code":-1,"msg":"synthetic-secret"}`} {
		h := managerHome(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, reply) })
		if _, err := h.InstanceToken(context.Background(), "a"); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("malformed token reply was accepted or exposed its contents")
		}
	}
}

func TestManagerTokenHonorsCanceledContext(t *testing.T) {
	h := managerHome(t, func(w http.ResponseWriter, r *http.Request) { t.Error("canceled request reached manager") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := h.InstanceToken(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
}
