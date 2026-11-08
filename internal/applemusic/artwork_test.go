package applemusic

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"amdl/internal/config"
)

func TestFormatArtworkURL(t *testing.T) {
	const raw = "https://is1-ssl.mzstatic.com/image/thumb/foo/{w}x{h}bb.jpg"
	for _, tt := range []struct{ format, size, want string }{
		{"jpg", "1400x1400", "https://is1-ssl.mzstatic.com/image/thumb/foo/1400x1400bb.jpg"},
		{"png", "600x600", "https://is1-ssl.mzstatic.com/image/thumb/foo/600x600bb.png"},
	} {
		if got := formatArtworkURL(raw, tt.format, tt.size); got != tt.want {
			t.Errorf("formatArtworkURL(%q, %q) = %q, want %q", tt.format, tt.size, got, tt.want)
		}
	}
}

func TestCoverSizeFallbacks(t *testing.T) {
	got := coverSizeFallbacks("5000x5000")
	if len(got) < 2 || got[0] != "5000x5000" {
		t.Fatalf("coverSizeFallbacks() = %#v", got)
	}
}

func TestFetchCoverFallsBackToSmallerSize(t *testing.T) {
	var asked []string
	client := newTestClient(t, config.CatalogConfig{}, func(req *http.Request) *http.Response {
		asked = append(asked, req.URL.Path)
		switch {
		case strings.Contains(req.URL.Path, "5000x5000"):
			return jsonResponse(http.StatusForbidden, "")
		case strings.Contains(req.URL.Path, "1400x1400"):
			return jsonResponse(http.StatusOK, "cover-bytes")
		}
		return jsonResponse(http.StatusNotFound, "")
	})

	data, err := client.FetchCover(context.Background(), []string{"https://is1-ssl.mzstatic.com/image/thumb/foo/{w}x{h}bb.jpg"}, "jpg", "5000x5000")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cover-bytes" {
		t.Fatalf("FetchCover() = %q", data)
	}
	want := []string{"/image/thumb/foo/5000x5000bb.jpg", "/image/thumb/foo/3000x3000bb.jpg", "/image/thumb/foo/1400x1400bb.jpg"}
	if !slices.Equal(asked, want) {
		t.Fatalf("sizes asked = %v, want %v", asked, want)
	}
}
