package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// pairingServer answers the pairing routes: POST starts a pairing whose code
// expires in ttl, and GET answers pending for polls-1 reads, then final.
func pairingServer(t *testing.T, ttl time.Duration, polls int, final string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	reads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		pending := `{"state":"pending","user_code":"ABCD-EFGH","verification_url":"https://gw.example/pair","expires_at":"` +
			time.Now().Add(ttl).UTC().Format(time.RFC3339Nano) + `"}`
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(pending))
		case http.MethodDelete:
			_, _ = w.Write([]byte(`{"state":"unpaired"}`))
		default:
			reads++
			if reads < polls {
				_, _ = w.Write([]byte(pending))
				return
			}
			_, _ = w.Write([]byte(final))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func fastPairing(t *testing.T) {
	t.Helper()
	old := pairPollEvery
	pairPollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pairPollEvery = old })
}

func TestWrapperPairPrintsTheCodeThenWaitsUntilPaired(t *testing.T) {
	fastPairing(t)
	srv, seen := pairingServer(t, time.Minute, 3, `{"state":"paired","paired_at":"2026-11-03T09:00:00Z"}`)
	r := run(t, srv, "", "wrapper", "pair")
	lines := strings.Split(strings.TrimSpace(r.stdout), "\n")
	if r.code != 0 || len(lines) != 2 {
		t.Fatalf("got %+v", r)
	}
	var first, last struct {
		State    string `json:"state"`
		UserCode string `json:"user_code"`
		URL      string `json:"verification_url"`
	}
	if json.Unmarshal([]byte(lines[0]), &first) != nil || first.State != "pending" || first.UserCode != "ABCD-EFGH" || first.URL != "https://gw.example/pair" {
		t.Errorf("first line = %q", lines[0])
	}
	if json.Unmarshal([]byte(lines[1]), &last) != nil || last.State != "paired" {
		t.Errorf("last line = %q", lines[1])
	}
	want := "POST /api/v1/wrapper/pairing|GET /api/v1/wrapper/pairing|GET /api/v1/wrapper/pairing|GET /api/v1/wrapper/pairing"
	if got := strings.Join(seen(), "|"); got != want {
		t.Errorf("requests = %s", got)
	}
}

func TestWrapperPairExitsNonZeroWhenTheWrapperDoesNotApprove(t *testing.T) {
	fastPairing(t)
	srv, _ := pairingServer(t, time.Minute, 2, `{"state":"unpaired","last_error":"the owner refused the pairing"}`)
	r := run(t, srv, "", "wrapper", "pair")
	if r.code != exitAuth || errorCode(t, r.stderr) != "pairing_failed" || !strings.Contains(r.stderr, "the owner refused the pairing") {
		t.Errorf("got %+v", r)
	}
}

func TestWrapperPairStopsWhenTheCodeExpiresUnanswered(t *testing.T) {
	fastPairing(t)
	old := pairGrace
	pairGrace = 50 * time.Millisecond
	t.Cleanup(func() { pairGrace = old })
	srv, _ := pairingServer(t, 100*time.Millisecond, 1<<30, "")
	start := time.Now()
	r := run(t, srv, "", "wrapper", "pair")
	if r.code != exitWaitTimeout || errorCode(t, r.stderr) != "wait_timeout" {
		t.Errorf("got %+v", r)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("waited %v past an expired code", time.Since(start))
	}
}

func TestWrapperPairWithoutPairingSupportSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_, _ = w.Write([]byte(`{"error":{"code":"pairing_unsupported","message":"this wrapper does not pair"}}`))
	}))
	t.Cleanup(srv.Close)
	r := run(t, srv, "", "wrapper", "pair")
	if r.code != exitAuth || errorCode(t, r.stderr) != "pairing_unsupported" || !strings.Contains(r.stderr, "nothing to pair") {
		t.Errorf("got %+v", r)
	}
}

func TestWrapperPairStatusOnlyReadsAndUnpairDeletes(t *testing.T) {
	srv, seen := pairingServer(t, time.Minute, 1, `{"state":"needs_pairing","last_error":"the gateway refused the token"}`)
	r := run(t, srv, "", "wrapper", "pair", "--status")
	var got map[string]any
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &got) != nil || got["state"] != "needs_pairing" {
		t.Fatalf("status: %+v", r)
	}
	r = run(t, srv, "", "wrapper", "unpair", "--yes")
	if r.code != 0 || !strings.Contains(r.stdout, `"unpaired"`) {
		t.Fatalf("unpair: %+v", r)
	}
	if got := strings.Join(seen(), "|"); got != "GET /api/v1/wrapper/pairing|DELETE /api/v1/wrapper/pairing" {
		t.Errorf("requests = %s", got)
	}
	if r := run(t, srv, "", "wrapper", "unpair"); r.code != exitConfirm {
		t.Errorf("unpair without --yes: %+v", r)
	}
}

func TestWrapperPairDryRunSendsNothing(t *testing.T) {
	srv, seen := pairingServer(t, time.Minute, 1, `{"state":"paired"}`)
	r := run(t, srv, "", "wrapper", "pair", "--dry-run")
	if r.code != 0 || !strings.Contains(r.stdout, `"method":"POST"`) {
		t.Errorf("got %+v", r)
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("a dry run sent %q", got)
	}
}

func TestWrapperPairStopsWaitingOnInterrupt(t *testing.T) {
	fastPairing(t)
	srv, _ := pairingServer(t, time.Minute, 1<<30, "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	var out, errb bytes.Buffer
	code := Run(ctx, []string{"wrapper", "pair", "--server", srv.URL}, IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
		Getenv: func(string) string { return "" }})
	if code != exitInterrupted {
		t.Errorf("exit %d, stderr %q", code, errb.String())
	}
}
