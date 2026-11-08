package media

import (
	"context"
	"errors"
	"slices"
	"testing"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/usertoken"
)

const inProcessTestToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// recordingPlayback is a playbackClient that records the requests it got and
// answers with the configured results.
type recordingPlayback struct {
	calls         *callLog
	storefront    string
	storefrontErr error
	lyrics        string
	lyricsErr     error
	webPlayback   string
	webErr        error
	license       string
	licenseErr    error
	// lyricsArgs receives the storefront and language SongLyrics was given.
	lyricsArgs *[2]string
}

func (p recordingPlayback) record(call, token string) {
	if token != inProcessTestToken {
		call += " with another token"
	}
	p.calls.add(call)
}

func (p recordingPlayback) AccountStorefront(_ context.Context, token string) (string, error) {
	p.record("storefront", token)
	return p.storefront, p.storefrontErr
}

func (p recordingPlayback) SongLyrics(_ context.Context, token, storefront, _, language string) (string, error) {
	p.record("lyrics", token)
	if p.lyricsArgs != nil {
		*p.lyricsArgs = [2]string{storefront, language}
	}
	return p.lyrics, p.lyricsErr
}

func (p recordingPlayback) WebPlayback(_ context.Context, token, _ string) (string, error) {
	p.record("webPlayback", token)
	return p.webPlayback, p.webErr
}

func (p recordingPlayback) License(_ context.Context, token, _, _, _ string) (string, error) {
	p.record("license", token)
	return p.license, p.licenseErr
}

type tokenRejected struct{}

func (tokenRejected) Error() string           { return "token rejected" }
func (tokenRejected) UserTokenRejected() bool { return true }
func (tokenRejected) NonRetryable() bool      { return true }

type notFound struct{}

func (notFound) Error() string      { return "404" }
func (notFound) NotFound() bool     { return true }
func (notFound) NonRetryable() bool { return true }

var errApple5xx = errors.New("Apple Music request failed: 503 Service Unavailable")

func inProcessDownloader(token string, playback playbackClient, calls *callLog) *Downloader {
	cfg := config.Default()
	cfg.Catalog.Language = "ja"
	return newTestDownloader(&Downloader{
		cfg:      cfg,
		tokens:   usertoken.NewReading(func(string) (string, error) { return token, nil }),
		playback: playback,
		wrapper: fakeDownloaderWrapper{
			lyrics: "wrapper lyrics doc", webPlayback: "https://wrapper.example/playlist.m3u8", license: "wrapper-license",
			calls: calls,
		},
	})
}

func TestInProcessRequestsPickTheirRoute(t *testing.T) {
	cases := []struct {
		name     string
		token    string
		playback func(*callLog) playbackClient
		want     []string
		// wantErr names the error webPlayback, license and lyrics return.
		wantErr error
	}{
		{
			name:  "no media-user-token goes to the wrapper",
			token: "",
			playback: func(calls *callLog) playbackClient {
				return recordingPlayback{calls: calls}
			},
			want: []string{"wrapper webPlayback", "wrapper license", "wrapper lyrics"},
		},
		{
			name:     "no in-process client goes to the wrapper",
			token:    inProcessTestToken,
			playback: func(*callLog) playbackClient { return nil },
			want:     []string{"wrapper webPlayback", "wrapper license", "wrapper lyrics"},
		},
		{
			name:  "a media-user-token asks Apple directly",
			token: inProcessTestToken,
			playback: func(calls *callLog) playbackClient {
				return recordingPlayback{calls: calls, storefront: "cn", lyrics: "<tt/>", webPlayback: "https://apple.example/p.m3u8", license: "apple-license"}
			},
			want: []string{"webPlayback", "license", "storefront", "lyrics"},
		},
		{
			name:  "a rejected token falls back to the wrapper",
			token: inProcessTestToken,
			playback: func(calls *callLog) playbackClient {
				return recordingPlayback{calls: calls, storefront: "cn", lyricsErr: tokenRejected{}, webErr: tokenRejected{}, licenseErr: tokenRejected{}}
			},
			want: []string{"webPlayback", "wrapper webPlayback", "license", "wrapper license", "storefront", "lyrics", "wrapper lyrics"},
		},
		{
			name:  "a transient Apple failure is returned for the pipeline's retries",
			token: inProcessTestToken,
			playback: func(calls *callLog) playbackClient {
				return recordingPlayback{calls: calls, storefront: "cn", lyricsErr: errApple5xx, webErr: errApple5xx, licenseErr: errApple5xx}
			},
			want:    []string{"webPlayback", "license", "storefront", "lyrics"},
			wantErr: errApple5xx,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := &callLog{}
			d := inProcessDownloader(tc.token, tc.playback(calls), calls)
			ctx := context.Background()
			if _, err := d.webPlaybackURL(ctx, "1"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("webPlaybackURL error = %v, want %v", err, tc.wantErr)
			}
			if _, err := d.aacLCLicense(ctx, "1", "Y2hhbGxlbmdl", "data:;base64,AAAA"); !errors.Is(err, tc.wantErr) {
				t.Fatalf("aacLCLicense error = %v, want %v", err, tc.wantErr)
			}
			if _, err := (realMedia{d: d}).lyrics(ctx, "1", lyricsRequest(d.cfg, true)); !errors.Is(err, tc.wantErr) {
				t.Fatalf("lyrics error = %v, want %v", err, tc.wantErr)
			}
			if !slices.Equal(calls.list(), tc.want) {
				t.Fatalf("requests = %q, want %q", calls.list(), tc.want)
			}
		})
	}
}

