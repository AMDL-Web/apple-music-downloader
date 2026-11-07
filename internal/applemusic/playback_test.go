package applemusic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testUserToken = "user-token-value-0123456789"

// playbackClient answers every request but a token scrape with respond and
// keeps each request and its JSON body.
func playbackClient(t *testing.T, respond func(*http.Request) *http.Response) (*CatalogClient, *[]*http.Request, *[]map[string]any) {
	t.Helper()
	var reqs []*http.Request
	var bodies []map[string]any
	client := newTestClient(t, libraryConfig, func(req *http.Request) *http.Response {
		if resp, ok := scrapeResponse(req); ok {
			return resp
		}
		reqs = append(reqs, req)
		var body map[string]any
		if req.Body != nil {
			raw, _ := io.ReadAll(req.Body) // a test request body always reads
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Errorf("request body is not JSON: %v", err)
				}
			}
		}
		bodies = append(bodies, body)
		return respond(req)
	})
	return client, &reqs, &bodies
}

// assertNoSecrets fails when err's text repeats the media-user-token or the
// web JWT.
func assertNoSecrets(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if msg := err.Error(); strings.Contains(msg, testUserToken) || strings.Contains(msg, "cached-token") {
		t.Fatalf("error %q repeats a token", msg)
	}
}

func TestWebPlaybackRequestShape(t *testing.T) {
	client, reqs, bodies := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"status":0,"songList":[{"hls-playlist-url":"https://aod.itunes.apple.com/p.m3u8"}]}`)
	})
	got, err := client.WebPlayback(context.Background(), testUserToken, "1635388718")
	if err != nil || got != "https://aod.itunes.apple.com/p.m3u8" {
		t.Fatalf("WebPlayback = (%q, %v)", got, err)
	}
	req := (*reqs)[0]
	if req.Method != http.MethodPost || req.URL.String() != "https://play.music.apple.com/WebObjects/MZPlay.woa/wa/webPlayback" {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	checkPlaybackHeaders(t, req)
	if want := map[string]any{"salableAdamId": "1635388718"}; !equalJSON((*bodies)[0], want) {
		t.Fatalf("body = %v, want %v", (*bodies)[0], want)
	}
}

func checkPlaybackHeaders(t *testing.T, req *http.Request) {
	t.Helper()
	if req.Header.Get("Authorization") != "Bearer cached-token" {
		t.Error("Authorization does not carry the scraped web JWT")
	}
	if req.Header.Get("X-Apple-Music-User-Token") != testUserToken {
		t.Error("X-Apple-Music-User-Token does not carry the media-user-token")
	}
	if req.Header.Get("Media-User-Token") != "" {
		t.Error("the play.* hosts take the token in X-Apple-Music-User-Token only")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", req.Header.Get("Content-Type"))
	}
}

func equalJSON(got map[string]any, want map[string]any) bool {
	a, _ := json.Marshal(got)  // maps of JSON values always marshal
	b, _ := json.Marshal(want) // maps of JSON values always marshal
	return string(a) == string(b)
}

func TestWebPlaybackReadsTheAACLCAssetWithoutAPlaylistURL(t *testing.T) {
	client, _, _ := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"status":0,"songList":[{"assets":[
			{"flavor":"30:cbcp256","URL":"https://example.com/other.m3u8"},
			{"flavor":"28:ctrp256","URL":"https://example.com/aac.m3u8"}]}]}`)
	})
	got, err := client.WebPlayback(context.Background(), testUserToken, "1")
	if err != nil || got != "https://example.com/aac.m3u8" {
		t.Fatalf("WebPlayback = (%q, %v)", got, err)
	}
}

