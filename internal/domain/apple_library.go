package domain

// AppleLibraryItem is one cached entry of the user's Apple Music account
// library (an album, song, playlist or artist), the AppleLibraryItem schema of
// GET /api/v1/apple-library/{kind}. It is not the on-disk library layout.
//
// ID is Apple's library id (l., i., p., r.); CatalogID and URL are the catalog
// id and link a download takes, both empty when the item has no catalog
// counterpart. Fields a kind does not have stay empty and are left out.
type AppleLibraryItem struct {
	Kind         string `json:"-"`
	ID           string `json:"id"`
	CatalogID    string `json:"catalog_id,omitempty"`
	URL          string `json:"url,omitempty"`
	Name         string `json:"name"`
	ArtistName   string `json:"artist_name,omitempty"`
	AlbumName    string `json:"album_name,omitempty"`
	ArtworkURL   string `json:"artwork_url,omitempty"`
	Description  string `json:"description,omitempty"`
	Genre        string `json:"genre,omitempty"`
	ReleaseDate  string `json:"release_date,omitempty"`
	TrackCount   int    `json:"track_count,omitempty"`
	DurationMS   int    `json:"duration_ms,omitempty"`
	TrackNumber  int    `json:"track_number,omitempty"`
	DiscNumber   int    `json:"disc_number,omitempty"`
	DateAdded    string `json:"date_added,omitempty"`
	LastModified string `json:"last_modified,omitempty"`
	// PlaylistKind is favorites, own, editorial, replay or shared.
	PlaylistKind string `json:"playlist_kind,omitempty"`
	// VersionHash is Apple's playParams.versionHash of a playlist the user
	// does not own; the sync re-reads the track list when it changes.
	VersionHash string `json:"-"`
	// AddOrder sorts by add time: higher is newer. Apple gives songs no add
	// time, only a dependable newest-first order, so the sync numbers items
	// by their place in it.
	AddOrder int64 `json:"-"`
}

// AppleLibraryTrack is one track of a cached Apple library playlist, in
// playlist order (the AppleLibraryTrack schema).
type AppleLibraryTrack struct {
	Position   int    `json:"position"`
	SongID     string `json:"song_id"`
	CatalogID  string `json:"catalog_id,omitempty"`
	URL        string `json:"url,omitempty"`
	Name       string `json:"name"`
	ArtistName string `json:"artist_name,omitempty"`
	AlbumName  string `json:"album_name,omitempty"`
	DurationMS int    `json:"duration_ms,omitempty"`
	ArtworkURL string `json:"artwork_url,omitempty"`
}
