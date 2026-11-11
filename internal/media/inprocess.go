package media

import (
	"context"
	"errors"

	"amdl/internal/applemusic"
	"amdl/internal/logging"
)

// playbackClient sends the lyrics, WebPlayback and license requests that
// wrapper-lite would otherwise make for us with the account's
// media-user-token (applemusic.CatalogClient). The Downloader uses it whenever
// it can read a media-user-token (internal/usertoken, afresh on every
// request), and the wrapper when it cannot, as before. These requests take
// the Apple request gate, not the wrapper's permits.
//
// A failed in-process request goes to the wrapper only when Apple refused the
// token (applemusic.IsUserTokenRejected): amdl's copy may be older than the
// token the wrapper has in memory, which still works. Lyrics also go
// to the wrapper on a 404, because Apple answers a rejected token there with
// the same 404 as a song without lyrics, and on an account storefront that
// cannot be read. Every other failure (Apple 5xx, 429, a timeout) is returned
// for the pipeline's retries: the wrapper would meet it too.
type playbackClient interface {
	AccountStorefront(ctx context.Context, mediaUserToken string) (string, error)
	SongLyrics(ctx context.Context, mediaUserToken, storefront, id, language string) (string, error)
	WebPlayback(ctx context.Context, mediaUserToken, adamID string) (string, error)
	License(ctx context.Context, mediaUserToken, adamID, challenge, uri string) (string, error)
}

// inProcessToken is the media-user-token for an in-process request, or ""
// when the request goes to the wrapper.
func (d *Downloader) inProcessToken() string {
	if d.playback == nil {
		return ""
	}
	return d.mediaUserToken()
}

// lyricsInProcess fetches songID's lyrics document from Apple. handled is
// false when the wrapper should be asked instead.
func (d *Downloader) lyricsInProcess(ctx context.Context, songID, language string) (doc string, handled bool, err error) {
	token := d.inProcessToken()
	if token == "" {
		return "", false, nil
	}
	storefront, err := d.playback.AccountStorefront(ctx, token)
	if err != nil {
		if ctx.Err() != nil {
			return "", true, ctx.Err()
		}
		d.noteWrapperFallback(ctx, "lyrics", songID, err)
		return "", false, nil
	}
	doc, err = d.playback.SongLyrics(ctx, token, storefront, songID, language)
	switch {
	case err == nil:
		return doc, true, nil
	case applemusic.IsNotFound(err), errors.Is(err, applemusic.ErrNoLyrics), applemusic.IsUserTokenRejected(err):
		d.noteWrapperFallback(ctx, "lyrics", songID, err)
		return "", false, nil
	default:
		return "", true, err
	}
}

// webPlaybackURL returns songID's AAC-LC WebPlayback playlist URL.
func (d *Downloader) webPlaybackURL(ctx context.Context, songID string) (string, error) {
	if token := d.inProcessToken(); token != "" {
		playlistURL, err := d.playback.WebPlayback(ctx, token, songID)
		if err == nil || !applemusic.IsUserTokenRejected(err) {
			return playlistURL, err
		}
		d.noteWrapperFallback(ctx, "webPlayback", songID, err)
	}
	return d.wrapper.WebPlayback(ctx, songID)
}

// aacLCLicense trades a base64 Widevine challenge for songID's base64
// license.
func (d *Downloader) aacLCLicense(ctx context.Context, songID, challenge, keyURI string) (string, error) {
	if token := d.inProcessToken(); token != "" {
		license, err := d.playback.License(ctx, token, songID, challenge, keyURI)
		if err == nil || !applemusic.IsUserTokenRejected(err) {
			return license, err
		}
		d.noteWrapperFallback(ctx, "license", songID, err)
	}
	return d.wrapper.License(ctx, songID, challenge, keyURI)
}

// noteWrapperFallback logs an in-process request handed to the wrapper, and
// when Apple refused the token asks the token provider to fetch the wrapper's
// again. err comes from applemusic, whose messages never carry a token.
func (d *Downloader) noteWrapperFallback(ctx context.Context, request, songID string, err error) {
	if applemusic.IsUserTokenRejected(err) {
		d.tokens.NoteRejected(d.inProcessToken())
		d.tokens.RefreshSoon()
	}
	logging.FromContext(ctx, d.logger).Info("in-process request not answered; asking the wrapper",
		"request", request, "song_id", songID, "error", err)
}
