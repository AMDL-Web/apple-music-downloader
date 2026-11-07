package media

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"amdl/internal/applemusic"
	"amdl/internal/config"
	"amdl/internal/jobs"
	"amdl/internal/limits"
	"amdl/internal/logging"
	"amdl/internal/usertoken"
	"amdl/internal/wrapper"
)

// Downloader runs the per-track pipeline for the job manager. One instance is
// shared by every job; each job works on a withConfig clone bound to its
// effective config.
type Downloader struct {
	// store is the live runtime config; every ProcessJob/ValidateRequest call
	// reads a fresh snapshot from it. cfg is the effective config the current
	// operation runs under (snapshot plus the job's overrides — see
	// withConfig); it doubles as the fallback when store is nil in unit tests
	// that build a Downloader literal around a fixed config.
	store *config.Store
	cfg   config.Config
	// tokens picks the media-user-token for playlists, stations and the
	// in-process playback requests; nil has none.
	tokens *usertoken.Provider
	// storefront is the storefront a job-scoped clone reads in: the token
	// of the account that serves it is used (usertoken.Provider.TokenFor).
	// Empty uses the default account's.
	storefront string
	catalog    Catalog
	// playback sends lyrics, WebPlayback and license requests to Apple
	// itself when there is a media-user-token (see playbackClient); nil
	// leaves all of them to the wrapper.
	playback playbackClient
	wrapper  Wrapper
	tools    *ToolChecker
	http     *http.Client
	mp4      *MP4Processor
	covers   *coverCache
	logger   *slog.Logger
	// These limiters are created once by NewDownloader and deliberately remain
	// shared by every shallow withConfig clone, so limits apply across jobs.
	downloadLimit *limits.Semaphore
	decryptLimit  *limits.Semaphore
	inFlightLimit *limits.Semaphore
	requestGate   *limits.RequestGate
	// deviceManifests is shared like the limiters above: a manifest one job
	// asked the wrapper for serves every other job.
	deviceManifests *deviceManifestCache
	// simulatedOutputs is shared like the limiters above: what one simulated
	// job committed counts as on disk for every later job.
	simulatedOutputs *outputSet

	// Per-job suppression for standalone cover paths that were already handled
	// (written, unavailable, or exhausted). The state has its own mutex because
	// different cover paths are allowed to proceed concurrently.
	standaloneCoverState *standaloneCoverState

	// background tracks work a job starts but does not wait for (the
	// motion-artwork backfill). A pointer, so every withConfig clone adds to
	// the one Wait drains.
	background *sync.WaitGroup
}

// Catalog is the Apple Music catalog a Downloader reads metadata and
// artwork from; *applemusic.CatalogClient is the production one.
type Catalog interface {
	SongMetadata(context.Context, string, string) (applemusic.Song, error)
	Album(context.Context, string, string) (applemusic.Collection, error)
	Playlist(context.Context, string, string, string) (applemusic.Collection, error)
	StationTracks(context.Context, string, string, string) (applemusic.Collection, error)
	ArtistAlbums(context.Context, string, string) (applemusic.ArtistAlbums, error)
	Artist(context.Context, string, string) (applemusic.Artist, error)
	FetchCover(context.Context, []string, string, string) ([]byte, error)
	MotionArtworkViaWebToken(context.Context, string, string) (applemusic.MotionArtwork, error)
}

// Wrapper is the decryptor a Downloader asks for manifests, lyrics, keys and
// decryption; *wrapper.Client is the production one.
type Wrapper interface {
	Status(context.Context) (wrapper.Status, error)
	M3U8(context.Context, string) (string, error)
	Lyrics(context.Context, string, wrapper.LyricsRequestOptions) (string, error)
	WebPlayback(context.Context, string) (string, error)
	NewDecryptSession(context.Context, string) (wrapper.DecryptSession, error)
	PrefetchKeys(context.Context, string, []string) error
	PrepareKeys(context.Context, string, []string) error
	License(context.Context, string, string, string) (string, error)
}

// NewDownloader builds the process-wide Downloader. It creates the shared media
// pools from the current config; changing their sizes needs a restart.
func NewDownloader(store *config.Store, tokens *usertoken.Provider, catalog *applemusic.CatalogClient, wrapperClient *wrapper.Client, tools *ToolChecker, logger *slog.Logger) *Downloader {
	return NewDownloaderFrom(store, tokens, catalog, wrapperClient, tools, logger)
}

