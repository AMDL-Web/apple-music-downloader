package applemusic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ErrNoEnhancedHLS reports that the catalog lists no Enhanced HLS master
// playlist for a song, so its lossless and spatial variants cannot be read.
// It is deterministic: errors wrapping it report NonRetryable, and callers
// match it with errors.Is.
var ErrNoEnhancedHLS error = nonRetryableError("no enhanced hls manifest")

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
	resp, err := c.doWithWebAuth(ctx, appleRequest{method: http.MethodGet, url: endpoint})
	if err != nil {
		return MotionArtwork{}, fmt.Errorf("fetch motion artwork with web token: %w", err)
	}
	var out catalogMotionArtworkResponse
	if err := decodeCatalogResponse(resp, &out); err != nil {
		return MotionArtwork{}, fmt.Errorf("fetch motion artwork with web token: %w", err)
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
	resp, err := c.doWithWebAuth(ctx, appleRequest{method: http.MethodGet, url: endpoint})
	if err != nil {
		return "", fmt.Errorf("fetch enhanced hls with web token: %w", err)
	}
	var out catalogSongResponse
	if err := decodeCatalogResponse(resp, &out); err != nil {
		return "", fmt.Errorf("fetch enhanced hls with web token: %w", err)
	}
	if len(out.Data) == 0 {
		return "", fmt.Errorf("song %s not found", id)
	}
	master := out.Data[0].Attributes.ExtendedAssetURLs.EnhancedHLS
	if master == "" {
		return "", fmt.Errorf("song %s: %w", id, ErrNoEnhancedHLS)
	}
	return master, nil
}
