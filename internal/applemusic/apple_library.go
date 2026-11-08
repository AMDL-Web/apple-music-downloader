package applemusic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// LibraryKind is one category of the signed-in user's Apple Music library, as
// amp-api names its /v1/me/library collection.
type LibraryKind string

// The library categories LibraryItems reads.
const (
	LibraryAlbums    LibraryKind = "albums"
	LibrarySongs     LibraryKind = "songs"
	LibraryPlaylists LibraryKind = "playlists"
	LibraryArtists   LibraryKind = "artists"
)

// LibraryKinds lists every LibraryKind in the order a library page shows them.
var LibraryKinds = []LibraryKind{LibraryAlbums, LibrarySongs, LibraryPlaylists, LibraryArtists}

// LibraryItem is one entry of the user's library or of a library playlist's
// track list. ID is the library id (l., i., p., r.), personal to the account;
// CatalogID is the catalog id downloads use, empty when the item has no
// catalog counterpart (uploaded, withdrawn, or an unshared own playlist).
//
// CatalogURL is the catalog page Apple names for albums and artists (from the
// include=catalog relationship); songs and playlists carry none and callers
// build the link from CatalogID.
type LibraryItem struct {
	ID           string
	CatalogID    string
	CatalogURL   string
	Name         string
	ArtistName   string
	AlbumName    string
	ArtworkURL   string
	Description  string
	Genre        string
	ReleaseDate  string
	TrackCount   int
	DurationMS   int
	TrackNumber  int
	DiscNumber   int
	DateAdded    string
	LastModified string
	// VersionHash is playParams.versionHash, set on playlists the user does
	// not own; it changes when the curator changes the playlist.
	VersionHash string
	CanEdit     bool
	CanDelete   bool
}

// LibraryPage is one page of a library listing. Total is Apple's meta.total
// for the whole collection; HasMore reports whether Apple offered a next page.
type LibraryPage struct {
	Items   []LibraryItem
	Total   int
	HasMore bool
}

type libraryListResponse struct {
	Next string `json:"next"`
	Meta struct {
		Total int `json:"total"`
	} `json:"meta"`
	Data []libraryResource `json:"data"`
}

type libraryResource struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Attributes struct {
		Name             string          `json:"name"`
		ArtistName       string          `json:"artistName"`
		AlbumName        string          `json:"albumName"`
		Artwork          artwork         `json:"artwork"`
		DurationInMillis int             `json:"durationInMillis"`
		TrackNumber      int             `json:"trackNumber"`
		DiscNumber       int             `json:"discNumber"`
		TrackCount       int             `json:"trackCount"`
		GenreNames       []string        `json:"genreNames"`
		ReleaseDate      string          `json:"releaseDate"`
		DateAdded        string          `json:"dateAdded"`
		LastModifiedDate string          `json:"lastModifiedDate"`
		Description      json.RawMessage `json:"description"`
		CanEdit          bool            `json:"canEdit"`
		CanDelete        bool            `json:"canDelete"`
		PlayParams       struct {
			CatalogID   string `json:"catalogId"`
			GlobalID    string `json:"globalId"`
			VersionHash string `json:"versionHash"`
		} `json:"playParams"`
	} `json:"attributes"`
	Relationships struct {
		Catalog struct {
			Data []struct {
				ID         string `json:"id"`
				Attributes struct {
					URL        string   `json:"url"`
					Artwork    artwork  `json:"artwork"`
					GenreNames []string `json:"genreNames"`
				} `json:"attributes"`
			} `json:"data"`
		} `json:"catalog"`
	} `json:"relationships"`
}

// LibraryItems reads one page of the user's library category kind. newestFirst
// orders songs, albums and playlists by add time descending (sort=-dateAdded,
// the one order Apple keeps dependable, see LibrarySong); otherwise, and always
// for artists, which accept no add order, the page is in Apple's name order.
// Albums and artists include their catalog counterpart, one request per page.
//
// mediaUserToken is required; Apple answers 403 without a usable one, which
// IsUserTokenRejected reports. limit is capped at Apple's 100.
func (c *CatalogClient) LibraryItems(ctx context.Context, mediaUserToken string, kind LibraryKind, newestFirst bool, offset, limit int) (LibraryPage, error) {
	if mediaUserToken == "" {
		return LibraryPage{}, fmt.Errorf("reading the library requires a media-user-token")
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(libraryLimit(limit)))
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	params.Set("l", c.cfg.Language)
	switch kind {
	case LibraryAlbums, LibraryArtists:
		params.Set("include", "catalog")
	case LibrarySongs, LibraryPlaylists:
	default:
		return LibraryPage{}, fmt.Errorf("unknown library kind %q", kind)
	}
	if newestFirst && kind != LibraryArtists {
		params.Set("sort", "-dateAdded")
	} else {
		params.Set("sort", "name")
	}
	return c.libraryPage(ctx, ampAPIBase+"/v1/me/library/"+string(kind), params, mediaUserToken)
}

