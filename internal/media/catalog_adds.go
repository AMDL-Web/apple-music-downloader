package media

import (
	"context"
	"errors"
	"fmt"

	"amdl/internal/jobs"
)

var _ jobs.Catalog = (*Downloader)(nil)

// AccountStorefront implements jobs.Catalog: the storefront of the default
// account (design §3.2). It takes the account's region, then asks Apple with
// its media-user-token, then takes the wrapper's single region, then
// catalog.default_storefront.
func (d *Downloader) AccountStorefront(ctx context.Context) (string, error) {
	if region := d.tokens.DefaultRegion(); region != "" {
		return region, nil
	}
	cfg := d.baseConfig()
	cd := d.withConfig(cfg)
	cd.storefront = ""
	if token := cd.inProcessToken(); token != "" {
		storefront, err := cd.playback.AccountStorefront(ctx, token)
		if err == nil && storefront != "" {
			return storefront, nil
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	if !cfg.Simulate.Enabled && cd.wrapper != nil {
		if status, err := cd.wrapper.Status(ctx); err == nil && len(status.Regions) == 1 {
			return status.Regions[0], nil
		}
	}
	if cfg.Catalog.DefaultStorefront == "" {
		return "", errors.New("no account storefront: no media-user-token, no single wrapper region and no catalog.default_storefront")
	}
	return cfg.Catalog.DefaultStorefront, nil
}

// RunStorefront is the storefront a record pinned to pin is read in: the pin,
// else the default account's (AccountStorefront).
func (d *Downloader) RunStorefront(ctx context.Context, pin string) (string, error) {
	if pin != "" {
		return pin, nil
	}
	return d.AccountStorefront(ctx)
}

// SongAlbumID implements jobs.Catalog with the song's required metadata read.
func (d *Downloader) SongAlbumID(ctx context.Context, storefront, songID string) (string, error) {
	song, err := d.catalog.SongMetadata(ctx, storefront, songID)
	if err != nil {
		return "", err
	}
	if song.AlbumID == "" {
		return "", placementError{fmt.Sprintf("song %s has no album to place it in", songID)}
	}
	return song.AlbumID, nil
}

// ArtistAlbumIDs implements jobs.Catalog with the artist's album list: every
// album the artist read returns for include "all", the ones that are not
// singles for "albums".
func (d *Downloader) ArtistAlbumIDs(ctx context.Context, storefront, artistID, include string) ([]string, error) {
	artist, err := d.catalog.ArtistAlbums(ctx, storefront, artistID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(artist.Albums))
	for _, album := range artist.Albums {
		if include == "albums" && album.IsSingle {
			continue
		}
		ids = append(ids, album.ID)
	}
	return ids, nil
}