func TestWebPlaybackErrors(t *testing.T) {
	cases := []struct {
		name          string
		status        int
		body          string
		tokenRejected bool
		nonRetryable  bool
	}{
		{"failureType 2002 is a rejected token", http.StatusOK, `{"failureType":"2002","customerMessage":"Your session has ended."}`, true, true},
		{"numeric failureType", http.StatusOK, `{"failureType":2002}`, true, true},
		{"401 after a fresh JWT is a rejected token", http.StatusUnauthorized, `{}`, true, true},
		{"403 is a rejected token", http.StatusForbidden, `{}`, true, true},
		{"503 is retried", http.StatusServiceUnavailable, `{}`, false, false},
		{"429 is retried", http.StatusTooManyRequests, `{}`, false, false},
		{"no asset is final", http.StatusOK, `{"status":0,"songList":[]}`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _, _ := playbackClient(t, func(*http.Request) *http.Response {
				return jsonResponse(tc.status, tc.body)
			})
			client.retryDelay = func(http.Header) time.Duration { return 0 }
			_, err := client.WebPlayback(context.Background(), testUserToken, "1")
			if err == nil {
				t.Fatal("WebPlayback succeeded")
			}
			assertNoSecrets(t, err)
			if got := IsUserTokenRejected(err); got != tc.tokenRejected {
				t.Fatalf("IsUserTokenRejected(%v) = %v, want %v", err, got, tc.tokenRejected)
			}
			var marker interface{ NonRetryable() bool }
			if got := errors.As(err, &marker) && marker.NonRetryable(); got != tc.nonRetryable {
				t.Fatalf("NonRetryable(%v) = %v, want %v", err, got, tc.nonRetryable)
			}
		})
	}
}

// A 429 is retried once through the request gate, and the retry must carry
// the whole JSON body again: Apple would read an empty one as a bad request.
func TestWebPlaybackRetriesA429WithTheBody(t *testing.T) {
	calls := 0
	client, _, bodies := playbackClient(t, func(*http.Request) *http.Response {
		calls++
		if calls == 1 {
			return jsonResponse(http.StatusTooManyRequests, `{}`)
		}
		return jsonResponse(http.StatusOK, `{"songList":[{"hls-playlist-url":"https://aod.itunes.apple.com/p.m3u8"}]}`)
	})
	client.retryDelay = func(http.Header) time.Duration { return 0 }
	got, err := client.WebPlayback(context.Background(), testUserToken, "1")
	if err != nil || got != "https://aod.itunes.apple.com/p.m3u8" {
		t.Fatalf("WebPlayback = (%q, %v)", got, err)
	}
	if len(*bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(*bodies))
	}
	for i, body := range *bodies {
		if body["salableAdamId"] != "1" {
			t.Fatalf("request %d body = %v, want salableAdamId 1", i+1, body)
		}
	}
}

func TestLicenseRequestShape(t *testing.T) {
	client, reqs, bodies := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"status":0,"license":"bGljZW5zZQ==","renew-after":86400}`)
	})
	got, err := client.License(context.Background(), testUserToken, "1635388718", "Y2hhbGxlbmdl", "skd://key")
	if err != nil || got != "bGljZW5zZQ==" {
		t.Fatalf("License = (%q, %v)", got, err)
	}
	req := (*reqs)[0]
	if req.Method != http.MethodPost || req.URL.String() != "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense" {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	checkPlaybackHeaders(t, req)
	want := map[string]any{
		"adamId": "1635388718", "challenge": "Y2hhbGxlbmdl", "isLibrary": false,
		"key-system": "com.widevine.alpha", "uri": "skd://key", "user-initiated": true,
	}
	if !equalJSON((*bodies)[0], want) {
		t.Fatalf("body = %v, want %v", (*bodies)[0], want)
	}
}

func TestLicenseErrors(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		tokenRejected bool
	}{
		{"failureType", `{"failureType":"2002","status":-1002}`, true},
		{"no license", `{"status":-1021}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _, _ := playbackClient(t, func(*http.Request) *http.Response {
				return jsonResponse(http.StatusOK, tc.body)
			})
			_, err := client.License(context.Background(), testUserToken, "1", "Y2hhbGxlbmdl", "skd://key")
			if err == nil {
				t.Fatal("License succeeded")
			}
			assertNoSecrets(t, err)
			if got := IsUserTokenRejected(err); got != tc.tokenRejected {
				t.Fatalf("IsUserTokenRejected(%v) = %v, want %v", err, got, tc.tokenRejected)
			}
		})
	}
}

