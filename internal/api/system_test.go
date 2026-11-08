package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"

	"amdl/internal/db"
	"amdl/internal/media"
)

func TestHealthEndpoint(t *testing.T) {
	closed, err := db.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		store      *db.Store
		wantStatus int
		wantBody   string
	}{
		{"store answers", nil, http.StatusOK, "ok"},
		{"store closed", closed, http.StatusServiceUnavailable, "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Health never asks the wrapper: a slow wrapper must not fail the
			// container's health check.
			fake := &fakeWrapperService{statusErr: errors.New("should not be called")}
			server := newServer(t, Deps{Store: tc.store, Wrapper: fake})
			recorder := requestJSON(t, server.Routes(), http.MethodGet, "/api/v1/health", "")
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			var body struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Status != tc.wantBody {
				t.Fatalf("body = %s, want status %q", recorder.Body.String(), tc.wantBody)
			}
			if fake.statusCalls != 0 {
				t.Fatalf("health called wrapper.Status %d times", fake.statusCalls)
			}
		})
	}
}

// qualitySlots is what media.QueryQuality returns for a track: every quality
// slot in the fixed codec order, here with only first available.
func qualitySlots(first media.QualityOption) []media.QualityOption {
	out := []media.QualityOption{first}
	for _, id := range []string{"aac", "aac-binaural", "aac-downmix", "ec3", "ac3"} {
		out = append(out, media.QualityOption{ID: id, Label: id})
	}
	return out
}

func TestQualityEndpointReturnsTracks(t *testing.T) {
	for _, tc := range []struct {
		name, input, typ, adamID string
		song                     media.QualitySong
		first                    media.QualityOption
	}{
		{
			name: "song", input: "https://music.apple.com/cn/album/example/1?i=2", typ: "song", adamID: "2",
			song:  media.QualitySong{ID: "2", Name: "One Last Kiss", Artist: "Hikaru Utada", Album: "One Last Kiss", HasLyrics: true},
			first: media.QualityOption{ID: "alac", Label: "Lossless", Available: true, CodecID: "audio-alac-stereo-44100-16", BitDepth: 16, SampleRate: 44100},
		},
		{
			name: "album", input: "https://music.apple.com/cn/album/example/1", typ: "album", adamID: "1",
			song:  media.QualitySong{ID: "2", Name: "One Last Kiss", Artist: "Hikaru Utada", Album: "One Last Kiss"},
			first: media.QualityOption{ID: "alac", Label: "Lossless", Available: true},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := media.QualityResult{
				Input: tc.input, Storefront: "cn", Type: tc.typ, AdamID: tc.adamID,
				Tracks: []media.QualityTrack{{Song: tc.song, Qualities: qualitySlots(tc.first)}},
			}
			fake := &fakeQualityService{result: want}
			server := newServer(t, Deps{Quality: fake})
			// The URL is trimmed before it reaches the service.
			recorder := requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/quality", `{"url":" `+tc.input+` "}`)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if fake.req.URL != tc.input {
				t.Fatalf("quality request URL = %q, want %q", fake.req.URL, tc.input)
			}
			var got media.QualityResult
			if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("body = %+v, want %+v", got, want)
			}
		})
	}
}

func TestQualityEndpointErrors(t *testing.T) {
	const body = `{"url":"https://music.apple.com/us/album/x/1"}`
	for _, tc := range []struct {
		name   string
		body   string
		err    error
		status int
		code   ErrorCode
	}{
		{"blank url", `{"url":" "}`, nil, http.StatusBadRequest, CodeInvalidRequest},
		{"upstream failure", body, errors.New("catalog lookup failed"), http.StatusBadGateway, CodeUpstreamError},
		{"not in the catalog", body, fmt.Errorf("resolve album: %w", notFoundError{}), http.StatusNotFound, CodeCatalogNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := newServer(t, Deps{Quality: &fakeQualityService{err: tc.err}})
			assertErrorCode(t, requestJSON(t, server.Routes(), http.MethodPost, "/api/v1/quality", tc.body), tc.status, tc.code)
		})
	}
}
