package applemusic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// MotionArtworkViaWebToken fetches an album's animated cover loops.
//
// Same shape as EnhancedHLSViaWebToken and for the same reason: editorialVideo
// is not available to third-party developer tokens at all. Apple states this
// outright — the only extended attribute a third party may load on Albums is
// artistUrl. Verified empirically: a self-signed token returns no editorialVideo
// on any storefront, and even the web-player token returns none from the public
// api.music.apple.com host. Both the amp-api host and a scraped web-player JWT
// are required, so this always goes through doWithWebAuth regardless of whether
// developer-token signing is enabled.
//
// amp-api is Apple's internal web-player endpoint, not a documented API. Treat a
// failure here as "this album has no motion artwork", never as a job error.
func (c *CatalogClient) MotionArtworkViaWebToken(ctx context.Context, storefront, id string) (MotionArtwork, error) {
	params := url.Values{
		"extend": []string{"editorialVideo"},
		"l":      []string{c.cfg.Language},
	}
	endpoint := fmt.Sprintf("https://amp-api.music.apple.com/v1/catalog/%s/albums/%s?%s", storefront, id, params.Encode())
	resp, err := c.doWithWebAuth(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "https://music.apple.com")
		req.Header.Set("User-Agent", "Mozilla/5.0")
		return req, nil
	})
	if err != nil {
		return MotionArtwork{}, fmt.Errorf("fetch motion artwork with web token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return MotionArtwork{}, fmt.Errorf("catalog request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out catalogMotionArtworkResponse
	if err := decodeJSONLimited(resp.Body, maxCatalogResponseBytes, &out); err != nil {
		return MotionArtwork{}, err
	}
	if len(out.Data) == 0 {
		return MotionArtwork{}, fmt.Errorf("album %s not found", id)
	}
	video := out.Data[0].Attributes.EditorialVideo
	// Most albums expose the motionDetail* pair; a few only carry the
	// motion*Video* aliases, which hold the same asset.
	square := pickMotionClip(video.MotionDetailSquare, video.MotionSquareVideo1x1)
	tall := pickMotionClip(video.MotionDetailTall, video.MotionTallVideo3x4)
	return MotionArtwork{
		Square:       square.Video,
		Tall:         tall.Video,
		SquareColors: square.PreviewFrame.colors(),
		TallColors:   tall.PreviewFrame.colors(),
	}, nil
}

// pickMotionClip returns the first clip that actually has a video, so the
// palette stays attached to the asset it describes.
func pickMotionClip(clips ...motionArtworkClip) motionArtworkClip {
	for _, clip := range clips {
		if clip.Video != "" {
			return clip
		}
	}
	return motionArtworkClip{}
}

// EnhancedHLSViaWebToken fetches a song's Enhanced HLS master playlist URL
// from amp-api.music.apple.com using a scraped music.apple.com web-player
// JWT, regardless of whether developer-token signing is enabled. It is used
// when signing is enabled but catalog.signed_mode_hls_source is "web_token":
// the self-signed developer token cannot read enhancedHls at all, so this
// reads it through the same legacy-style web token as unsigned mode while
// catalog metadata itself keeps using the official signed-token endpoint.
func (c *CatalogClient) EnhancedHLSViaWebToken(ctx context.Context, storefront, id string) (string, error) {
	params := url.Values{
		"extend": []string{"extendedAssetUrls"},
		"l":      []string{c.cfg.Language},
	}
	endpoint := fmt.Sprintf("https://amp-api.music.apple.com/v1/catalog/%s/songs/%s?%s", storefront, id, params.Encode())
	resp, err := c.doWithWebAuth(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", "https://music.apple.com")
		req.Header.Set("User-Agent", "Mozilla/5.0")
		return req, nil
	})
	if err != nil {
		return "", fmt.Errorf("fetch enhanced hls with web token: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("catalog request failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out catalogSongResponse
	if err := decodeJSONLimited(resp.Body, maxCatalogResponseBytes, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", fmt.Errorf("song %s not found", id)
	}
	master := out.Data[0].Attributes.ExtendedAssetURLs.EnhancedHLS
	if master == "" {
		return "", fmt.Errorf("song %s has no enhanced hls manifest", id)
	}
	return master, nil
}