// LibraryPlaylistTracks reads one page of a library playlist's tracks in
// playlist order. It is how a playlist without a catalog counterpart (an
// unshared own playlist, Favorite Songs) can be listed at all.
func (c *CatalogClient) LibraryPlaylistTracks(ctx context.Context, mediaUserToken, playlistID string, offset, limit int) (LibraryPage, error) {
	if mediaUserToken == "" {
		return LibraryPage{}, fmt.Errorf("reading the library requires a media-user-token")
	}
	params := url.Values{}
	params.Set("limit", strconv.Itoa(libraryLimit(limit)))
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	params.Set("l", c.cfg.Language)
	endpoint := fmt.Sprintf("%s/v1/me/library/playlists/%s/tracks", ampAPIBase, url.PathEscape(playlistID))
	page, err := c.libraryPage(ctx, endpoint, params, mediaUserToken)
	if err != nil && IsNotFound(err) && offset > 0 {
		// Apple answers 404 for an offset past the last track.
		return LibraryPage{Items: []LibraryItem{}}, nil
	}
	return page, err
}

func libraryLimit(limit int) int {
	if limit <= 0 || limit > maxLibraryPageLimit {
		return maxLibraryPageLimit
	}
	return limit
}

func (c *CatalogClient) libraryPage(ctx context.Context, endpoint string, params url.Values, mediaUserToken string) (LibraryPage, error) {
	var resp libraryListResponse
	if err := c.getWithUserToken(ctx, endpoint, params, mediaUserToken, &resp); err != nil {
		return LibraryPage{}, err
	}
	page := LibraryPage{Items: make([]LibraryItem, 0, len(resp.Data)), Total: resp.Meta.Total, HasMore: resp.Next != ""}
	for _, raw := range resp.Data {
		page.Items = append(page.Items, mapLibraryItem(raw))
	}
	return page, nil
}

func mapLibraryItem(raw libraryResource) LibraryItem {
	a := raw.Attributes
	item := LibraryItem{
		ID: raw.ID, Name: a.Name, ArtistName: a.ArtistName, AlbumName: a.AlbumName,
		ArtworkURL: a.Artwork.URL, Description: libraryDescription(a.Description),
		Genre: PrimaryGenre(a.GenreNames), ReleaseDate: a.ReleaseDate, TrackCount: a.TrackCount,
		DurationMS: a.DurationInMillis, TrackNumber: a.TrackNumber, DiscNumber: a.DiscNumber,
		DateAdded: a.DateAdded, LastModified: a.LastModifiedDate, VersionHash: a.PlayParams.VersionHash,
		CanEdit: a.CanEdit, CanDelete: a.CanDelete,
		CatalogID: firstNonEmpty(a.PlayParams.CatalogID, a.PlayParams.GlobalID),
	}
	if catalog := raw.Relationships.Catalog.Data; len(catalog) > 0 {
		item.CatalogID = firstNonEmpty(item.CatalogID, catalog[0].ID)
		item.CatalogURL = catalog[0].Attributes.URL
		item.ArtworkURL = firstNonEmpty(item.ArtworkURL, catalog[0].Attributes.Artwork.URL)
		item.Genre = firstNonEmpty(item.Genre, PrimaryGenre(catalog[0].Attributes.GenreNames))
	}
	return item
}

// libraryDescription reads a library playlist's description, which Apple
// sends as {"standard": "..."}; a plain string is accepted too.
func libraryDescription(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var obj struct {
		Standard string `json:"standard"`
		Short    string `json:"short"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return firstNonEmpty(obj.Standard, obj.Short)
	}
	return ""
}
