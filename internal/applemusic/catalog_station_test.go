package applemusic

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

const stationTracksBody = `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"My Station","isLive":false,"artwork":{"url":"station-art"},"playParams":{"format":"tracks"}}}]}`

func TestStationTracksResolvesTracksFormat(t *testing.T) {
	var gotMediaToken, nextTracksMethod, nextTracksLimit string
	nextTracks := fixture(t, "catalog/station-next-tracks.json")
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		switch req.URL.Path {
		case "/v1/catalog/us/stations/ra.1":
			return jsonResponse(http.StatusOK, stationTracksBody)
		case "/v1/me/stations/next-tracks/ra.1":
			gotMediaToken = req.Header.Get("Media-User-Token")
			nextTracksMethod = req.Method
			nextTracksLimit = req.URL.Query().Get("limit")
			return jsonResponse(http.StatusOK, nextTracks)
		}
		return jsonResponse(http.StatusNotFound, "unexpected path: "+req.URL.RequestURI())
	})

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
	// The limit is how many songs a station job captures; changing it changes
	// what every station download resolves to.
	if nextTracksLimit != "10" {
		t.Fatalf("next-tracks limit = %q, want 10", nextTracksLimit)
	}
}

// A live station and a missing media-user-token are both refused from the
// station resource alone: neither may reach next-tracks.
func TestStationTracksRefusesBeforeNextTracks(t *testing.T) {
	tests := []struct {
		name, id, station, mediaToken string
	}{
		{"live stream", "ra.live", `{"data":[{"id":"ra.live","type":"stations","attributes":{"name":"Apple Music 1","isLive":true,"playParams":{"format":"stream"}}}]}`, "media-token-xyz"},
		{"missing media-user-token", "ra.1", `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"My Station","playParams":{"format":"tracks"}}}]}`, "  "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
				if req.URL.Path == "/v1/catalog/us/stations/"+tt.id {
					return jsonResponse(http.StatusOK, tt.station)
				}
				t.Errorf("unexpected request to %s", req.URL.RequestURI())
				return jsonResponse(http.StatusTeapot, "unexpected")
			})
			if _, err := client.StationTracks(context.Background(), "us", tt.id, tt.mediaToken); err == nil {
				t.Fatal("StationTracks succeeded, want a refusal")
			}
		})
	}
}

func TestStationNextTracksRetriesOnce401ThenSucceeds(t *testing.T) {
	var nextTracksCalls int
	client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
		if resp, ok := scrapeResponse(req); ok {
			return resp
		}
		switch req.URL.Path {
		case "/v1/catalog/us/stations/ra.1":
			return jsonResponse(http.StatusOK, stationTracksBody)
		case "/v1/me/stations/next-tracks/ra.1":
			nextTracksCalls++
			if nextTracksCalls == 1 {
				return jsonResponse(http.StatusUnauthorized, `{"errors":[{"status":"401"}]}`)
			}
			return jsonResponse(http.StatusOK, `{"data":[{"id":"song-1","type":"songs","attributes":{"name":"One","artistName":"Artist"}}]}`)
		}
		return jsonResponse(http.StatusNotFound, "unexpected: "+req.URL.RequestURI())
	})

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
