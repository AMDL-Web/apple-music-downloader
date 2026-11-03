package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"amdl/internal/limits"
)

// M3U8 returns the device master playlist for adamID. It takes the manifest
// permit before the playback permit, so the /m3u8 requests queued behind it
// wait outside the playback queue and a needed /key never waits behind more
// than one. Keys fetched ahead of need queue in the same manifest lane, after
// the /m3u8 requests of their job (withKeyPermits). The wait for both permits
// is bounded as withPlaybackPermits describes.
func (c *Client) M3U8(ctx context.Context, adamID string) (string, error) {
	var body struct {
		M3U8 string `json:"m3u8"`
	}
	err := c.withPlaybackPermits(ctx, "/m3u8", []*limits.Semaphore{c.manifestLimit, c.playbackLimit}, func() error {
		return c.get(ctx, "/m3u8", url.Values{"adamId": {adamID}}, &body)
	})
	return body.M3U8, err
}

// Lyrics returns adamID's lyrics as wrapper-lite renders them for opts.
func (c *Client) Lyrics(ctx context.Context, adamID string, opts LyricsRequestOptions) (string, error) {
	query := url.Values{"adamId": {adamID}, "syllable": {"0"}}
	if opts.Syllable {
		query.Set("syllable", "1")
	}
	if opts.Language != "" {
		query.Set("language", opts.Language)
	}
	if opts.Script != "" {
		query.Set("script", opts.Script)
	}
	var body struct {
		Lyrics string `json:"lyrics"`
	}
	err := withPermit(ctx, c.dataLimit, func() error {
		return c.get(ctx, "/lyrics", query, &body)
	})
	return body.Lyrics, err
}

// WebPlayback returns the web-player playlist URL for adamID, which carries
// the AAC-LC rendition.
func (c *Client) WebPlayback(ctx context.Context, adamID string) (string, error) {
	var body struct {
		M3U8 string `json:"m3u8"`
	}
	err := withPermit(ctx, c.dataLimit, func() error {
		return c.get(ctx, "/webplayback", url.Values{"adamId": {adamID}}, &body)
	})
	return body.M3U8, err
}

// License exchanges a base64 Widevine challenge for a base64 license.
func (c *Client) License(ctx context.Context, adamID, challenge, uri string) (string, error) {
	var body struct {
		License string `json:"license"`
	}
	err := withPermit(ctx, c.dataLimit, func() error {
		data, err := c.call(ctx, http.MethodPost, "/license", nil, map[string]string{
			"adamId": adamID, "challenge": challenge, "uri": uri,
		})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return fmt.Errorf("%w: wrapper /license data: %v", ErrWrapperResponse, err)
		}
		return nil
	})
	if err == nil && body.License == "" {
		err = fmt.Errorf("%w: wrapper /license reply has no license", ErrWrapperResponse)
	}
	return body.License, err
}
