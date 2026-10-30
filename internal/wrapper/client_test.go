package wrapper

import (
	"amdl/internal/config"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// wrapper-lite answers /key and /m3u8 one at a time behind one mutex, so the
// client keeps the rest of a burst waiting on its own side.
func TestKeyAndM3U8ShareOnePlaybackLimit(t *testing.T) {
	var inFlight, peak atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		inFlight.Add(-1)
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 1, "m3u8": "#EXTM3U"})
	}))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			if _, err := c.M3U8(context.Background(), fmt.Sprint(i)); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			session, _ := c.NewDecryptSession(context.Background(), "1")
			if _, err := session.DecryptFragment(fmt.Sprintf("skd://k%d", i), [][]byte{make([]byte, 16)}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if p := peak.Load(); p > playbackRequestLimit {
		t.Fatalf("peak /key + /m3u8 requests in flight = %d, want at most %d", p, playbackRequestLimit)
	}
}

func TestStatusReportsLoginThroughRegions(t *testing.T) {
	tests := []struct {
		name    string
		regions any
		want    Status
	}{
		{name: "logged in", regions: []string{"cn"}, want: Status{Ready: true, LoggedIn: true, Regions: []string{"cn"}, Accounts: []string{}}},
		{name: "not logged in", regions: []string{}, want: Status{Ready: true, LoggedIn: false, Regions: []string{}, Accounts: []string{}}},
		{name: "regions missing", regions: nil, want: Status{Ready: true, LoggedIn: false, Regions: []string{}, Accounts: []string{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/status" {
					t.Errorf("path = %s, want /status", r.URL.Path)
				}
				data := map[string]any{}
				if tt.regions != nil {
					data["regions"] = tt.regions
				}
				writeReply(w, 0, "SUCCESS", data)
			}))
			got, err := c.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Status() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestStatusOfAnUnreachableWrapperIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	address := srv.URL
	srv.Close()
	c, err := NewClient(config.WrapperConfig{Address: address, TimeoutSeconds: 5}, WithTemplateParser(parseXOR))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("Status() of a closed server succeeded")
	}
}

func TestFailedRepliesAreReplyErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    ReplyError
	}{
		{
			name:    "envelope error",
			handler: func(w http.ResponseWriter, r *http.Request) { writeReply(w, 404, "failed to get m3u8", nil) },
			want:    ReplyError{Endpoint: "/m3u8", HTTPStatus: 200, Code: 404, Msg: "failed to get m3u8"},
		},
		{
			name: "unknown route",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				writeReply(w, 404, "not found", nil)
			},
			want: ReplyError{Endpoint: "/m3u8", HTTPStatus: 404, Code: 404, Msg: "not found"},
		},
		{
			name:    "not an envelope",
			handler: func(w http.ResponseWriter, r *http.Request) { http.Error(w, "upstream down", http.StatusBadGateway) },
			want:    ReplyError{Endpoint: "/m3u8", HTTPStatus: 502, Msg: "upstream down\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, tt.handler)
			_, err := c.M3U8(context.Background(), "1")
			var reply *ReplyError
			if !errors.As(err, &reply) || !errors.Is(err, ErrWrapperResponse) {
				t.Fatalf("M3U8() error = %v, want a ReplyError wrapping ErrWrapperResponse", err)
			}
			if *reply != tt.want {
				t.Fatalf("ReplyError = %#v, want %#v", *reply, tt.want)
			}
		})
	}
}

func TestLyricsSendsItsParametersAsGiven(t *testing.T) {
	tests := []struct {
		name string
		opts LyricsRequestOptions
		want url.Values
	}{
		{
			name: "all set",
			opts: LyricsRequestOptions{Language: "ja", Syllable: true, Script: "ja-Latn"},
			want: url.Values{"adamId": {"42"}, "language": {"ja"}, "syllable": {"1"}, "script": {"ja-Latn"}},
		},
		{
			name: "defaults left to the wrapper except syllable",
			opts: LyricsRequestOptions{},
			want: url.Values{"adamId": {"42"}, "syllable": {"0"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got url.Values
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = r.URL.Query()
				writeReply(w, 0, "SUCCESS", map[string]any{"lyrics": "<tt/>"})
			}))
			lyrics, err := c.Lyrics(context.Background(), "42", tt.opts)
			if err != nil || lyrics != "<tt/>" {
				t.Fatalf("Lyrics() = (%q, %v), want <tt/>", lyrics, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("query = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPlaybackManifestsAndLicense(t *testing.T) {
	var license map[string]string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/m3u8":
			writeReply(w, 0, "SUCCESS", map[string]any{"adamId": r.URL.Query().Get("adamId"), "m3u8": "device-" + r.URL.Query().Get("adamId")})
		case "/webplayback":
			writeReply(w, 0, "SUCCESS", map[string]any{"m3u8": "web-" + r.URL.Query().Get("adamId")})
		case "/license":
			raw, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || json.Unmarshal(raw, &license) != nil {
				writeReply(w, 400, "invalid JSON body", nil)
				return
			}
			writeReply(w, 0, "SUCCESS", map[string]any{"license": "TElD", "renew": 0})
		}
	}))
	ctx := context.Background()
	if got, err := c.M3U8(ctx, "7"); err != nil || got != "device-7" {
		t.Fatalf("M3U8() = (%q, %v)", got, err)
	}
	if got, err := c.WebPlayback(ctx, "7"); err != nil || got != "web-7" {
		t.Fatalf("WebPlayback() = (%q, %v)", got, err)
	}
	got, err := c.License(ctx, "7", "Q0hBTA==", "data:;base64,AAAA")
	if err != nil || got != "TElD" {
		t.Fatalf("License() = (%q, %v)", got, err)
	}
	if want := map[string]string{"adamId": "7", "challenge": "Q0hBTA==", "uri": "data:;base64,AAAA"}; !reflect.DeepEqual(license, want) {
		t.Fatalf("license body = %v, want %v", license, want)
	}
}

func TestNewClientRejects(t *testing.T) {
	tests := []struct {
		name    string
		address string
		parse   TemplateParser
	}{
		{name: "host:port", address: "127.0.0.1:8080", parse: parseXOR},
		{name: "other scheme", address: "grpc://127.0.0.1:8080", parse: parseXOR},
		{name: "no host", address: "http://", parse: parseXOR},
		{name: "no template parser", address: "http://127.0.0.1:12340", parse: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if c, err := NewClient(config.WrapperConfig{Address: tt.address}, WithTemplateParser(tt.parse)); err == nil {
				_ = c.Close()
				t.Fatal("NewClient() succeeded")
			}
		})
	}
}
