package store

import (
	"context"
	"encoding/json"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// Records is what the store knows of amdl's records beyond what an answer
// carried: each record's newest mark, from the feed's resource_updated and
// from submits. The records decide what is downloaded; nothing here reads
// the disk.
type Records struct {
	marks map[domain.ResourceRef]domain.ResourceMark
	// gone holds the records removed since an answer named them, so a
	// cached answer's mark of one reads as none.
	gone map[domain.ResourceRef]bool
}

func (r *Records) reset() {
	*r = Records{marks: map[domain.ResourceRef]domain.ResourceMark{}, gone: map[domain.ResourceRef]bool{}}
}

// applyResource keeps a resource_updated message's record as its newest
// mark, reads the open album again when its tracks moved on, and moves the
// record's row in the queue.
func (s *Store) applyResource(m domain.DownloadFeedMessage) {
	if m.ResourceKind == "" || m.ResourceID == "" || m.Resource == nil {
		return
	}
	raw, err := json.Marshal(m.Resource)
	if err != nil {
		return
	}
	var r Record
	switch m.ResourceKind {
	case domain.ResourceAlbum:
		var a client.AlbumRecord
		err = json.Unmarshal(raw, &a)
		r = albumRecord(a)
	case domain.ResourcePlaylist:
		var p client.PlaylistRecord
		err = json.Unmarshal(raw, &p)
		r = playlistRecord(p)
	default:
		return
	}
	if err != nil {
		// A record the store cannot read keeps its last mark.
		return
	}
	r.Ref.ID = m.ResourceID
	s.putMark(*r.Mark())
	s.Queue.recordFeed(s, r)
}

// putMark keeps mark as its record's newest, and moves what shows the
// record when it changed.
func (s *Store) putMark(mark domain.ResourceMark) {
	ref := domain.ResourceRef{Kind: mark.Kind, ID: mark.ID}
	old, had := s.Records.marks[ref]
	s.Records.marks[ref] = mark
	delete(s.Records.gone, ref)
	if had && old == mark {
		return
	}
	if ref.Kind == domain.ResourceAlbum {
		s.albumMoved(ref.ID)
	}
	s.Library.recordMoved(ref)
}

// forgetRecord drops what the store holds of a removed record: its mark,
// its Library keep and its queue rows; an answer's mark of it reads as
// none from now on, and the open album and the queue counts are read again.
func (s *Store) forgetRecord(ref domain.ResourceRef) {
	delete(s.Records.marks, ref)
	s.Records.gone[ref] = true
	delete(s.Library.Kept, ref.ID)
	s.Queue.dropRecord(ref)
	if ref.Kind == domain.ResourceAlbum {
		s.albumMoved(ref.ID)
	}
	if s.Queue.open {
		s.Queue.count(s)
	}
}

// noteSubmitted marks the records a submit added with the run it queued,
// before the feed says so.
func (s *Store) noteSubmitted(res domain.BatchSubmitResponse) {
	for _, r := range res.Results {
		if r.Resource == nil {
			continue
		}
		job := ""
		switch {
		case r.Status == domain.SubmitAccepted && r.Job != nil:
			job = r.Job.ID
		case r.Status == domain.SubmitDuplicateActive:
			job = r.ExistingJobID
		}
		if job == "" {
			continue
		}
		mark := s.Records.marks[*r.Resource]
		mark.Kind, mark.ID, mark.ActiveJobID = r.Resource.Kind, r.Resource.ID, job
		s.Records.marks[*r.Resource] = mark
		delete(s.Records.gone, *r.Resource)
	}
}

// Latest is r with the newest news of its record: the feed's fields over
// those an answer carried, keeping a song's SongState from the answer. nil
// for nil.
func (s *Store) Latest(r *domain.ResourceMark) *domain.ResourceMark {
	if r == nil {
		return nil
	}
	ref := domain.ResourceRef{Kind: r.Kind, ID: r.ID}
	if s.Records.gone[ref] {
		if _, ok := s.Records.marks[ref]; !ok {
			return nil
		}
	}
	out := *r
	if fresh, ok := s.Records.marks[domain.ResourceRef{Kind: r.Kind, ID: r.ID}]; ok {
		song := out.SongState
		out = fresh
		out.SongState = song
	}
	// A run the feed already ended is no longer the record's.
	if j := s.byID[out.ActiveJobID]; j != nil && j.Status.IsTerminal() {
		out.ActiveJobID = ""
	}
	return &out
}

// RecordMark is the newest mark the store holds of record kind id, nil when
// no answer or message named it.
func (s *Store) RecordMark(kind domain.ResourceKind, id string) *domain.ResourceMark {
	if m, ok := s.Records.marks[domain.ResourceRef{Kind: kind, ID: id}]; ok {
		return &m
	}
	return nil
}

// MarkOf is how a record mark reads, and the run it names while one is
// queued or running. song reads a song's own state in its album's record:
// done is in the library, a slot still to download is in the queue or
// paused with its album, anything else marks nothing. tracks is the
// album's track count where the caller knows it (a hit's), else 0: an
// album record that wants only some of its tracks reads partial while it
// holds fewer than the album's (MarkTotal), so its Download adds the rest.
func MarkOf(r *domain.ResourceMark, song bool, tracks int) (MarkState, string) {
	if r == nil {
		return MarkNone, ""
	}
	if song {
		switch r.SongState {
		case domain.AudioDone:
			return MarkDone, ""
		case domain.AudioPending, domain.AudioRunning, domain.AudioFailed:
			if r.ActiveJobID != "" {
				return MarkActive, r.ActiveJobID
			}
			if r.Paused {
				return MarkPaused, ""
			}
		}
		return MarkNone, ""
	}
	switch {
	case r.ActiveJobID != "":
		return MarkActive, r.ActiveJobID
	case r.Paused && r.State != domain.RecordComplete:
		return MarkPaused, ""
	case r.State == domain.RecordComplete && r.TracksDone < MarkTotal(r, tracks):
		return MarkPartial, ""
	case r.State == domain.RecordComplete:
		return MarkDone, ""
	case r.State == domain.RecordPartial:
		return MarkPartial, ""
	}
	return MarkNone, ""
}

// MarkTotal is how many tracks a record mark's done ones are out of: its
// wanted tracks, or, for an album record that wants only the tracks added
// one by one, the album's available ones when tracks (its track count) is
// known.
func MarkTotal(r *domain.ResourceMark, tracks int) int {
	if r == nil {
		return 0
	}
	if r.Kind == domain.ResourceAlbum && !r.WantedAll && tracks > 0 {
		return max(r.TracksWanted, tracks-r.WithheldCount)
	}
	return r.TracksWanted
}

// HitRecord is the record a hit's mark reads, with the newest news: an
// album's or playlist's own, a song's album record. An album or playlist
// the search found without a record has one once the feed names it.
func (s *Store) HitRecord(h client.SearchHit) *domain.ResourceMark {
	if h.Resource != nil {
		return s.Latest(h.Resource)
	}
	switch h.Type {
	case "album":
		return s.Latest(s.RecordMark(domain.ResourceAlbum, h.ID))
	case "playlist":
		return s.Latest(s.RecordMark(domain.ResourcePlaylist, h.ID))
	}
	return nil
}

// Pause stops record ref: its run is cancelled with its partial downloads
// kept, and nothing queues it until Resume.
func (s *Store) Pause(ref domain.ResourceRef) {
	s.markAction(ref, (*client.Client).PauseRecord, "Paused")
}

// Resume lets record ref download again.
func (s *Store) Resume(ref domain.ResourceRef) {
	s.markAction(ref, (*client.Client).ResumeRecord, "Resumed")
}

// markAction runs a record action that answers the record's mark, and
// keeps the mark.
func (s *Store) markAction(ref domain.ResourceRef, act func(*client.Client, context.Context, domain.ResourceKind, string) (domain.ResourceMark, error), done string) {
	s.recordAction(ref, func(c *client.Client) error {
		m, err := act(c, s.ctx, ref.Kind, ref.ID)
		if err == nil && m.ID != "" {
			s.post(func() { s.putMark(m) })
		}
		return err
	}, done)
}

// recordAction runs act against the backend and says done, or why not.
// The feed's resource_updated brings the record's new mark.
func (s *Store) recordAction(ref domain.ResourceRef, act func(*client.Client) error, done string) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	go func() {
		err := act(c)
		s.postIf(ctx, func() {
			if err != nil {
				s.setNotice(err.Error(), true)
				return
			}
			if done != "" {
				s.setNotice(done, false)
			}
			s.recordsChanged()
		})
	}()
}

