package domain

// LibraryAnchorEntry is one remembered Apple Music library song. The watcher
// keeps the newest N of them in order (index 0 = most recently added) and
// treats anything newer than the oldest remembered entry, but not among them,
// as a new addition.
//
// Apple does not expose a per-song dateAdded — library-songs simply has no such
// attribute, and library-albums.dateAdded is the moment the album's *first*
// song entered, so it never moves when a later song is added to the same album.
// What is reliable is the ORDER that sort=-dateAdded returns, so the watermark
// is a list of song ids rather than a timestamp.
//
// Name and AlbumName are carried for legibility only; nothing keys off them.
type LibraryAnchorEntry struct {
	SongID    string `json:"song_id"`
	Name      string `json:"name,omitempty"`
	AlbumName string `json:"album_name,omitempty"`
}
