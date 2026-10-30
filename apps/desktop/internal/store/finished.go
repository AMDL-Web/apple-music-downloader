package store

import (
	"sort"
	"time"

	"amdl/internal/domain"
)

// Finished returns the feed's completed downloads, the one that finished
// last first: the queue shows it large while nothing downloads, and the
// ones before it as a row of covers.
func (s *Store) Finished() []*Job {
	var out []*Job
	for _, j := range s.Jobs {
		if j.Status == domain.JobCompleted {
			out = append(out, j)
		}
	}
	// The feed lists jobs by creation, so a job retried long after it was
	// added would otherwise sit far down.
	sort.SliceStable(out, func(a, b int) bool { return finishedAt(out[a]).After(finishedAt(out[b])) })
	return out
}

func finishedAt(j *Job) time.Time {
	if j.FinishedAt != nil {
		return *j.FinishedAt
	}
	return j.UpdatedAt
}

// WantTracks reads the tracks of j, which the feed lists without them,
// once: the latest download shows its quality, size and folder from them.
func (s *Store) WantTracks(j *Job) {
	if len(j.Items) > 0 || s.tracksRead[j.ID] || s.client == nil {
		return
	}
	s.tracksRead[j.ID] = true
	id, c, ctx := j.ID, s.client, s.ctx
	go func() {
		d, err := c.Download(ctx, id)
		s.postIf(ctx, func() {
			// The hero leaves its facts out rather than show an error.
			if err == nil && s.byID[id] != nil {
				s.applyDetail(id, d)
			}
		})
	}()
}
