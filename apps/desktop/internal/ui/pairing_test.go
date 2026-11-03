package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"amdl/apps/desktop/internal/store"
	"amdl/internal/client"
)

func TestConnectShowsOnlyWhileThereIsNoPairing(t *testing.T) {
	exp := time.Now().Add(9 * time.Minute)
	for _, tc := range []struct {
		name    string
		p       client.WrapperPairing
		managed bool
		want    []string
		not     []string
	}{
		{name: "unpaired", p: client.WrapperPairing{State: client.PairingUnpaired}, want: []string{"Not paired", "Connect"}, not: []string{"Disconnect…"}},
		{name: "needs pairing", p: client.WrapperPairing{State: client.PairingNeeded, LastError: "the gateway refused the token"},
			want: []string{"Needs connecting", "the gateway refused the token", "Connect"}},
		{name: "pending", p: client.WrapperPairing{State: client.PairingPending, UserCode: "WDJB-MJHT", VerificationURL: "https://wrapper.example.com/pair", ExpiresAt: &exp},
			want: []string{"Waiting for approval", "WDJB-MJHT", "https://wrapper.example.com/pair", "Expires in"}, not: []string{"Connect"}},
		{name: "paired", p: client.WrapperPairing{State: client.PairingPaired}, want: []string{"Connected", "Disconnect…"}, not: []string{"Connect"}},
		{name: "this computer's guest", p: client.WrapperPairing{State: client.PairingUnpaired}, managed: true, not: []string{"Connect", "Not paired"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newTestApp(remoteSettings())
			a.GoSettings("connection")
			sv := &a.Store.Server
			sv.Read = time.Now()
			sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true}
			if tc.managed {
				sv.Wrapper.Managed = &client.ManagedWrapper{Mode: "serving"}
			}
			p := tc.p
			sv.Pairing = &p
			tt := ui.NewTester(a.View, 1280, 800)
			for _, w := range tc.want {
				if !tt.HasText(w) {
					t.Errorf("no %q: %q", w, tt.Texts())
				}
			}
			for _, w := range tc.not {
				if _, ok := tt.Find(w); ok {
					t.Errorf("%q shown: %q", w, tt.Texts())
				}
			}
		})
	}
}

// fakePairing is a backend with a wrapper gateway: a start answers pending,
// the reads after it say pending until approve, then paired.
type fakePairing struct {
	mu          sync.Mutex
	state       client.WrapperPairing
	unsupported bool
	approve     int
	reads       int
}

func (f *fakePairing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply := func(code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	switch r.Method + " " + r.URL.Path {
	case "POST /api/v1/wrapper/pairing":
		if f.unsupported {
			reply(http.StatusNotImplemented, map[string]any{"error": map[string]string{"code": "pairing_unsupported", "message": "the wrapper takes requests without a token"}})
			return
		}
		exp := time.Now().Add(10 * time.Minute)
		f.state = client.WrapperPairing{State: client.PairingPending, UserCode: "WDJB-MJHT", VerificationURL: "https://wrapper.example.com/pair", ExpiresAt: &exp}
		reply(http.StatusAccepted, f.state)
	case "GET /api/v1/wrapper/pairing":
		if f.state.State == client.PairingPending {
			if f.reads++; f.reads >= f.approve {
				now := time.Now()
				f.state = client.WrapperPairing{State: client.PairingPaired, PairedAt: &now}
			}
		}
		reply(http.StatusOK, f.state)
	case "DELETE /api/v1/wrapper/pairing":
		f.state = client.WrapperPairing{State: client.PairingUnpaired}
		reply(http.StatusOK, f.state)
	default:
		http.NotFound(w, r)
	}
}

func pairingApp(t *testing.T, fake *fakePairing) (*App, *ui.Tester, func(func() bool)) {
	t.Helper()
	poll := store.PairingPoll
	store.PairingPoll = 20 * time.Millisecond
	t.Cleanup(func() { store.PairingPoll = poll })
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	a, _ := newTestApp(remoteSettings())
	queue := make(chan func(), 256)
	a.Store = store.New(func(fn func()) { queue <- fn }, nil)
	c, err := client.New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	a.Store.Connect(c)
	t.Cleanup(a.Store.Close)
	run := func(until func() bool) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for !until() {
			select {
			case fn := <-queue:
				fn()
			case <-deadline:
				t.Fatal("the fake backend's answer never arrived")
			}
		}
	}
	a.GoSettings("connection")
	sv := &a.Store.Server
	sv.Read = time.Now()
	sv.Wrapper = &client.WrapperStatus{Ready: true, LoggedIn: true}
	sv.Pairing = &client.WrapperPairing{State: client.PairingUnpaired}
	return a, liveTester(a), run
}

func TestConnectShowsTheCodeThenConnected(t *testing.T) {
	fake := &fakePairing{approve: 3}
	a, tt, run := pairingApp(t, fake)
	sv := &a.Store.Server
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return sv.Pairing.State == client.PairingPending })
	tt.Frame()
	for _, w := range []string{"WDJB-MJHT", "https://wrapper.example.com/pair", "Expires in"} {
		if !tt.HasText(w) {
			t.Errorf("pending: no %q in %q", w, tt.Texts())
		}
	}
	if _, ok := tt.Find("Connect"); ok {
		t.Errorf("pending still offers Connect: %q", tt.Texts())
	}
	run(func() bool { return sv.Pairing.State == client.PairingPaired })
	tt.Frame()
	if !tt.HasText("Connected") || !tt.HasText("Disconnect…") || tt.HasText("WDJB-MJHT") {
		t.Errorf("paired: %q", tt.Texts())
	}
	if err := tt.Click("Disconnect…"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Disconnect"); err != nil {
		t.Fatalf("%v in %q", err, tt.Texts())
	}
	run(func() bool { return sv.Pairing.State == client.PairingUnpaired })
}

func TestConnectHidesWhereTheWrapperNeedsNoPairing(t *testing.T) {
	a, tt, run := pairingApp(t, &fakePairing{unsupported: true})
	if err := tt.Click("Connect"); err != nil {
		t.Fatal(err)
	}
	run(func() bool { return a.Store.Pair.Unsupported })
	tt.Frame()
	if _, ok := tt.Find("Connect"); ok || tt.HasText("Not paired") || a.Store.Pair.Err != "" {
		t.Errorf("an unsupported pairing is still offered: %q", tt.Texts())
	}
}

func TestExpiresInCountsDown(t *testing.T) {
	for d, want := range map[time.Duration]string{9*time.Minute + 41*time.Second: "Expires in 9:41", 5 * time.Second: "Expires in 0:05", 0: "Expired"} {
		if got := expiresIn(d); got != want {
			t.Errorf("%v: %q; want %q", d, got, want)
		}
	}
}
