// Package applemusic parses Apple Music links (ParseWithAlbumTrackMode) and
// reads catalog, artwork and library metadata through CatalogClient, built
// once by NewCatalogClient: Song, Album, Playlist, StationTracks, ArtistAlbums,
// FetchCover, LibrarySongsNewestFirst, LibraryAlbumCatalogURL. EnrichWithAlbum
// merges album-level fields into a song for every caller. Startup calls
// InitDeveloperToken and StartTokenRefresher once.
//
// Invariants: apiBase picks api.music.apple.com when developer-token signing is
// enabled, amp-api.music.apple.com otherwise; MotionArtworkViaWebToken and
// EnhancedHLSViaWebToken always use amp-api with a scraped web-player JWT, and a
// MotionArtworkViaWebToken error means "no motion artwork", never a job failure.
// The catalog token and the web-player JWT live in two separate tokenCache
// values, each trusts a token for half its TTL, and doWithAuth drops only the
// rejected token on a 401 and retries once. Every Apple request goes through
// doAppleRequest and the client's one RequestGate, and upstream bodies are
// always read through a size cap. Every non-2xx JSON answer becomes a
// catalogRequestError (NonRetryable for a 4xx, RetryDelay from Retry-After), and
// a song without an Enhanced HLS manifest is ErrNoEnhancedHLS, so retry helpers
// stop at once on deterministic failures.
package applemusic
