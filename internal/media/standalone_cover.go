package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"amdl/internal/applemusic"
)

type standaloneCoverState struct {
	mu      sync.Mutex
	handled map[string]struct{}
}

func newStandaloneCoverState() *standaloneCoverState {
	return &standaloneCoverState{handled: make(map[string]struct{})}
}

func (s *standaloneCoverState) wasHandled(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.handled[path]
	return ok
}

func (s *standaloneCoverState) markHandled(path string) {
	s.mu.Lock()
	s.handled[path] = struct{}{}
	s.mu.Unlock()
}

// savePlaylistCover writes cover.<ext> into playlistDir. replace writes over
// a file already there (a covers reset).
func (d *Downloader) savePlaylistCover(ctx context.Context, artworkURL, playlistDir string, replace bool) error {
	if artworkURL == "" {
		return nil
	}
	ext, err := standaloneCoverExt(d.cfg.Download.CoverFormat)
	if err != nil {
		return err
	}
	path := filepath.Join(playlistDir, "cover."+ext)
	if err := d.ensureStandaloneCover(ctx, path, func(context.Context) (string, error) {
		return artworkURL, nil
	}, replace); err != nil {
		return fmt.Errorf("save playlist cover: %w", err)
	}
	return nil
}

func (d *Downloader) saveStandaloneCovers(ctx context.Context, song applemusic.Song, collectionType applemusic.URLType, storefront, albumDir, artistDir string) error {
	if !inLibrary(collectionType) {
		return nil
	}
	var saveErrors []error
	if d.cfg.Download.SaveAlbumCover {
		if err := d.saveAlbumCover(ctx, song, albumDir, false); err != nil {
			saveErrors = append(saveErrors, err)
		}
	}
	if d.cfg.Download.SaveArtistCover && artistDir != "" {
		if err := d.saveArtistCover(ctx, song, storefront, artistDir, false); err != nil {
			saveErrors = append(saveErrors, err)
		}
	}
	return errors.Join(saveErrors...)
}

// saveAlbumCover writes cover.<ext> into albumDir from song's album artwork.
func (d *Downloader) saveAlbumCover(ctx context.Context, song applemusic.Song, albumDir string, replace bool) error {
	ext, err := standaloneCoverExt(d.cfg.Download.CoverFormat)
	if err != nil {
		return err
	}
	artworkURL := firstNonEmpty(song.AlbumArtworkURL, song.ArtworkURL)
	if err := d.ensureStandaloneCover(ctx, filepath.Join(albumDir, "cover."+ext), func(context.Context) (string, error) {
		return artworkURL, nil
	}, replace); err != nil {
		return fmt.Errorf("save album cover: %w", err)
	}
	return nil
}

// saveArtistCover writes artist.<ext> into artistDir. The artist folder is
// the album artist's ({AlbumArtist}), so its picture is too, whichever add
// wrote the track.
func (d *Downloader) saveArtistCover(ctx context.Context, song applemusic.Song, storefront, artistDir string, replace bool) error {
	ext, err := standaloneCoverExt(d.cfg.Download.CoverFormat)
	if err != nil {
		return err
	}
	if err := d.ensureStandaloneCover(ctx, filepath.Join(artistDir, "artist."+ext), func(ctx context.Context) (string, error) {
		artistID := firstNonEmpty(song.AlbumArtistID, song.ArtistID)
		artworkURL := firstNonEmpty(song.AlbumArtistArtworkURL, song.ArtistArtworkURL)
		if artworkURL != "" {
			return artworkURL, nil
		}
		if artistID == "" {
			return "", nil
		}
		artist, err := d.catalog.Artist(ctx, storefront, artistID)
		return artist.ArtworkURL, err
	}, replace); err != nil {
		return fmt.Errorf("save artist cover: %w", err)
	}
	return nil
}

// ensureStandaloneCover writes the cover at path unless a file is already
// there, or over it with replace. Every outcome is final for the job: the path
// is not tried again.
func (d *Downloader) ensureStandaloneCover(ctx context.Context, path string, resolveURL func(context.Context) (string, error), replace bool) error {
	// Reuse the process-wide keyed output locks: callers targeting the same
	// canonical path remain serialized, while unrelated album and artist covers
	// can resolve and download concurrently. Waiting is cancellation-aware.
	unlock, err := processOutputLocks.acquireContext(ctx, path)
	if err != nil {
		return err
	}
	defer unlock()

	if d.standaloneCoverState != nil {
		if d.standaloneCoverState.wasHandled(path) {
			return nil
		}
		// Every outcome below is final for this job. A failed lookup or fetch has
		// already spent the configured retry budget; letting every remaining
		// track repeat it only amplifies pressure on the same upstream resource.
		defer d.standaloneCoverState.markHandled(path)
	}
	if !replace {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	artworkURL, _, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(int) (string, error) {
		return resolveURL(ctx)
	}, nil)
	if err != nil {
		return err
	}
	if artworkURL == "" {
		return nil
	}
	backend := d.backend()
	data, _, err := retryValue(ctx, d.cfg.Download.MaxAttempts, retryBackoff, func(int) ([]byte, error) {
		return backend.coverArt(ctx, []string{artworkURL}, d.cfg.Download.CoverFormat, d.cfg.Download.CoverSize)
	}, nil)
	if err != nil {
		return err
	}
	if err := backend.mkdirAll(filepath.Dir(path)); err != nil {
		return err
	}
	// Atomic: the existence check above treats any file at path as done, so a
	// truncated cover must never be visible there.
	return backend.writeFile(path, data)
}

func standaloneCoverExt(format string) (string, error) {
	switch format {
	case "jpg", "jpeg":
		return "jpg", nil
	case "png":
		return "png", nil
	default:
		return "", fmt.Errorf("unsupported cover format %q", format)
	}
}