// NewDownloaderFrom is NewDownloader over any Catalog and Wrapper, so tests
// can run the real pipeline against fakes. A catalog that also answers
// Apple's playback requests (as *applemusic.CatalogClient does) serves the
// in-process lyrics/WebPlayback/license path, and one with a RequestGate
// shares it with the media requests.
func NewDownloaderFrom(store *config.Store, tokens *usertoken.Provider, catalog Catalog, wrapperClient Wrapper, tools *ToolChecker, logger *slog.Logger) *Downloader {
	cfg := store.Get()
	d := &Downloader{
		store: store, cfg: cfg, tokens: tokens, catalog: catalog, wrapper: wrapperClient, tools: tools,
		http: newHTTPClient(), mp4: newMP4Processor(cfg), logger: logger,
		downloadLimit:    limits.NewSemaphore(cfg.Download.MaxParallelDownloads),
		decryptLimit:     limits.NewSemaphore(config.MaxParallelDecrypts),
		inFlightLimit:    limits.NewSemaphore(cfg.Download.MaxParallelDownloads + config.MaxParallelDecrypts),
		deviceManifests:  newDeviceManifestCache(),
		simulatedOutputs: newOutputSet(),
		background:       &sync.WaitGroup{},
	}
	if playback, ok := catalog.(playbackClient); ok {
		d.playback = playback
	}
	if gated, ok := catalog.(interface{ RequestGate() *limits.RequestGate }); ok {
		d.requestGate = gated.RequestGate()
	}
	return d
}

// Wait blocks until the background work jobs started has finished, or ctx is
// done. Call it after the job manager has stopped, so no job starts more.
func (d *Downloader) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		d.background.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("wait for background downloader work: %w", ctx.Err())
	}
}

// baseConfig returns the current runtime config, falling back to the fixed
// cfg field for test Downloaders constructed without a store.
func (d *Downloader) baseConfig() config.Config {
	if d.store != nil {
		return d.store.Get()
	}
	return d.cfg
}

// withConfig returns a shallow copy of d bound to cfg, so one operation's
// effective config (runtime snapshot plus per-job overrides) never leaks into
// other jobs running concurrently on the shared Downloader. The MP4 processor
// is rebuilt because it captures config (temp dir, embed_cover) itself.
func (d *Downloader) withConfig(cfg config.Config) *Downloader {
	clone := *d
	clone.cfg = cfg
	clone.mp4 = newMP4Processor(cfg)
	clone.covers = nil
	clone.standaloneCoverState = nil
	return &clone
}

// ValidateRequest implements jobs.Processor: it parses url and checks that the
// wrapper serves its storefront (skipped in simulate mode).
func (d *Downloader) ValidateRequest(ctx context.Context, url string) (jobs.ValidationResult, error) {
	return d.withConfig(d.baseConfig()).validateRequest(ctx, url)
}

func (d *Downloader) validateRequest(ctx context.Context, url string) (jobs.ValidationResult, error) {
	parsed, err := applemusic.ParseWithAlbumTrackMode(url, d.cfg.Catalog.AlbumTrackURLMode)
	if err != nil {
		if strings.Contains(err.Error(), "album_track_url_mode") {
			return jobs.ValidationResult{}, &jobs.RequestError{Code: "invalid_configuration", Message: err.Error(), Cause: err}
		}
		return jobs.ValidationResult{}, &jobs.RequestError{Code: "invalid_url", Message: err.Error(), Cause: err}
	}
	if parsed.Type == applemusic.TypeVideo {
		message := fmt.Sprintf("%s download is not implemented", parsed.Type)
		return jobs.ValidationResult{}, &jobs.RequestError{Code: "unsupported_input", Message: message}
	}
	if storefront, which, ok := d.requestStorefront(ctx, parsed.Type, parsed.Storefront); ok {
		if err := d.validateStorefront(ctx, storefront, which); err != nil {
			return jobs.ValidationResult{}, err
		}
	}
	result := jobs.ValidationResult{Type: string(parsed.Type), Storefront: parsed.Storefront, ID: parsed.ID}
	if parsed.Type == applemusic.TypeSong && parsed.TrackID != "" {
		// An album-track link names the song's album: read it in album mode.
		if album, err := applemusic.ParseWithAlbumTrackMode(url, "album"); err == nil {
			result.AlbumID = album.ID
		}
	}
	return result, nil
}