func TestSongLyricsRequestShape(t *testing.T) {
	client, reqs, _ := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"data":[{"id":"1","type":"syllable-lyrics","attributes":{"ttmlLocalizations":"<tt itunes:timing=\"Word\"/>"}}]}`)
	})
	got, err := client.SongLyrics(context.Background(), testUserToken, "cn", "536115195", "zh-Hans_CN")
	if err != nil || got != `<tt itunes:timing="Word"/>` {
		t.Fatalf("SongLyrics = (%q, %v)", got, err)
	}
	req := (*reqs)[0]
	if req.Method != http.MethodGet || req.URL.Host != libraryHost || req.URL.Path != "/v1/catalog/cn/songs/536115195/syllable-lyrics" {
		t.Fatalf("request = %s %s", req.Method, req.URL)
	}
	q := req.URL.Query()
	if q.Get("l[lyrics]") != "zh-Hans_CN" || q.Get("extend") != "ttmlLocalizations" || q.Get("l[script]") != "en-Latn" {
		t.Fatalf("query = %v", q)
	}
	if req.Header.Get("Media-User-Token") != testUserToken || req.Header.Get("Origin") != libraryOrigin || req.Header.Get("Authorization") != "Bearer cached-token" {
		t.Fatal("syllable-lyrics needs the web JWT, Origin and Media-User-Token")
	}
}

func TestSongLyricsAnswers(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		want     string
		notFound bool
		noLyrics bool
	}{
		{"plain text", http.StatusOK, `{"data":[{"attributes":{"text":"first line"}}]}`, "first line", false, false},
		{"empty", http.StatusOK, `{"data":[{"attributes":{}}]}`, "", false, true},
		{"no data", http.StatusOK, `{"data":[]}`, "", false, true},
		{"404", http.StatusNotFound, `{"errors":[{"status":"404","code":"40403","title":"No related resources"}]}`, "", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _, _ := playbackClient(t, func(*http.Request) *http.Response {
				return jsonResponse(tc.status, tc.body)
			})
			got, err := client.SongLyrics(context.Background(), testUserToken, "cn", "1", "")
			assertNoSecrets(t, err)
			if got != tc.want || IsNotFound(err) != tc.notFound || errors.Is(err, ErrNoLyrics) != tc.noLyrics {
				t.Fatalf("SongLyrics = (%q, %v)", got, err)
			}
		})
	}
}

func TestSongLyricsDefaultsTheLanguageLikeTheWrapper(t *testing.T) {
	client, reqs, _ := playbackClient(t, func(*http.Request) *http.Response {
		return jsonResponse(http.StatusOK, `{"data":[{"attributes":{"text":"x"}}]}`)
	})
	if _, err := client.SongLyrics(context.Background(), testUserToken, "cn", "1", ""); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[0].URL.Query().Get("l[lyrics]"); got != "en" {
		t.Fatalf("l[lyrics] = %q, want wrapper-lite's default en", got)
	}
}

func TestAccountStorefrontIsReadOncePerToken(t *testing.T) {
	client, reqs, _ := playbackClient(t, func(req *http.Request) *http.Response {
		if req.URL.Path != "/v1/me/storefront" || req.Header.Get("Media-User-Token") == "" {
			return jsonResponse(http.StatusBadRequest, `{}`)
		}
		return jsonResponse(http.StatusOK, `{"data":[{"id":"cn","type":"storefronts"}]}`)
	})
	ctx := context.Background()
	for _, token := range []string{testUserToken, testUserToken, "another-token-value-01234"} {
		if got, err := client.AccountStorefront(ctx, token); err != nil || got != "cn" {
			t.Fatalf("AccountStorefront = (%q, %v)", got, err)
		}
	}
	if len(*reqs) != 2 {
		t.Fatalf("requests = %d, want one per distinct token", len(*reqs))
	}
}
