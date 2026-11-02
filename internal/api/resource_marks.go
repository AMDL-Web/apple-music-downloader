package api

import (
	"context"

	"amdl/internal/applemusic"
	"amdl/internal/domain"
)

// markResources sets resource on hits from the records: an album or playlist
// hit gets its own record, a song hit the album record holding it. A failed
// store read leaves the hits unmarked; the search still answers.
func (s *Server) markResources(ctx context.Context, hits []*searchHit) {
	byType := map[applemusic.URLType][]string{}
	for _, h := range hits {
		byType[h.Type] = append(byType[h.Type], h.ID)
	}
	marks := map[applemusic.URLType]map[string]domain.ResourceMark{}
	for typ, ids := range byType {
		if m, err := s.resourceMarks(ctx, typ, ids); err == nil {
			marks[typ] = m
		}
	}
	for _, h := range hits {
		if m, ok := marks[h.Type][h.ID]; ok {
			h.Resource = &m
		}
	}
}

// resourceMark is the record mark of one catalog object, nil without one.
func (s *Server) resourceMark(ctx context.Context, typ applemusic.URLType, id string) *domain.ResourceMark {
	marks, err := s.resourceMarks(ctx, typ, []string{id})
	if err != nil {
		return nil
	}
	if m, ok := marks[id]; ok {
		return &m
	}
	return nil
}

func (s *Server) resourceMarks(ctx context.Context, typ applemusic.URLType, ids []string) (map[string]domain.ResourceMark, error) {
	if s.store == nil {
		// Catalog-only test servers have no store.
		return map[string]domain.ResourceMark{}, nil
	}
	switch typ {
	case applemusic.TypeAlbum:
		return s.store.RecordMarks(ctx, domain.ResourceAlbum, ids)
	case applemusic.TypePlaylist:
		return s.store.RecordMarks(ctx, domain.ResourcePlaylist, ids)
	case applemusic.TypeSong:
		return s.store.SongMarks(ctx, ids)
	}
	return map[string]domain.ResourceMark{}, nil
}
