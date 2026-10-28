package media

import (
	"context"
	"fmt"
	"strings"

	"amdl/internal/applemusic"
	"amdl/internal/config"
)

func collectionFolderArtist(collectionType applemusic.URLType, tracks []applemusic.Song) string {
	if outputCollectionType(collectionType) != applemusic.TypeAlbum || len(tracks) == 0 {
		return ""
	}
	if tracks[0].AlbumArtist != "" {
		return tracks[0].AlbumArtist
	}
	return tracks[0].ArtistName
}

func outputCollectionType(collectionType applemusic.URLType) applemusic.URLType {
	if collectionType == applemusic.TypeArtist {
		return applemusic.TypeAlbum
	}
	return collectionType
}

type resolvedCollection struct {
	Tracks     []applemusic.Song
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

// mediaUserToken returns the token from this job's effective config. ProcessJob
// has already layered overrides.media_user_token over the current runtime
// catalog value, so retries and post-restart recovery follow the same path.
func (d *Downloader) mediaUserToken() string {
	return strings.TrimSpace(d.cfg.Catalog.MediaUserToken)
}

func (d *Downloader) resolveCollection(ctx context.Context, parsed applemusic.ParsedURL) (resolvedCollection, error) {
	switch parsed.Type {
	case applemusic.TypeSong:
		song, err := d.catalog.Song(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resolvedCollection{}, err
		}
		artworkURL, colors := song.DisplayArtwork()
		return resolvedCollection{
			Tracks: []applemusic.Song{song}, Name: song.Name, ArtworkURL: artworkURL,
			ArtistName: song.ArtistName, ArtistURL: song.ArtistURL, ReleaseDate: song.ReleaseDate, Genre: applemusic.PrimaryGenre(song.GenreNames),
			ArtworkColors: colors,
		}, nil
	case applemusic.TypeAlbum:
		album, err := d.catalog.Album(ctx, parsed.Storefront, parsed.ID)
		if err != nil {
			return resolvedCollection{}, err
		}
		return resolvedCollection{
			Tracks: album.Tracks, ID: album.ID, Name: album.Name, ArtworkURL: album.ArtworkURL,
			ArtistName: album.Artist, ArtistURL: album.ArtistURL, ReleaseDate: album.ReleaseDate, Genre: applemusic.PrimaryGenre(album.GenreNames),
			ArtworkColors: album.ArtworkColors,
		}, nil
	case applemusic.TypePlaylist:
		playlist, err := d.catalog.Playlist(ctx, parsed.Storefront, parsed.ID, d.mediaUserToken())
		if err != nil {
			return resolvedCollection{}, err
		}
		return resolvedCollection{
			Tracks: playlist.Tracks, ID: playlist.ID, Name: playlist.Name, ArtworkURL: playlist.ArtworkURL,
			// Playlist collections carry curatorName (falling back to
			// artistName) in Artist.
			CuratorName:   playlist.Artist,
			ArtworkColors: playlist.ArtworkColors,
		}, nil
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
		tracks, err := d.artistTracks(ctx, parsed.Storefront, artist.Albums)
		if err != nil {
			return resolvedCollection{}, err
		}
		return resolvedCollection{
			Tracks: tracks, ID: artist.ID, Name: artist.Name, ArtworkURL: artist.ArtworkURL,
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

// collectionIndexes numbers each resolved track for {SongNumber}. For songs,
// albums, playlists and stations that is the track's position in the resolved
// list — for an album, Apple orders the track relationship disc by disc, so the
// numbering runs unbroken across discs instead of restarting like TrackNumber
// does.
//
// An artist job concatenates every album's tracks into one list, so its counter
// restarts per album — but only when the template groups the output per album.
// A configured artist template naming neither the album nor its id drops the
// whole discography into a single directory, where a per-album counter would
// hand two albums the same {SongNumber} and collide on an identically titled
// track at the same position; there the job-wide position is what keeps the
// names apart. The counter restarts exactly when the directories do.
func collectionIndexes(cfg config.Config, collectionType applemusic.URLType, tracks []applemusic.Song) []int {
	indexes := make([]int, len(tracks))
	perAlbum := collectionType == applemusic.TypeArtist &&
		segmentReferences(pathPattern(cfg, collectionType), "AlbumName", "AlbumId")
	if !perAlbum {
		for i := range tracks {
			indexes[i] = i + 1
		}
		return indexes
	}
	counts := make(map[string]int, len(tracks))
	for i, track := range tracks {
		counts[track.AlbumID]++
		indexes[i] = counts[track.AlbumID]
	}
	return indexes
}

func (d *Downloader) artistTracks(ctx context.Context, storefront string, albums []applemusic.Collection) ([]applemusic.Song, error) {
	tracks := make([]applemusic.Song, 0)
	for _, summary := range albums {
		album, err := d.catalog.Album(ctx, storefront, summary.ID)
		if err != nil {
			return nil, err
		}
		// Keep the complete track list of every album. A catalog song may be
		// related to more than one album with the same Adam ID; artist downloads
		// still need one output in each album directory.
		tracks = append(tracks, album.Tracks...)
	}
	return tracks, nil
}