// Reset downloads again what the record ref holds: the main files of songs
// (every track when empty), with codecs as their quality order (nil keeps
// each slot's). A record with a queued or running run is refused: pause
// it first.
func (s *Store) Reset(ref domain.ResourceRef, songs, codecs []string, name string) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	r := client.RecordReset{What: "audio", Songs: songs}
	if codecs != nil {
		r.Slot, r.Codecs = domain.SlotMain, codecs
	}
	go func() {
		job, err := c.ResetRecord(ctx, ref.Kind, ref.ID, r)
		s.postIf(ctx, func() {
			switch {
			case err != nil:
				s.setNotice(err.Error(), true)
			case job != nil:
				s.upsert(*job)
				mark := s.Records.marks[ref]
				mark.Kind, mark.ID, mark.ActiveJobID = ref.Kind, ref.ID, job.ID
				s.Records.marks[ref] = mark
				s.setNotice("Downloading "+name+" again", false)
			default:
				s.setNotice(name+" waits to download again", false)
			}
			s.recordsChanged()
		})
	}()
}

// runAnswerNotice is what a record download or a retry that queued nothing
// says.
func runAnswerNotice(a client.RunAnswer) string {
	switch a.Status {
	case "already_downloaded":
		return "Already in your library"
	case "waiting":
		return "Waits for its next try"
	}
	return ""
}
