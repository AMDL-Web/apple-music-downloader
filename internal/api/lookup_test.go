package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/usertoken"
)

func newLookupServer(t *testing.T, catalog CatalogService, mutate func(*config.Config)) http.Handler {
	t.Helper()
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg)
	}
	return newServer(t, Deps{Config: config.NewStore(cfg), Catalog: catalog, AppleLibrary: &fakeAppleLibrary{}}).Routes()
}

func decodeLookup(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return got
}

var palette = applemusic.ArtworkColors{BgColor: "101010", TextColor1: "f1f1f1", TextColor2: "e2e2e2", TextColor3: "d3d3d3", TextColor4: "c4c4c4"}

func TestLookupAlbumReturnsLinkAndJobRowMetadata(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: &applemusic.Collection{
		ID: "1463409338", Name: "IGOR", Artist: "Tyler, The Creator", ArtistURL: "https://music.apple.com/us/artist/420368335",
		ArtworkURL: "https://is1.mzstatic.com/a/{w}x{h}bb.jpg", ArtworkColors: palette,
		ReleaseDate: "2019-05-17", GenreNames: []string{"Music", "Hip-Hop/Rap"},
		ContentRating: "explicit", AppleDigitalMaster: true, AudioTraits: []string{"lossless", "lossy-stereo"},
		Tracks: make([]applemusic.Song, 12),
	}}
	recorder := requestJSON(t, newLookupServer(t, catalog, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"  https://music.apple.com/us/album/igor/1463409338  "}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	want := map[string]any{
		"input": "https://music.apple.com/us/album/igor/1463409338", "type": "album", "storefront": "us", "id": "1463409338",
		"title": "IGOR", "artist_name": "Tyler, The Creator", "artist_url": "https://music.apple.com/us/artist/420368335",
		"artwork_url": "https://is1.mzstatic.com/a/{w}x{h}bb.jpg", "artwork_bg_color": "101010",
		"artwork_text_color1": "f1f1f1", "artwork_text_color2": "e2e2e2", "artwork_text_color3": "d3d3d3", "artwork_text_color4": "c4c4c4",
		"release_date": "2019-05-17", "genre": "Hip-Hop/Rap", "track_count": float64(12),
		"content_rating": "explicit", "apple_digital_master": true, "audio_traits": []any{"lossless", "lossy-stereo"},
	}
	if got := decodeLookup(t, recorder.Body.Bytes()); !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %v\nwant       %v", got, want)
	}
	if want := []string{"album us/1463409338"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
}

