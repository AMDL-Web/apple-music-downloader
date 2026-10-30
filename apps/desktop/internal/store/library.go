package store

import (
	"amdl/internal/client"
	"amdl/internal/domain"
)

// LibraryKinds are the job types each of the library's tabs lists: albums,
// playlists, songs.
var LibraryKinds = [][]string{{"album"}, {"playlist"}, {"song"}}

// libraryPage is how many finished downloads one read of the library asks
// for.
const libraryPage = 60

// Library is the finished downloads of one kind, read page by page apart
// from the feed, whose snapshot holds only the newest jobs of every status.
type Library struct {
	// Kind indexes LibraryKinds, and Search is the text the list is
	// filtered by.
	Kind   int
	Search string
	// Jobs are newest first; Total is how many match on the backend.
	Jobs  []domain.Job
	Total int
	// Loaded is set once the first page of Kind and Search arrived.
	Loaded  bool
	Loading bool
	Err     string
	// Latest is the newest finished download with its items, for the hero's
	// quality and size, or nil until it is read.
	Latest *client.DownloadDetail

	gen   int
	stale bool
}

// ShowLibrary makes the library list kind and search, reading it again
// when either changed or a download finished since. Views call it every
// frame they show the library; it reads only when something changed.
func (s *Store) ShowLibrary(kind int, search string) {
	l := &s.Library
	// A read that failed waits for ReloadLibrary rather than retry every
	// frame.
	if l.Kind == kind && l.Search == search && (l.Loaded || l.Loading || l.Err != "") && !l.stale {
		return
	}
	if l.Kind != kind || l.Search != search {
		l.Jobs, l.Total, l.Loaded, l.Latest = nil, 0, false, nil
	}
	l.Kind, l.Search, l.stale = kind, search, false
	s.readLibrary(0)
}

// ReloadLibrary reads the library again on the next ShowLibrary.
func (s *Store) ReloadLibrary() { s.Library.stale = true }

// MoreLibrary reads the next page, when there is one.
func (s *Store) MoreLibrary() {
	l := &s.Library
	if l.Loaded && !l.Loading && len(l.Jobs) < l.Total {
		s.readLibrary(len(l.Jobs))
	}
}

func (s *Store) readLibrary(offset int) {
	l := &s.Library
	if s.client == nil {
		return
	}
	l.gen++
	l.Loading, l.Err = true, ""
	gen, c, ctx := l.gen, s.client, s.ctx
	q := client.ListQuery{
		Limit: libraryPage, Offset: offset, Status: []domain.JobStatus{domain.JobCompleted},
		Types: LibraryKinds[l.Kind], Search: l.Search, Sort: "updated_at",
	}
	go func() {
		page, err := c.Downloads(ctx, q)
		s.postIf(ctx, func() {
			if l.gen != gen {
				return // a newer read replaced this one
			}
			l.Loading = false
			if err != nil {
				l.Err = err.Error()
				return
			}
			if offset == 0 {
				l.Jobs = l.Jobs[:0]
			}
			l.Jobs = append(l.Jobs, page.Downloads...)
			l.Total, l.Loaded = page.Total, true
			if offset == 0 && len(l.Jobs) > 0 && (l.Latest == nil || l.Latest.Job.ID != l.Jobs[0].ID) {
				s.readLatest(l.Jobs[0].ID)
			}
		})
	}()
}

func (s *Store) readLatest(id string) {
	c, ctx := s.client, s.ctx
	go func() {
		d, err := c.Download(ctx, id)
		s.postIf(ctx, func() {
			l := &s.Library
			// The hero keeps its facts out rather than show an error: the
			// list already says what the library holds.
			if err == nil && len(l.Jobs) > 0 && l.Jobs[0].ID == id {
				l.Latest = &d
			}
		})
	}()
}
