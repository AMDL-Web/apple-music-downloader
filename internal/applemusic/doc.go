// Package applemusic parses Apple Music links (ParseWithAlbumTrackMode) and
// reads catalog, artwork and library metadata through CatalogClient, built
// once by NewCatalogClient: Song, Album, Playlist, StationTracks, ArtistAlbums,
// FetchCover, LibrarySongsNewestFirst, LibraryAlbumCatalogURL, and for browsing
// Search and ArtistAlbumsPage. EnrichWithAlbum
// merges album-level fields into a song for every caller. Startup runs
// RunTokenRefresher until shutdown.
//
// Invariants: every request goes to amp-api.music.apple.com (ampAPIBase) with a
// JWT scraped from the music.apple.com web player and the Origin header it is
// scoped to; a MotionArtworkViaWebToken error means "no motion artwork", never a
// job failure. The scraped token lives in one tokenCache that trusts a token for
// half its TTL, and doWithWebAuth drops only the rejected token on a 401 and
// retries once. Every Apple request goes through doAppleRequest and the
// client's one RequestGate, and upstream bodies are
// always read through a size cap. Every non-2xx JSON answer becomes a
// catalogRequestError (NonRetryable for a 4xx, RetryDelay from Retry-After):
// its Error() is a short message that job rows and API responses can show,
// while the raw body stays on the value for log lines (logging.UpstreamBody).
// A song without an Enhanced HLS manifest is ErrNoEnhancedHLS, so retry helpers
// stop at once on deterministic failures.
package applemusic
