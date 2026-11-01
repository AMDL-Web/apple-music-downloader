package applemusic

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// The web player's playback hosts. They are not catalog reads: webPlayback
// names the AAC-LC rendition of one song and acquireWebPlaybackLicense trades
// a Widevine challenge for its license. Both take the scraped web JWT (Apple
// refuses the license to the Android developer token) and the
// media-user-token in X-Apple-Music-User-Token.
const (
	webPlaybackURL = "https://play.music.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"
	webLicenseURL  = "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense"
	playUserHeader = "X-Apple-Music-User-Token"
	// webAACLCFlavor is the asset flavor of the 256 kbps AAC-LC rendition,
	// read when an answer carries no hls-playlist-url.
	webAACLCFlavor = "28:ctrp256"
)

// ErrNoLyrics is a lyrics answer that holds neither a TTML document nor
// plain text.
var ErrNoLyrics error = nonRetryableError("Apple Music returned no lyrics")

// errNoWebPlaybackAsset is a webPlayback answer without an AAC-LC rendition.
var errNoWebPlaybackAsset error = nonRetryableError("Apple Music webPlayback returned no AAC-LC asset")

// playbackRejectedError is a play.* answer that carries a failureType instead
// of a result, with HTTP 200. Apple answers failureType 2002 to a missing,
// invalid or expired media-user-token.
type playbackRejectedError struct {
	endpoint    string
	failureType string
}

func (e playbackRejectedError) Error() string {
	return fmt.Sprintf("Apple Music %s refused the media-user-token (failureType %s)", e.endpoint, e.failureType)
}

// NonRetryable reports true: the same token gets the same answer.
func (playbackRejectedError) NonRetryable() bool { return true }

// UserTokenRejected reports true: Apple named the token as the problem.
func (playbackRejectedError) UserTokenRejected() bool { return true }

// UserTokenRejected reports an HTTP 401 or 403, which doWithWebAuth returns
// only after a freshly scraped web JWT was refused too.
func (e catalogRequestError) UserTokenRejected() bool {
	return e.statusCode == http.StatusUnauthorized || e.statusCode == http.StatusForbidden
}

// IsUserTokenRejected reports whether err, or an error it wraps, is Apple
// refusing the media-user-token a WebPlayback, License or SongLyrics request
// carried: HTTP 401 or 403, or a play.* failureType. Another token for the
// same account (the wrapper's own) may still be accepted.
func IsUserTokenRejected(err error) bool {
	var marker interface{ UserTokenRejected() bool }
	return errors.As(err, &marker) && marker.UserTokenRejected()
}

// WebPlayback returns the web player's playlist URL for song adamID, which
// carries its AAC-LC rendition, asking as the account mediaUserToken belongs
// to.
func (c *CatalogClient) WebPlayback(ctx context.Context, mediaUserToken, adamID string) (string, error) {
	body, err := json.Marshal(map[string]string{"salableAdamId": adamID})
	if err != nil {
		return "", err
	}
	var out struct {
		FailureType json.RawMessage `json:"failureType"`
		SongList    []struct {
			HLSPlaylistURL string `json:"hls-playlist-url"`
			Assets         []struct {
				Flavor string `json:"flavor"`
				URL    string `json:"URL"`
			} `json:"assets"`
		} `json:"songList"`
	}
	if err := c.postPlayback(ctx, webPlaybackURL, mediaUserToken, body, &out); err != nil {
		return "", fmt.Errorf("webPlayback: %w", err)
	}
	if failure := failureType(out.FailureType); failure != "" {
		return "", playbackRejectedError{endpoint: "webPlayback", failureType: failure}
	}
	if len(out.SongList) == 0 {
		return "", errNoWebPlaybackAsset
	}
	song := out.SongList[0]
	if song.HLSPlaylistURL != "" {
		return song.HLSPlaylistURL, nil
	}
	for _, asset := range song.Assets {
		if asset.Flavor == webAACLCFlavor && asset.URL != "" {
			return asset.URL, nil
		}
	}
	return "", errNoWebPlaybackAsset
}

