// Package applemusic parses Apple Music links (ParseWithAlbumTrackMode) and
// reads catalog, artwork and library metadata through CatalogClient, built
// once by NewCatalogClient: Song, Album, Playlist, StationTracks, ArtistAlbums,
// FetchCover, LibrarySongsNewestFirst. Startup calls InitDeveloperToken and
// StartTokenRefresher once.
//
// Invariants: apiBase picks api.music.apple.com when developer-token signing is
// enabled, amp-api.music.apple.com otherwise; MotionArtworkViaWebToken and
// EnhancedHLSViaWebToken always use amp-api with a scraped web-player JWT, and a
// MotionArtworkViaWebToken error means "no motion artwork", never a job failure.
// The developer-token and web-player JWT caches are separate, each trusts a
// token for half its TTL, and a 401 drops only the rejected token and retries
// once. Every Apple request goes through doAppleRequest and the client's one
// RequestGate, and upstream bodies are always read through a size cap.
package applemusic
