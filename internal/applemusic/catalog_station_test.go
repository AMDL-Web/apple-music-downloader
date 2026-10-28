package applemusic

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"amdl/internal/config"
)

func TestStationTracksResolvesTracksFormat(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	var gotMediaToken string
	var nextTracksMethod string
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/catalog/us/stations/ra.1":
			body := `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"My Station","isLive":false,"artwork":{"url":"station-art"},"playParams":{"format":"tracks"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "/v1/me/stations/next-tracks/ra.1":
			gotMediaToken = req.Header.Get("Media-User-Token")
			nextTracksMethod = req.Method
			body := `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}},{"id":"video-1","type":"music-videos","attributes":{"name":"Video"}},{"id":"song-2","type":"songs","attributes":{"name":"Two","artistName":"Artist"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected path: " + req.URL.RequestURI())), Header: make(http.Header)}, nil
	})}

	station, err := client.StationTracks(context.Background(), "us", "ra.1", "media-token-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if station.Type != TypeStation || station.Name != "My Station" || station.ArtworkURL != "station-art" {
		t.Fatalf("station = %+v", station)
	}
	if got, want := trackIDs(station.Tracks), []string{"song-1", "song-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("track IDs = %#v, want %#v", got, want)
	}
	if gotMediaToken != "media-token-xyz" {
		t.Fatalf("Media-User-Token header = %q", gotMediaToken)
	}
	if nextTracksMethod != http.MethodPost {
		t.Fatalf("next-tracks method = %q, want POST", nextTracksMethod)
	}
}

func TestStationTracksRejectsLiveStream(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v1/catalog/us/stations/ra.live" {
			body := `{"data":[{"id":"ra.live","type":"stations","attributes":{"name":"Apple Music 1","isLive":true,"playParams":{"format":"stream"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		t.Fatalf("unexpected request to %s (live station must not hit next-tracks)", req.URL.RequestURI())
		return nil, nil
	})}

	if _, err := client.StationTracks(context.Background(), "us", "ra.live", "media-token-xyz"); err == nil {
		t.Fatal("expected live station to be rejected")
	}
}

func TestStationTracksRequiresMediaUserToken(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "test-token"
	client.tokenUntil = time.Now().Add(time.Hour)
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/v1/catalog/us/stations/ra.1" {
			body := `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"My Station","playParams":{"format":"tracks"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		t.Fatalf("unexpected request to %s (missing token must not hit next-tracks)", req.URL.RequestURI())
		return nil, nil
	})}

	if _, err := client.StationTracks(context.Background(), "us", "ra.1", "  "); err == nil {
		t.Fatal("expected missing media_user_token to be rejected")
	}
}

func TestStationNextTracksRetriesOnce401ThenSucceeds(t *testing.T) {
	client := newTestCatalogClient(config.CatalogConfig{Language: "en-US"}, slog.Default())
	client.token = "stale-token"
	client.tokenUntil = time.Now().Add(time.Hour)

	var nextTracksCalls int
	client.http = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/v1/catalog/us/stations/ra.1":
			body := `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"My Station","isLive":false,"artwork":{"url":"station-art"},"playParams":{"format":"tracks"}}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		case "/v1/me/stations/next-tracks/ra.1":
			nextTracksCalls++
			if nextTracksCalls == 1 {
				return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"errors":[{"status":"401"}]}`)), Header: make(http.Header)}, nil
			}
			body := `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}]}`
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`<script src="/assets/index~abc123.js"></script>`)), Header: make(http.Header)}, nil
		}
		if req.URL.String() == "https://music.apple.com/assets/index~abc123.js" {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`token:"eyJhbGciOiJFUzI1NiJ9.eyJmb28iOiJiYXIifQ.sig-part-here"`)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("unexpected: " + req.URL.RequestURI())), Header: make(http.Header)}, nil
	})}

	station, err := client.StationTracks(context.Background(), "us", "ra.1", "media-token-xyz")
	if err != nil {
		t.Fatalf("expected success after 401 retry, got %v", err)
	}
	if got, want := trackIDs(station.Tracks), []string{"song-1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("track IDs = %#v, want %#v", got, want)
	}
	if nextTracksCalls != 2 {
		t.Fatalf("next-tracks calls = %d, want 2 (401 then retry)", nextTracksCalls)
	}
}
