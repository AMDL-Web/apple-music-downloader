package media

import (
	"context"
	"fmt"

	"amdl/internal/applemusic"
)

type resolvedCollection struct {
	Tracks []applemusic.Song
	// Placements parallels Tracks for a song, album or artist: each track's
	// album and position, from which its library path follows. Nil for
	// playlists and stations, and for a song resolved without place.
	Placements []libraryPlacement
	ID         string
	Name       string
	ArtworkURL string
	// Display metadata backfilled onto the job next to Name/ArtworkURL.
	// Populated per type in resolveCollection; empty when the catalog has no
	// suitable value (never an error).
	ArtistName string
	// ArtistURL is the artist's own music.apple.com page. Only the types whose
	// ArtistName is a real artist carry one — album, song and artist; playlists
	// and stations leave it empty along with ArtistName.
	ArtistURL   string
	CuratorName string
	ReleaseDate string
	Genre       string
	// ArtworkColors is the palette Apple attaches to the artwork behind
	// ArtworkURL; zero-valued when the catalog omits it.
	ArtworkColors applemusic.ArtworkColors
}

// mediaUserToken returns the effective media-user-token (internal/usertoken):
// the wrapper's when amdl can read it, else catalog.media_user_token from the
// runtime snapshot this job runs under. A submission cannot supply its own.
func (d *Downloader) mediaUserToken() string {
	return d.tokens.Token(d.cfg).Value
}

// resolveCollection reads the link's resource and the tracks it downloads.
// With place, a song is placed in the library: its album is read to find its
// position there, and a failed read, a song without an album or one missing
// from its album's track list fails the resolve. Without place (a quality
// query) that album read only enriches the song and degrades to nothing.
func (d *Downloader) resolveCollection(ctx context.Context, parsed applemusic.ParsedURL, place bool) (resolvedCollection, error) {
	switch parsed.Type {
	case applemusic.TypeSong:
		song, placements, err := d.resolveSong(ctx, parsed.Storefront, parsed.ID, place)
		if err != nil {
			return resolvedCollection{}, err
		}
		artworkURL, colors := song.DisplayArtwork()
		return resolvedCollection{
			Tracks: []applemusic.Song{song}, Placements: placements, Name: song.Name, ArtworkURL: artworkURL,
			ArtistName: song.ArtistName, ArtistURL: song.ArtistURL, ReleaseDate: song.ReleaseDate, Genre: applemusic.PrimaryGenre(song.GenreNames),
			ArtworkColors: colors,
		}, nil
	case applemusic.TypeAlbum:
		album, err := d.catalog.Album(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resolvedCollection{}, err
		}
		return albumCollection(album), nil
	case applemusic.TypePlaylist:
		playlist, err := d.catalog.Playlist(ctx, parsed.Storefront, parsed.ID, d.mediaUserToken())
		if err != nil {
			return resolvedCollection{}, err
		}
		return playlistCollection(playlist), nil
	case applemusic.TypeStation:
		station, err := d.catalog.StationTracks(ctx, parsed.Storefront, parsed.ID, d.mediaUserToken())
		if err != nil {
			return resolvedCollection{}, err
		}
		return resolvedCollection{
			Tracks: station.Tracks, ID: station.ID, Name: station.Name, ArtworkURL: station.ArtworkURL,
			// Artist holds applemusic.StationCuratorName.
			CuratorName:   station.Artist,
			ArtworkColors: station.ArtworkColors,
		}, nil
	case applemusic.TypeArtist:
		artist, err := d.catalog.ArtistAlbums(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resolvedCollection{}, err
		}
		tracks, placements, err := d.artistTracks(ctx, parsed.Storefront, artist.Albums)
		if err != nil {
			return resolvedCollection{}, err
		}
		return resolvedCollection{
			Tracks: tracks, Placements: placements, ID: artist.ID, Name: artist.Name, ArtworkURL: artist.ArtworkURL,
			// For artist jobs the artist's own name doubles as the display
			// artist.
			ArtistName:    artist.Name,
			ArtistURL:     artist.URL,
			ArtworkColors: artist.ArtworkColors,
		}, nil
	default:
		return resolvedCollection{}, fmt.Errorf("unsupported input type %s", parsed.Type)
	}
}

// albumCollection is an album read as the tracks it downloads, each placed in
// the album.
func albumCollection(album applemusic.Collection) resolvedCollection {
	return resolvedCollection{
		Tracks: album.Tracks, Placements: albumPlacements(album), ID: album.ID, Name: album.Name, ArtworkURL: album.ArtworkURL,
		ArtistName: album.Artist, ArtistURL: album.ArtistURL, ReleaseDate: album.ReleaseDate, Genre: applemusic.PrimaryGenre(album.GenreNames),
		ArtworkColors: album.ArtworkColors,
	}
}

// playlistCollection is a playlist read as the tracks it downloads.
func playlistCollection(playlist applemusic.Collection) resolvedCollection {
	return resolvedCollection{
		Tracks: playlist.Tracks, ID: playlist.ID, Name: playlist.Name, ArtworkURL: playlist.ArtworkURL,
		// Playlist collections carry curatorName (falling back to
		// artistName) in Artist.
		CuratorName:   playlist.Artist,
		ArtworkColors: playlist.ArtworkColors,
	}
}

// resolveSong reads a single song. Its song read carries the stream
// (extendedAssetUrls) the album's track entries lack, so that read stays the
// track; the album read adds the album-wide values and, with place, the
// song's position for its library path.
func (d *Downloader) resolveSong(ctx context.Context, storefront, id string, place bool) (applemusic.Song, []libraryPlacement, error) {
	song, err := d.catalog.SongMetadata(ctx, storefront, id)
	if err != nil {
		return applemusic.Song{}, nil, err
	}
	if song.AlbumID == "" {
		if place {
			return applemusic.Song{}, nil, placementError{fmt.Sprintf("song %s has no album to place it in", song.ID)}
		}
		return song, nil, nil
	}
	album, err := d.catalog.Album(ctx, storefront, song.AlbumID)
	if err != nil {
		if place {
			return applemusic.Song{}, nil, fmt.Errorf("read album %s to place song %s: %w", song.AlbumID, song.ID, err)
		}
		return song, nil, nil
	}
	if len(album.Tracks) > 0 {
		song = applemusic.EnrichWithAlbum(song, album)
	}
	if !place {
		return song, nil, nil
	}
	placement, ok := placeInAlbum(album, song.ID)
	if !ok {
		return applemusic.Song{}, nil, placementError{fmt.Sprintf("song %s is not in its album's track list (album %s)", song.ID, song.AlbumID)}
	}
	return song, []libraryPlacement{placement}, nil
}

// artistTracks reads every album of an artist. Each track is placed in its
// own album, so an album credited to another artist (a compilation, a
// collaboration) lands under that album's artist.
func (d *Downloader) artistTracks(ctx context.Context, storefront string, albums []applemusic.Collection) ([]applemusic.Song, []libraryPlacement, error) {
	tracks := make([]applemusic.Song, 0)
	placements := make([]libraryPlacement, 0)
	for _, summary := range albums {
		album, err := d.catalog.Album(ctx, storefront, summary.ID)
		if err != nil {
			return nil, nil, err
		}
		// Keep the complete track list of every album. A catalog song may be
		// related to more than one album with the same Adam ID; artist downloads
		// still need one output in each album directory.
		tracks = append(tracks, album.Tracks...)
		placements = append(placements, albumPlacements(album)...)
	}
	return tracks, placements, nil
}