// An album link with ?i= follows catalog.album_track_url_mode like a
// submission, and reports the track either way.
func TestLookupAlbumTrackLinkFollowsTrackMode(t *testing.T) {
	const link = "https://music.apple.com/us/album/igor/1463409338?i=1463409349"
	song := &applemusic.Song{
		ID: "1463409349", Name: "EARFQUAKE", ArtistName: "Tyler, The Creator", AlbumName: "IGOR",
		AlbumArtworkURL: "album-art", AlbumArtworkColors: palette, ReleaseDate: "2019-05-17",
		GenreNames: []string{"Hip-Hop/Rap"}, DurationInMillis: 190067, ContentRating: "explicit",
	}
	catalog := &fakeCatalog{t: t, song: song}
	recorder := requestJSON(t, newLookupServer(t, catalog, nil), http.MethodPost, "/api/v1/lookup", `{"url":"`+link+`"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	got := decodeLookup(t, recorder.Body.Bytes())
	for key, want := range map[string]any{
		"type": "song", "id": "1463409349", "track_id": "1463409349", "title": "EARFQUAKE", "album_name": "IGOR",
		"artwork_url": "album-art", "artwork_bg_color": "101010", "track_count": float64(1), "duration_ms": float64(190067),
	} {
		if got[key] != want {
			t.Errorf("%s = %v, want %v", key, got[key], want)
		}
	}
	if _, ok := got["apple_digital_master"]; ok {
		t.Errorf("apple_digital_master present for a song that is not one: %v", got)
	}

	albumCatalog := &fakeCatalog{t: t, album: &applemusic.Collection{Name: "IGOR", Tracks: make([]applemusic.Song, 12)}}
	albumMode := newLookupServer(t, albumCatalog, func(cfg *config.Config) { cfg.Catalog.AlbumTrackURLMode = "album" })
	recorder = requestJSON(t, albumMode, http.MethodPost, "/api/v1/lookup", `{"url":"`+link+`"}`)
	got = decodeLookup(t, recorder.Body.Bytes())
	if got["type"] != "album" || got["id"] != "1463409338" || got["track_id"] != "1463409349" {
		t.Fatalf("album mode = %v", got)
	}
}

// A lookup reads private playlist covers with the wrapper's token and never
// echoes it.
func TestLookupPlaylistUsesTheWrapperTokenButNeverEchoesIt(t *testing.T) {
	secret := strings.Repeat("S", 200)
	playlist := &applemusic.Collection{Name: "Mix", Artist: "Apple Music Hip-Hop", ArtworkURL: "pl-art", Tracks: make([]applemusic.Song, 50)}
	catalog := &fakeCatalog{t: t, playlist: playlist}
	handler := newServer(t, Deps{Config: config.NewStore(config.Default()), Catalog: catalog, AppleLibrary: &fakeAppleLibrary{},
		Tokens: usertoken.NewReading(func(string) (string, error) { return " " + secret + " ", nil })}).Routes()
	recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/us/playlist/mix/pl.u-abc"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if catalog.token != secret {
		t.Fatalf("token passed to the catalog: %d bytes, want the wrapper's", len(catalog.token))
	}
	if body := recorder.Body.String(); strings.Contains(body, "token") {
		t.Fatalf("response mentions a token: %s", body)
	}
	got := decodeLookup(t, recorder.Body.Bytes())
	if got["title"] != "Mix" || got["curator_name"] != "Apple Music Hip-Hop" || got["track_count"] != float64(50) || got["artist_name"] != nil {
		t.Fatalf("playlist = %v", got)
	}

	// The request body no longer carries a token: the field is unknown.
	recorder = requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/us/playlist/mix/pl.u-abc","media_user_token":"x"}`)
	assertErrorCode(t, recorder, http.StatusBadRequest, "invalid_request")
	if !strings.Contains(recorder.Body.String(), "media_user_token") {
		t.Fatalf("error body %q does not name the unknown field", recorder.Body.String())
	}
}