// Apple answers a rejected token on syllable-lyrics with the same 404 as a
// song without lyrics, so a 404, an empty answer or an unreadable account
// storefront all leave the lyrics to the wrapper.
func TestInProcessLyricsFallBackToTheWrapper(t *testing.T) {
	cases := []struct {
		name     string
		playback recordingPlayback
		want     []string
	}{
		{"not found", recordingPlayback{storefront: "cn", lyricsErr: notFound{}}, []string{"storefront", "lyrics", "wrapper lyrics"}},
		{"no document", recordingPlayback{storefront: "cn", lyricsErr: applemusic.ErrNoLyrics}, []string{"storefront", "lyrics", "wrapper lyrics"}},
		{"storefront unreadable", recordingPlayback{storefrontErr: errApple5xx}, []string{"storefront", "wrapper lyrics"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := &callLog{}
			tc.playback.calls = calls
			d := inProcessDownloader(inProcessTestToken, tc.playback, calls)
			doc, err := (realMedia{d: d}).lyrics(context.Background(), "1", lyricsRequest(d.cfg, true))
			if err != nil || doc != "wrapper lyrics doc" {
				t.Fatalf("lyrics = (%q, %v), want the wrapper's document", doc, err)
			}
			if !slices.Equal(calls.list(), tc.want) {
				t.Fatalf("requests = %q, want %q", calls.list(), tc.want)
			}
		})
	}
}

func TestInProcessLyricsUseTheAccountStorefrontAndCatalogLanguage(t *testing.T) {
	calls := &callLog{}
	var args [2]string
	d := inProcessDownloader(inProcessTestToken, recordingPlayback{calls: calls, storefront: "jp", lyrics: "<tt/>", lyricsArgs: &args}, calls)
	if _, err := (realMedia{d: d}).lyrics(context.Background(), "1", lyricsRequest(d.cfg, true)); err != nil {
		t.Fatal(err)
	}
	if args != [2]string{"jp", "ja"} {
		t.Fatalf("SongLyrics got storefront and language %q, want the account's jp and catalog.language ja", args)
	}
}

// The syllable=0 request only follows a wrapper 404 for the word-timed one,
// so it always goes to the wrapper.
func TestLineTimedLyricsRequestGoesToTheWrapper(t *testing.T) {
	calls := &callLog{}
	d := inProcessDownloader(inProcessTestToken, recordingPlayback{calls: calls, storefront: "cn", lyrics: "<tt/>"}, calls)
	if _, err := (realMedia{d: d}).lyrics(context.Background(), "1", lyricsRequest(d.cfg, false)); err != nil {
		t.Fatal(err)
	}
	if want := []string{"wrapper lyrics"}; !slices.Equal(calls.list(), want) {
		t.Fatalf("requests = %q, want %q", calls.list(), want)
	}
}