// requestStorefront is the storefront a submitted link's run reads in: for
// a record the link's when an account serves it (the record is pinned to
// it), else the default account's; the link's for a station, which plays in
// it. An unreadable account storefront skips the check; the run reads it
// again and fails there.
func (d *Downloader) requestStorefront(ctx context.Context, typ applemusic.URLType, linkStorefront string) (string, string, bool) {
	if typ == applemusic.TypeStation {
		return linkStorefront, "the link's storefront", true
	}
	if d.cfg.Simulate.Enabled {
		return "", "", false
	}
	if d.tokens.Serves(linkStorefront) {
		return strings.ToLower(linkStorefront), "the link's storefront", true
	}
	account, err := d.AccountStorefront(ctx)
	if err != nil {
		return "", "", false
	}
	return account, "the account storefront", true
}

// How long a run waits for a wrapper that does not answer or is not ready
// yet, as when the backend starts it with runs to recover, and how often it
// asks. Tests run the wait on synctest's virtual clock.
const (
	wrapperStartWait = 2 * time.Minute
	wrapperStartPoll = 2 * time.Second
)

// awaitWrapper holds a run until the wrapper answers ready, for at most
// wrapperStartWait or until ctx ends (a pause, a cancel, shutdown); the
// run's validateStorefront then says what still stands in the way. A ready
// wrapper whose account is logged out, or serves another storefront, is not
// waited for: nothing would change by itself.
func (d *Downloader) awaitWrapper(ctx context.Context) {
	if d.cfg.Simulate.Enabled {
		return
	}
	deadline := time.Now().Add(wrapperStartWait)
	logged := false
	for {
		status, err := d.wrapper.Status(ctx)
		if err == nil && status.Ready {
			return
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return
		}
		if !logged {
			logged = true
			logging.FromContext(ctx, d.logger).Info("waiting for the wrapper before the run starts", "error", err)
		}
		timer := time.NewTimer(wrapperStartPoll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// validateStorefront checks that the wrapper's account serves storefront,
// which a run reads in; which names it in the error. A mismatch is
// decryptor_unavailable naming both.
// signedOut is the Apple ID (else the id) of an account of storefront that
// the wrapper lists but no longer serves, its Apple session gone; such an
// instance reports no region, so the account's own is the one discovery
// stored (internal/accounts#Service.List says the same).
func (d *Downloader) signedOut(status wrapper.Status, storefront string) (string, bool) {
	for _, a := range d.tokens.Accounts() {
		if !a.Active || !strings.EqualFold(a.Region, storefront) {
			continue
		}
		for _, in := range status.Instances {
			if in.ID == a.ID && !in.Ready && in.Region == "" {
				return cmp.Or(in.Name, in.ID), true
			}
		}
	}
	return "", false
}

func (d *Downloader) validateStorefront(ctx context.Context, storefront, which string) error {
	if d.cfg.Simulate.Enabled {
		// Test mode never decrypts, so it must not depend on a running
		// wrapper or its supported-storefront list.
		return nil
	}
	status, err := d.wrapper.Status(ctx)
	if err != nil {
		message := fmt.Sprintf("cannot reach the wrapper: %v", err)
		return &jobs.RequestError{Code: "decryptor_unavailable", Message: message, Cause: err}
	}
	if len(status.Regions) == 0 && (!status.Ready || !status.LoggedIn) {
		return &jobs.RequestError{Code: "decryptor_unavailable", Message: "the wrapper is not ready"}
	}
	for _, region := range status.Regions {
		if strings.EqualFold(region, storefront) {
			if !status.Ready || !status.LoggedIn {
				return &jobs.RequestError{Code: "decryptor_unavailable", Message: "the wrapper is not ready"}
			}
			return nil
		}
	}
	message := fmt.Sprintf("%s %q is not served by the wrapper's account (it serves: %s)", which, storefront, strings.Join(status.Regions, ", "))
	if name, ok := d.signedOut(status, storefront); ok {
		message = fmt.Sprintf("%s %q is the region of account %s, whose Apple session stopped working in the wrapper; log it in again (the wrapper serves: %s)",
			which, storefront, name, strings.Join(status.Regions, ", "))
	}
	return &jobs.RequestError{
		Code: "decryptor_unavailable", Message: message, Storefront: storefront,
		SupportedStorefronts: append([]string(nil), status.Regions...),
	}
}
