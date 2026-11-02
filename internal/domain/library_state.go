package domain

// LibraryStatus says how much of an album (or a song's album entry) is in
// the library on disk.
type LibraryStatus string

const (
	LibraryNone     LibraryStatus = "none"
	LibraryPartial  LibraryStatus = "partial"
	LibraryComplete LibraryStatus = "complete"
)

// LibraryState is the `library` field of search hits, lookups and the album
// page: how many of an album's tracks had a file at their library path. The
// backend no longer fills it, since records and not files say what is
// downloaded; the clients and the demo still read it. TODO(P3): the record's
// `resource` replaces it and this file goes.
type LibraryState struct {
	State        LibraryStatus `json:"state"`
	TracksOnDisk int           `json:"tracks_on_disk"`
	TracksTotal  int           `json:"tracks_total"`
}

// NewLibraryState classifies onDisk of total tracks: complete once every
// track is there (and there is at least one), partial for some, none
// otherwise. onDisk is capped at total, since a folder count can see files
// the track list does not name.
func NewLibraryState(onDisk, total int) LibraryState {
	onDisk = max(0, min(onDisk, total))
	state := LibraryNone
	switch {
	case total > 0 && onDisk == total:
		state = LibraryComplete
	case onDisk > 0:
		state = LibraryPartial
	}
	return LibraryState{State: state, TracksOnDisk: onDisk, TracksTotal: total}
}
