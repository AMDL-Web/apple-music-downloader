package applemusic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestAppleEndpointsClassifyRetries pins that the station and motion-artwork
// reads report the same retry classification as every other catalog read: a 4xx
// stops retries at once, a 5xx stays retryable and carries Apple's Retry-After.
func TestAppleEndpointsClassifyRetries(t *testing.T) {
	endpoints := []struct {
		name string
		path string // the request that answers with the status under test
		call func(*CatalogClient) error
	}{
		{
			name: "station next-tracks",
			path: "/v1/me/stations/next-tracks/ra.1",
			call: func(c *CatalogClient) error {
				_, err := c.StationTracks(context.Background(), "us", "ra.1", "media-token")
				return err
			},
		},
		{
			name: "motion artwork",
			path: "/v1/catalog/us/albums/album-1",
			call: func(c *CatalogClient) error {
				_, err := c.MotionArtworkViaWebToken(context.Background(), "us", "album-1")
				return err
			},
		},
	}
	statuses := []struct {
		code             int
		retryAfter       string
		wantNonRetryable bool
		wantDelay        time.Duration
	}{
		{code: http.StatusNotFound, wantNonRetryable: true},
		{code: http.StatusForbidden, wantNonRetryable: true},
		{code: http.StatusServiceUnavailable, retryAfter: "7", wantNonRetryable: false, wantDelay: 7 * time.Second},
	}
	for _, endpoint := range endpoints {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s %d", endpoint.name, status.code), func(t *testing.T) {
				var hits int
				client := newTestClient(t, enUS, func(req *http.Request) *http.Response {
					switch req.URL.Path {
					case endpoint.path:
						hits++
						resp := jsonResponse(status.code, `{"errors":[]}`)
						if status.retryAfter != "" {
							resp.Header.Set("Retry-After", status.retryAfter)
						}
						return resp
					case "/v1/catalog/us/stations/ra.1":
						return jsonResponse(http.StatusOK, `{"data":[{"id":"ra.1","type":"stations","attributes":{"name":"Station","playParams":{"format":"tracks"}}}]}`)
					}
					return jsonResponse(http.StatusTeapot, "unexpected "+req.URL.Path)
				})

				err := endpoint.call(client)
				if err == nil {
					t.Fatal("expected an error")
				}
				if hits != 1 {
					t.Fatalf("requests to %s = %d, want 1", endpoint.path, hits)
				}
				var marker interface{ NonRetryable() bool }
				if !errors.As(err, &marker) {
					t.Fatalf("error %v carries no retry classification", err)
				}
				if got := marker.NonRetryable(); got != status.wantNonRetryable {
					t.Fatalf("NonRetryable() = %v, want %v (error %v)", got, status.wantNonRetryable, err)
				}
				var hint interface{ RetryDelay() time.Duration }
				if !errors.As(err, &hint) || hint.RetryDelay() != status.wantDelay {
					t.Fatalf("RetryDelay = %v, want %v", hint, status.wantDelay)
				}
			})
		}
	}
}