// License trades a base64 Widevine challenge for the base64 license of key
// uri in song adamID, asking as the account mediaUserToken belongs to.
func (c *CatalogClient) License(ctx context.Context, mediaUserToken, adamID, challenge, uri string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"challenge":      challenge,
		"uri":            uri,
		"key-system":     "com.widevine.alpha",
		"adamId":         adamID,
		"isLibrary":      false,
		"user-initiated": true,
	})
	if err != nil {
		return "", err
	}
	var out struct {
		FailureType json.RawMessage `json:"failureType"`
		Status      int             `json:"status"`
		License     string          `json:"license"`
	}
	if err := c.postPlayback(ctx, webLicenseURL, mediaUserToken, body, &out); err != nil {
		return "", fmt.Errorf("acquireWebPlaybackLicense: %w", err)
	}
	if failure := failureType(out.FailureType); failure != "" {
		return "", playbackRejectedError{endpoint: "acquireWebPlaybackLicense", failureType: failure}
	}
	if out.License == "" {
		return "", fmt.Errorf("acquireWebPlaybackLicense: no license in the answer (status %d)", out.Status)
	}
	return out.License, nil
}

func (c *CatalogClient) postPlayback(ctx context.Context, endpoint, mediaUserToken string, body []byte, out any) error {
	resp, err := c.doWithWebAuth(ctx, appleRequest{
		method: http.MethodPost, url: endpoint, body: body,
		mediaUserToken: mediaUserToken, userTokenHeader: playUserHeader,
	})
	if err != nil {
		return err
	}
	return decodeCatalogResponse(resp, out)
}

// failureType reads a failureType that Apple sends as a string or a number.
func failureType(raw json.RawMessage) string {
	v := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if v == "null" {
		return ""
	}
	return v
}

// SongLyrics returns song id's lyrics in storefront, which must be the
// storefront of the account mediaUserToken belongs to (AccountStorefront):
// Apple's word-timed TTML from syllable-lyrics, with the translation in
// language and the romanization, or plain text when that is all Apple has. A
// song without lyrics, a token from another storefront and a rejected token
// all get Apple's 404 (IsNotFound); an answer with no document is ErrNoLyrics.
func (c *CatalogClient) SongLyrics(ctx context.Context, mediaUserToken, storefront, id, language string) (string, error) {
	if language == "" {
		language = "en"
	}
	params := url.Values{
		"l[lyrics]": {language},
		"extend":    {"ttmlLocalizations"},
		"l[script]": {"en-Latn"},
	}
	endpoint := fmt.Sprintf("%s/v1/catalog/%s/songs/%s/syllable-lyrics", ampAPIBase, storefront, id)
	var out struct {
		Data []struct {
			Attributes struct {
				TTMLLocalizations string `json:"ttmlLocalizations"`
				Text              string `json:"text"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.getWithUserToken(ctx, endpoint, params, mediaUserToken, &out); err != nil {
		return "", err
	}
	if len(out.Data) == 0 {
		return "", ErrNoLyrics
	}
	attrs := out.Data[0].Attributes
	if attrs.TTMLLocalizations != "" {
		return attrs.TTMLLocalizations, nil
	}
	if attrs.Text != "" {
		return attrs.Text, nil
	}
	return "", ErrNoLyrics
}

// AccountStorefront returns the storefront of the account mediaUserToken
// belongs to. The answer is kept for the last token asked about, so a
// refreshed token is read once more.
func (c *CatalogClient) AccountStorefront(ctx context.Context, mediaUserToken string) (string, error) {
	key := sha256.Sum256([]byte(mediaUserToken))
	c.accountMu.Lock()
	if c.accountKey == key && c.accountStorefront != "" {
		storefront := c.accountStorefront
		c.accountMu.Unlock()
		return storefront, nil
	}
	c.accountMu.Unlock()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := c.getWithUserToken(ctx, ampAPIBase+"/v1/me/storefront", nil, mediaUserToken, &out); err != nil {
		return "", fmt.Errorf("read the account storefront: %w", err)
	}
	if len(out.Data) == 0 || out.Data[0].ID == "" {
		return "", errors.New("read the account storefront: Apple named none")
	}
	storefront := out.Data[0].ID
	c.accountMu.Lock()
	c.accountKey, c.accountStorefront = key, storefront
	c.accountMu.Unlock()
	return storefront, nil
}
