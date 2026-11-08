package wrapper

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"amdl/internal/config"
	"amdl/internal/limits"
)

// writeReply writes a wrapper-lite envelope with HTTP 200, as wrapper-lite
// answers every handled request.
func writeReply(w http.ResponseWriter, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json")
	body := map[string]any{"code": code, "msg": msg}
	if data != nil {
		body["data"] = data
	}
	_ = json.NewEncoder(w).Encode(body)
}

// newTestClient is a client of a wrapper served by handler on a real local
// listener. Key retries do not wait.
func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(config.WrapperConfig{Address: srv.URL, TimeoutSeconds: 5}, WithTemplateParser(parseXOR))
	if err != nil {
		t.Fatal(err)
	}
	c.retryDelays = make([]time.Duration, len(keyRetryDelays))
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// xorTemplate stands in for a FairPlay template: it XORs every byte of the
// 16-byte-aligned prefix with its value, so a test can build ciphertext from
// known plaintext and tell which template decrypted it.
type xorTemplate byte

func (x xorTemplate) DecryptSample(sample []byte) {
	for i := range len(sample) / 16 * 16 {
		sample[i] ^= byte(x)
	}
}

// parseXOR reads {"xor": n}; a reply without it is a template wrapper-lite
// failed to capture.
func parseXOR(data []byte) (SampleDecrypter, error) {
	var body struct {
		XOR *int `json:"xor"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	if body.XOR == nil {
		return nil, errors.New("reply has no template fields")
	}
	return xorTemplate(*body.XOR), nil
}

func encrypt(plain []byte, x byte) []byte {
	out := append([]byte(nil), plain...)
	xorTemplate(x).DecryptSample(out)
	return out
}

// counts is a set of counters a test waits on without polling: every change
// wakes the waiters, which check their condition again.
type counts struct {
	mu      sync.Mutex
	n       map[any]int
	peak    map[any]int
	changed chan struct{}
}

func newCounts() *counts {
	return &counts{n: map[any]int{}, peak: map[any]int{}, changed: make(chan struct{})}
}

// add moves k by delta and returns its new value.
func (c *counts) add(k any, delta int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[k] += delta
	c.peak[k] = max(c.peak[k], c.n[k])
	close(c.changed)
	c.changed = make(chan struct{})
	return c.n[k]
}

// set sets k to v.
func (c *counts) set(k any, v int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[k] = v
	c.peak[k] = max(c.peak[k], v)
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *counts) max(k any) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.peak[k]
}

// await returns once k is at least want. The 5 s limit only turns a hang
// into a failure; nothing waits for time to pass.
func (c *counts) await(t *testing.T, k any, want int) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		c.mu.Lock()
		got, changed := c.n[k], c.changed
		c.mu.Unlock()
		if got >= want {
			return
		}
		select {
		case <-changed:
		case <-timeout:
			t.Fatalf("%v is %d, waited for %d", k, got, want)
		}
	}
}

// watchPermitWaits counts, per semaphore of c, the requests that queued for a
// permit because none was free.
func watchPermitWaits(c *Client) *counts {
	w := newCounts()
	c.permitWaited = func(sem *limits.Semaphore) { w.add(sem, 1) }
	return w
}

// watchTemplateWaits counts, per key URI, the callers waiting for its
// template fetch. Set it after any replacement of c.templates.
func watchTemplateWaits(c *Client) *counts {
	w := newCounts()
	c.templates.waiting = func(key string, waiters int) { w.set(key, waiters) }
	return w
}

// fakeWrapper is the wrapper the tests talk to. It answers like
// wrapper-manager v2 (or, with bare, like a wrapper-lite without login API):
//
//   - /status, /login and /logout run a login state machine: every reply is
//     HTTP 200 with a {code, msg, data} envelope, code 2 asks for a two-step
//     code, and a code is accepted only for an account whose first phase
//     asked for one;
//   - /key answers the template xor holds for its uri, and refuses any other
//     uri; with xor nil every /key and /m3u8 gets a template of 1;
//   - with serial, /key and /m3u8 are answered one at a time in the order they
//     arrived, as wrapper-lite does behind its mutex, the first held until
//     holdFirst is closed.
//
// The zero value is ready to use; set the fields before the first request.
type fakeWrapper struct {
	bare      bool
	needsCode bool
	// hold, when set, blocks every /login that carries a code until it is closed.
	hold chan struct{}
	// stall, when set, blocks every /login without a code until it is closed
	// or the client goes away.
	stall chan struct{}
	// reply, when set for a username, answers that account's /login.
	reply    map[string][2]any
	accounts map[string]bool

	xor       map[string]int
	serial    bool
	holdFirst chan struct{}

	init sync.Once
	ev   *counts // "/login" (logins seen), "gone" (stalled logins whose client left), "arrived" (/key and /m3u8)

	mu       sync.Mutex
	pending  map[string]bool
	logins   []loginBody
	logouts  []string
	keyCalls map[string]int
	order    []string // "path adamId" of every /key and /m3u8, in arrival order
	busy     bool
	queue    []chan struct{}
}

func (f *fakeWrapper) events() *counts {
	f.init.Do(func() { f.ev = newCounts() })
	return f.ev
}

func (f *fakeWrapper) loginCalls() []loginBody {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.logins)
}

func (f *fakeWrapper) logoutCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.logouts)
}

func (f *fakeWrapper) callCount(uri string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.keyCalls[uri]
}

// served lists the /key and /m3u8 requests in the order they arrived.
func (f *fakeWrapper) served() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.order)
}

func (f *fakeWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/status":
		data := map[string]any{"regions": []string{"us"}}
		if !f.bare {
			f.mu.Lock()
			data["clientCount"] = len(f.accounts)
			f.mu.Unlock()
			data["ready"] = true
		}
		writeReply(w, 0, "SUCCESS", data)
	case "/login":
		f.serveLogin(w, r)
	case "/logout":
		f.serveLogout(w, r)
	case "/key", "/m3u8":
		f.servePlayback(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeWrapper) servePlayback(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Query().Get("uri")
	f.mu.Lock()
	first := len(f.order) == 0
	f.order = append(f.order, r.URL.Path+" "+r.URL.Query().Get("adamId"))
	if r.URL.Path == "/key" {
		if f.keyCalls == nil {
			f.keyCalls = map[string]int{}
		}
		f.keyCalls[uri]++
	}
	var turn chan struct{}
	if f.serial {
		if f.busy {
			turn = make(chan struct{})
			f.queue = append(f.queue, turn)
		}
		f.busy = true
	}
	f.mu.Unlock()
	f.events().add("arrived", 1)
	if turn != nil {
		<-turn
	}
	if f.serial {
		defer f.next()
		if first && f.holdFirst != nil {
			<-f.holdFirst
		}
	}
	if f.xor == nil {
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 1, "m3u8": "#EXTM3U"})
		return
	}
	x, ok := f.xor[uri]
	if r.URL.Path != "/key" || !ok {
		writeReply(w, 400, "unexpected request", nil)
		return
	}
	writeReply(w, 0, "SUCCESS", map[string]any{"xor": x})
}

// next hands the serial turn to the request that arrived next.
func (f *fakeWrapper) next() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		f.busy = false
		return
	}
	close(f.queue[0])
	f.queue = f.queue[1:]
}

func (f *fakeWrapper) serveLogin(w http.ResponseWriter, r *http.Request) {
	var body loginBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeReply(w, -1, "bad body", nil)
		return
	}
	f.mu.Lock()
	f.logins = append(f.logins, body)
	hold, stall := f.hold, f.stall
	f.mu.Unlock()
	f.events().add("/login", 1)
	if hold != nil && body.Code != "" {
		<-hold
	}
	if stall != nil && body.Code == "" {
		select {
		case <-stall:
		case <-r.Context().Done():
			f.events().add("gone", 1)
			return
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if canned, ok := f.reply[body.Username]; ok {
		writeReply(w, canned[0].(int), canned[1].(string), nil)
		return
	}
	switch {
	case f.accounts[body.Username]:
		writeReply(w, -1, "already login", nil)
	case body.Password != testPassword:
		writeReply(w, -1, "login failed (exit code 1)", nil)
	case body.Code == "" && f.needsCode:
		if f.pending == nil {
			f.pending = map[string]bool{}
		}
		f.pending[body.Username] = true
		writeReply(w, replyNeedsTwoStep, "need 2fa", nil)
	case body.Code != "" && !f.pending[body.Username]:
		writeReply(w, -1, "no pending login for this account, restart login without code", nil)
	case body.Code != "" && body.Code != testCode:
		delete(f.pending, body.Username)
		writeReply(w, -1, "login failed (exit code 1)", nil)
	default:
		delete(f.pending, body.Username)
		if f.accounts == nil {
			f.accounts = map[string]bool{}
		}
		f.accounts[body.Username] = true
		writeReply(w, 0, "SUCCESS", nil)
	}
}

func (f *fakeWrapper) serveLogout(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logouts = append(f.logouts, body.Username)
	if !f.accounts[body.Username] {
		writeReply(w, -1, "no such account", nil)
		return
	}
	delete(f.accounts, body.Username)
	writeReply(w, 0, "SUCCESS", nil)
}

// trackCtx is the context a job's track i runs under.
func trackCtx(i int) context.Context {
	return limits.WithSubpriority(limits.WithPriority(context.Background(), 1), int64(i))
}
