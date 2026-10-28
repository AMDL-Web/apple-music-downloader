package media

import (
	"context"
	"strings"
	"sync"

	"amdl/internal/applemusic"
)

type albumMetadataResult struct {
	done  chan struct{}
	album applemusic.Collection
	err   error
}

// trackMetadataResolver lives for one job. It keeps the intentional per-track
// song refresh while coalescing album enrichment shared by playlist tracks.
// Album and artist jobs already carry that enrichment in their resolved track
// values, so those paths never read the same album again here.
type trackMetadataResolver struct {
	downloader *Downloader
	storefront string

	mu     sync.Mutex
	albums map[string]*albumMetadataResult
}

func newTrackMetadataResolver(d *Downloader, storefront string) *trackMetadataResolver {
	return &trackMetadataResolver{
		downloader: d,
		storefront: storefront,
		albums:     make(map[string]*albumMetadataResult),
	}
}

func (r *trackMetadataResolver) song(ctx context.Context, initial applemusic.Song, collectionType applemusic.URLType) (applemusic.Song, error) {
	// A single-song resolve already used CatalogClient.Song, including its album
	// enrichment, immediately before processTrack. Reusing it avoids fetching
	// both the song and its album twice. The name guard retains a defensive
	// fallback for callers that provide only an Adam ID.
	if collectionType == applemusic.TypeSong && strings.TrimSpace(initial.Name) != "" {
		if initial.AlbumID != "" && (initial.TrackCount == 0 || initial.DiscCount == 0) {
			// Song's album enrichment is best-effort. If it failed during the
			// collection resolve, retain the old pipeline's recovery opportunity
			// without repeating the successful song request.
			if album, err := r.album(ctx, initial.AlbumID); err == nil && len(album.Tracks) > 0 {
				initial = applemusic.EnrichWithAlbum(initial, album)
			}
		}
		return initial, nil
	}

	// CatalogClient owns the process-wide Apple request gate; do not layer a
	// second metadata limiter here or a track could hold one global resource
	// while waiting for another.
	song, err := r.downloader.catalog.SongMetadata(ctx, r.storefront, initial.ID)
	if err != nil {
		return applemusic.Song{}, err
	}
	song = mergeResolvedSong(song, initial)
	if collectionType == applemusic.TypeAlbum || collectionType == applemusic.TypeArtist {
		// Album track relationships are authoritative for placement. In
		// particular, the same catalog song ID can belong to multiple albums;
		// the lightweight song refresh may describe only its default album and
		// must not redirect every occurrence into that album's output path.
		song = preserveCollectionTrackContext(song, initial)
	}
	if (collectionType != applemusic.TypePlaylist && collectionType != applemusic.TypeStation) || song.AlbumID == "" {
		return song, nil
	}

	// Playlist and station track payloads do not carry the collection-level
	// album fields available to album and artist resolves (station tracks come
	// from the rolling next-tracks feed, whose album relationship lacks
	// per-track disc totals and the album artist id). Preserve Song's
	// historical best-effort enrichment, but share one album read across every
	// track from that album and every concurrent worker in this job.
	album, err := r.album(ctx, song.AlbumID)
	if err == nil && len(album.Tracks) > 0 {
		song = applemusic.EnrichWithAlbum(song, album)
	}
	return song, nil
}

func (r *trackMetadataResolver) album(ctx context.Context, albumID string) (applemusic.Collection, error) {
	r.mu.Lock()
	if pending, ok := r.albums[albumID]; ok {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return applemusic.Collection{}, ctx.Err()
		case <-pending.done:
			return pending.album, pending.err
		}
	}
	pending := &albumMetadataResult{done: make(chan struct{})}
	r.albums[albumID] = pending
	r.mu.Unlock()

	pending.album, pending.err = r.downloader.catalog.Album(ctx, r.storefront, albumID)
	r.mu.Lock()
	if pending.err != nil && r.albums[albumID] == pending {
		// Do not retain transient failures for the whole job. Concurrent callers
		// still share this result, while a later track may try the album again.
		delete(r.albums, albumID)
	}
	close(pending.done)
	r.mu.Unlock()
	return pending.album, pending.err
}

func mergeResolvedSong(song, initial applemusic.Song) applemusic.Song {
	song.ID = firstNonEmpty(song.ID, initial.ID)
	song.Name = firstNonEmpty(song.Name, initial.Name)
	song.ArtistName = firstNonEmpty(song.ArtistName, initial.ArtistName)
	song.AlbumName = firstNonEmpty(song.AlbumName, initial.AlbumName)
	song.ComposerName = firstNonEmpty(song.ComposerName, initial.ComposerName)
	if len(song.GenreNames) == 0 {
		song.GenreNames = append([]string(nil), initial.GenreNames...)
	}
	song.ReleaseDate = firstNonEmpty(song.ReleaseDate, initial.ReleaseDate)
	if song.TrackNumber == 0 {
		song.TrackNumber = initial.TrackNumber
	}
	if song.DiscNumber == 0 {
		song.DiscNumber = initial.DiscNumber
	}
	if song.DurationInMillis == 0 {
		song.DurationInMillis = initial.DurationInMillis
	}
	song.ISRC = firstNonEmpty(song.ISRC, initial.ISRC)
	song.ContentRating = firstNonEmpty(song.ContentRating, initial.ContentRating)
	song.ArtworkURL = firstNonEmpty(song.ArtworkURL, initial.ArtworkURL)
	song.ArtistArtworkURL = firstNonEmpty(song.ArtistArtworkURL, initial.ArtistArtworkURL)
	song.EnhancedHLS = firstNonEmpty(song.EnhancedHLS, initial.EnhancedHLS)
	song.AlbumID = firstNonEmpty(song.AlbumID, initial.AlbumID)
	song.ArtistID = firstNonEmpty(song.ArtistID, initial.ArtistID)

	// Collection resolves have more authoritative album-wide values than the
	// individual song relationship, especially disc and track totals.
	song.AlbumArtist = firstNonEmpty(initial.AlbumArtist, song.AlbumArtist)
	song.AlbumArtistID = firstNonEmpty(initial.AlbumArtistID, song.AlbumArtistID)
	song.AlbumArtworkURL = firstNonEmpty(initial.AlbumArtworkURL, song.AlbumArtworkURL)
	song.AlbumArtistArtworkURL = firstNonEmpty(initial.AlbumArtistArtworkURL, song.AlbumArtistArtworkURL)
	song.AlbumRelease = firstNonEmpty(initial.AlbumRelease, song.AlbumRelease)
	song.Copyright = firstNonEmpty(initial.Copyright, song.Copyright)
	song.RecordLabel = firstNonEmpty(initial.RecordLabel, song.RecordLabel)
	song.UPC = firstNonEmpty(initial.UPC, song.UPC)
	if initial.TrackCount > 0 {
		song.TrackCount = initial.TrackCount
	}
	if initial.DiscCount > 0 {
		song.DiscCount = initial.DiscCount
	}
	return song
}

func preserveCollectionTrackContext(song, initial applemusic.Song) applemusic.Song {
	song.AlbumID = firstNonEmpty(initial.AlbumID, song.AlbumID)
	song.AlbumName = firstNonEmpty(initial.AlbumName, song.AlbumName)
	song.ArtworkURL = firstNonEmpty(initial.ArtworkURL, song.ArtworkURL)
	if initial.TrackNumber > 0 {
		song.TrackNumber = initial.TrackNumber
	}
	if initial.DiscNumber > 0 {
		song.DiscNumber = initial.DiscNumber
	}
	return song
}
