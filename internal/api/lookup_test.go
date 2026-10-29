package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

// fakeCatalog records the reads POST /lookup makes and answers from its
// fields; a read whose result is unset fails the test.
type fakeCatalog struct {
	t        *testing.T
	song     *applemusic.Song
	album    *applemusic.Collection
	playlist *applemusic.Collection
	station  *applemusic.StationInfo
	artist   *applemusic.Artist
	err      error
	calls    []string
	token    string
}

func (f *fakeCatalog) record(method, storefront, id string) error {
	f.calls = append(f.calls, fmt.Sprintf("%s %s/%s", method, storefront, id))
	return f.err
}

func (f *fakeCatalog) SongMetadata(_ context.Context, storefront, id string) (applemusic.Song, error) {
	if err := f.record("song", storefront, id); err != nil || f.song == nil {
		return applemusic.Song{}, f.missing(err, "song")
	}
	return *f.song, nil
}

func (f *fakeCatalog) Album(_ context.Context, storefront, id string) (applemusic.Collection, error) {
	if err := f.record("album", storefront, id); err != nil || f.album == nil {
		return applemusic.Collection{}, f.missing(err, "album")
	}
	return *f.album, nil
}

func (f *fakeCatalog) Playlist(_ context.Context, storefront, id, mediaUserToken string) (applemusic.Collection, error) {
	f.token = mediaUserToken
	if err := f.record("playlist", storefront, id); err != nil || f.playlist == nil {
		return applemusic.Collection{}, f.missing(err, "playlist")
	}
	return *f.playlist, nil
}

func (f *fakeCatalog) Station(_ context.Context, storefront, id string) (applemusic.StationInfo, error) {
	if err := f.record("station", storefront, id); err != nil || f.station == nil {
		return applemusic.StationInfo{}, f.missing(err, "station")
	}
	return *f.station, nil
}

func (f *fakeCatalog) Artist(_ context.Context, storefront, id string) (applemusic.Artist, error) {
	if err := f.record("artist", storefront, id); err != nil || f.artist == nil {
		return applemusic.Artist{}, f.missing(err, "artist")
	}
	return *f.artist, nil
}

func (f *fakeCatalog) missing(err error, what string) error {
	if err != nil {
		return err
	}
	f.t.Errorf("unexpected %s read", what)
	return errors.New("unexpected read")
}

func newLookupServer(catalog CatalogService, mutate func(*config.Config)) http.Handler {
	cfg := config.Default()
	if mutate != nil {
		mutate(&cfg)
	}
	return (&Server{cfg: config.NewStore(cfg), catalog: catalog}).Routes()
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
		ContentRating: "explicit", AppleDigitalMaster: true,
		Tracks: make([]applemusic.Song, 12),
	}}
	recorder := requestJSON(t, newLookupServer(catalog, nil), http.MethodPost, "/api/v1/lookup",
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
		"content_rating": "explicit", "apple_digital_master": true,
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
	recorder := requestJSON(t, newLookupServer(catalog, nil), http.MethodPost, "/api/v1/lookup", `{"url":"`+link+`"}`)
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
	albumMode := newLookupServer(albumCatalog, func(cfg *config.Config) { cfg.Catalog.AlbumTrackURLMode = "album" })
	recorder = requestJSON(t, albumMode, http.MethodPost, "/api/v1/lookup", `{"url":"`+link+`"}`)
	got = decodeLookup(t, recorder.Body.Bytes())
	if got["type"] != "album" || got["id"] != "1463409338" || got["track_id"] != "1463409349" {
		t.Fatalf("album mode = %v", got)
	}
}

func TestLookupPlaylistUsesSettingsTokenButNeverEchoesIt(t *testing.T) {
	const secret = "settings-token-secret"
	playlist := &applemusic.Collection{Name: "Mix", Artist: "Apple Music Hip-Hop", ArtworkURL: "pl-art", Tracks: make([]applemusic.Song, 50)}
	catalog := &fakeCatalog{t: t, playlist: playlist}
	handler := newLookupServer(catalog, func(cfg *config.Config) { cfg.Catalog.MediaUserToken = " " + secret + " " })
	recorder := requestJSON(t, handler, http.MethodPost, "/api/v1/lookup", `{"url":"https://music.apple.com/us/playlist/mix/pl.u-abc"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if catalog.token != secret {
		t.Fatalf("token passed to the catalog = %q, want %q", catalog.token, secret)
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

func TestLookupStationAndArtist(t *testing.T) {
	station := &fakeCatalog{t: t, station: &applemusic.StationInfo{Name: "Chill Station", ArtworkURL: "st-art", Format: "tracks"}}
	recorder := requestJSON(t, newLookupServer(station, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/station/chill/ra.123"}`)
	got := decodeLookup(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["curator_name"] != applemusic.StationCuratorName || got["needs_media_user_token"] != true || got["track_count"] != nil {
		t.Fatalf("station: status %d, %v", recorder.Code, got)
	}

	live := &fakeCatalog{t: t, station: &applemusic.StationInfo{Name: "Apple Music 1", Format: "stream", IsLive: true}}
	recorder = requestJSON(t, newLookupServer(live, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/station/apple-music-1/ra.978194965"}`)
	assertLookupErrorLink(t, recorder, http.StatusBadRequest, CodeInvalidRequest, "station")

	artist := &fakeCatalog{t: t, artist: &applemusic.Artist{Name: "Taylor Swift", URL: "https://music.apple.com/us/artist/159260351", GenreNames: []string{"Music", "Pop"}, ArtworkURL: "ar-art"}}
	recorder = requestJSON(t, newLookupServer(artist, nil), http.MethodPost, "/api/v1/lookup",
		`{"url":"https://music.apple.com/us/artist/taylor-swift/159260351"}`)
	got = decodeLookup(t, recorder.Body.Bytes())
	if recorder.Code != http.StatusOK || got["title"] != "Taylor Swift" || got["artist_name"] != "Taylor Swift" ||
		got["artist_url"] != "https://music.apple.com/us/artist/159260351" || got["genre"] != "Pop" || got["track_count"] != nil {
		t.Fatalf("artist: status %d, %v", recorder.Code, got)
	}
}

func TestLookupRejectsLinksItCannotDownload(t *testing.T) {
	catalog := &fakeCatalog{t: t}
	handler := newLookupServer(catalog, nil)
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
	recorder := requestJSON(t, newLookupServer(upstream, nil), http.MethodPost, "/api/v1/lookup", link)
	assertLookupErrorLink(t, recorder, http.StatusBadGateway, CodeUpstreamError, "album")

	slow := &fakeCatalog{t: t, err: fmt.Errorf("get album: %w", context.DeadlineExceeded)}
	recorder = requestJSON(t, newLookupServer(slow, nil), http.MethodPost, "/api/v1/lookup", link)
	assertLookupErrorLink(t, recorder, http.StatusGatewayTimeout, CodeUpstreamTimeout, "album")
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
