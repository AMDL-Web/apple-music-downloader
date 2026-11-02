package store

import (
	"context"
	"encoding/json"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// Records is what the store knows of amdl's records beyond what an answer
// carried: each record's newest mark, from the feed's resource_updated and
// from submits, and the disk check the feed's check_updated moves on. The
// records decide what is downloaded; nothing here reads the disk.
type Records struct {
	marks map[domain.ResourceRef]domain.ResourceMark
	// Check is the running or last disk check; nil until one is read or
	// sent.
	Check *domain.CheckRun
}

func (r *Records) reset() { *r = Records{marks: map[domain.ResourceRef]domain.ResourceMark{}} }

// feedRecord is what a mark reads of a record as GET /library/{kind} lists
// it, which resource_updated carries.
type feedRecord struct {
	domain.Rollup
	WithheldCount int    `json:"withheld_count"`
	Paused        bool   `json:"paused"`
	ActiveJobID   string `json:"active_job_id"`
}

// applyResource keeps a resource_updated message's record as its newest
// mark, and reads the open album again when its tracks moved on.
func (s *Store) applyResource(m domain.DownloadFeedMessage) {
	if m.ResourceKind == "" || m.ResourceID == "" || m.Resource == nil {
		return
	}
	raw, err := json.Marshal(m.Resource)
	var r feedRecord
	if err != nil || json.Unmarshal(raw, &r) != nil {
		// A record the store cannot read keeps its last mark.
		return
	}
	s.putMark(domain.ResourceMark{Kind: m.ResourceKind, ID: m.ResourceID, State: r.State, TracksDone: r.TracksDone,
		TracksWanted: r.TracksWanted, WithheldCount: r.WithheldCount, ActiveJobID: r.ActiveJobID, Paused: r.Paused})
}

// putMark keeps mark as its record's newest, and moves what shows the
// record when it changed.
func (s *Store) putMark(mark domain.ResourceMark) {
	ref := domain.ResourceRef{Kind: mark.Kind, ID: mark.ID}
	old, had := s.Records.marks[ref]
	s.Records.marks[ref] = mark
	if had && old == mark {
		return
	}
	if ref.Kind == domain.ResourceAlbum {
		s.albumMoved(ref.ID)
	}
	s.Library.recordMoved(s, ref, mark)
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
	}
}

// Latest is r with the newest news of its record: the feed's fields over
// those an answer carried, keeping a song's SongState from the answer. nil
// for nil.
func (s *Store) Latest(r *domain.ResourceMark) *domain.ResourceMark {
	if r == nil {
		return nil
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
// paused with its album, anything else marks nothing.
func MarkOf(r *domain.ResourceMark, song bool) (MarkState, string) {
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
	case r.State == domain.RecordComplete:
		return MarkDone, ""
	case r.State == domain.RecordPartial:
		return MarkPartial, ""
	}
	return MarkNone, ""
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
			s.Library.refresh(s)
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
			s.Library.refresh(s)
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
