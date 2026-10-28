package applemusic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

func (c *CatalogClient) Cover(ctx context.Context, artworkURL, format, size string) ([]byte, error) {
	return c.FetchCover(ctx, []string{artworkURL}, format, size)
}

func (c *CatalogClient) FetchCover(ctx context.Context, artworkURLs []string, format, size string) ([]byte, error) {
	if format == "" {
		format = "jpg"
	}
	var lastErr error
	for _, artworkURL := range artworkURLs {
		if artworkURL == "" {
			continue
		}
		for _, coverSize := range coverSizeFallbacks(size) {
			data, err := c.fetchCoverOnce(ctx, artworkURL, format, coverSize)
			if err == nil && len(data) > 0 {
				return data, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		return nil, fmt.Errorf("no artwork url available")
	}
	return nil, lastErr
}

func coverSizeFallbacks(preferred string) []string {
	defaults := []string{"5000x5000", "3000x3000", "1400x1400", "600x600", "300x300"}
	if preferred == "" {
		return defaults
	}
	out := []string{preferred}
	for _, size := range defaults {
		if size != preferred {
			out = append(out, size)
		}
	}
	return out
}

func formatArtworkURL(artworkURL, format, size string) string {
	u := artworkURL
	if format != "" && format != "jpg" && strings.Contains(u, "bb.jpg") {
		u = strings.Replace(u, "bb.jpg", "bb."+format, 1)
	}
	u = strings.ReplaceAll(u, "{w}x{h}", size)
	u = strings.ReplaceAll(u, "{f}", format)
	return u
}

func (c *CatalogClient) fetchCoverOnce(ctx context.Context, artworkURL, format, size string) ([]byte, error) {
	if artworkURL == "" {
		return nil, fmt.Errorf("empty artwork url")
	}
	u := formatArtworkURL(artworkURL, format, size)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://music.apple.com/")
	resp, err := c.doAppleRequest(ctx, req, false)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cover request failed: %s", resp.Status)
	}
	return readLimited(resp.Body, maxArtworkBytes)
}