// A song, album or playlist link is read in the account storefront, as its
// download is, so a us link on a cn account previews the cn release; the
// response keeps the link's storefront. A station is read in its own.
func TestLookupReadsInTheAccountStorefront(t *testing.T) {
	catalog := &fakeCatalog{t: t, album: &applemusic.Collection{ID: "1463409338", Name: "IGOR", Tracks: make([]applemusic.Song, 9)},
		station: &applemusic.StationInfo{Name: "Chill Station", Format: "tracks"}}
	cfg := config.Default()
	handler := newServer(t, Deps{Config: config.NewStore(cfg), Catalog: catalog, AppleLibrary: &fakeAppleLibrary{},
		AccountStorefront: func(context.Context) (string, error) { return "cn", nil }}).Routes()

	recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/us/album/igor/1463409338"}`)
	got := decodeLookup(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["storefront"] != "us" || got["track_count"] != float64(9) {
		t.Fatalf("album: status %d, %v", recorder.Code, got)
	}
	requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/us/station/chill/ra.123"}`)
	if want := []string{"album cn/1463409338", "station us/ra.123"}; !reflect.DeepEqual(catalog.calls, want) {
		t.Fatalf("catalog calls = %v, want %v", catalog.calls, want)
	}
}

func TestLookupStationAndArtist(t *testing.T) {
	station := &fakeCatalog{t: t, station: &applemusic.StationInfo{Name: "Chill Station", ArtworkURL: "st-art", Format: "tracks"}}
	recorder := requestJSON(t, newLookupServer(t, station, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/station/chill/ra.123"}`)
	got := decodeLookup(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["curator_name"] != applemusic.StationCuratorName || got["needs_media_user_token"] != true || got["track_count"] != nil {
		t.Fatalf("station: status %d, %v", recorder.Code, got)
	}

	live := &fakeCatalog{t: t, station: &applemusic.StationInfo{Name: "Apple Music 1", Format: "stream", IsLive: true}}
	recorder = requestJSON(t, newLookupServer(t, live, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/station/apple-music-1/ra.978194965"}`)
	assertLookupErrorLink(t, recorder, http.StatusBadRequest, CodeInvalidRequest, "station")

	artist := &fakeCatalog{t: t, artist: &applemusic.Artist{Name: "Taylor Swift", URL: "https://music.apple.com/us/artist/159260351", GenreNames: []string{"Music", "Pop"}, ArtworkURL: "ar-art"}}
	recorder = requestJSON(t, newLookupServer(t, artist, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/artist/taylor-swift/159260351"}`)
	got = decodeLookup(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["title"] != "Taylor Swift" || got["artist_name"] != "Taylor Swift" ||
		got["artist_url"] != "https://music.apple.com/us/artist/159260351" || got["genre"] != "Pop" || got["track_count"] != nil {
		t.Fatalf("artist: status %d, %v", recorder.Code, got)
	}
}

func TestLookupRejectsLinksItCannotDownload(t *testing.T) {
	catalog := &fakeCatalog{t: t}
	handler := newLookupServer(t, catalog, nil)
	recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/music-video/never-gonna-give-you-up/1558533900"}`)
	assertLookupErrorLink(t, recorder, http.StatusBadRequest, CodeInvalidRequest, "music-video")

	for _, body := range []string{
		`{"url":"https://example.com/us/album/x/1"}`,
		`{"url":"https://music.apple.com/us/podcast/x/1"}`,
		`{"url":"   "}`,
		`{}`,
		`{"url":"https://music.apple.com/us/album/x/1","mediaUserToken":"typo"}`,
	} {
		recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", body)
		details := assertErrorCode(t, recorder, http.StatusBadRequest, CodeInvalidRequest)
		if len(details) != 0 {
			t.Errorf("%s: details = %s, want none before the link parses", body, details)
		}
	}
	if len(catalog.calls) != 0 {
		t.Fatalf("catalog read for a rejected link: %v", catalog.calls)
	}
}

func TestLookupCatalogFailures(t *testing.T) {
	const link = `{"url":"https://music.apple.com/us/album/igor/1463409338"}`
	upstream := &fakeCatalog{t: t, err: errors.New("catalog request failed: 500 Internal Server Error")}
	recorder := requestJSON(t, newLookupServer(t, upstream, nil), http.MethodPost, "/api/v1/lookup", link)
	assertLookupErrorLink(t, recorder, http.StatusBadGateway, CodeUpstreamError, "album")

	slow := &fakeCatalog{t: t, err: fmt.Errorf("get album: %w", context.DeadlineExceeded)}
	recorder = requestJSON(t, newLookupServer(t, slow, nil), http.MethodPost, "/api/v1/lookup", link)
	assertLookupErrorLink(t, recorder, http.StatusGatewayTimeout, CodeUpstreamTimeout, "album")
}

func TestLookupMissingIDIsNotFoundAndNoWarning(t *testing.T) {
	const link = `{"url":"https://music.apple.com/us/album/igor/1463409338"}`
	for _, tt := range []struct {
		err      error
		status   int
		code     ErrorCode
		logLevel string
	}{
		{fmt.Errorf("get album: %w", notFoundError{}), http.StatusNotFound, CodeCatalogNotFound, "level=INFO"},
		{errors.New("Apple Music request for album 1463409338 in storefront us failed: 503 Service Unavailable"), http.StatusBadGateway, CodeUpstreamError, "level=WARN"},
	} {
		var logs strings.Builder
		server := newServer(t, Deps{
			Config: config.NewStore(config.Default()), Catalog: &fakeCatalog{t: t, err: tt.err},
			Logger: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		})
		recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/lookup", link)
		assertLookupErrorLink(t, recorder, tt.status, tt.code, "album")
		if line := logs.String(); !strings.Contains(line, "lookup failed") || !strings.Contains(line, tt.logLevel) {
			t.Fatalf("%v logged %q, want a %s lookup failed line", tt.err, line, tt.logLevel)
		}
	}
}

// assertLookupErrorLink checks the error envelope and that details.link
// carries the parsed link, so a client can still show what was pasted.
func assertLookupErrorLink(t *testing.T, recorder *httptest.ResponseRecorder, status int, code ErrorCode, wantType string) {
	t.Helper()
	raw := assertErrorCode(t, recorder, status, code)
	var details struct {
		Link lookupLinkFields `json:"link"`
	}
	if err := json.Unmarshal(raw, &details); err != nil {
		t.Fatalf("details %s: %v", raw, err)
	}
	if string(details.Link.Type) != wantType || details.Link.Storefront != "us" || details.Link.ID == "" || details.Link.Input == "" {
		t.Fatalf("details.link = %+v, want type %s", details.Link, wantType)
	}
}
