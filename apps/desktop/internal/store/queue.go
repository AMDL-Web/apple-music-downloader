package store

import (
	"slices"
	"sort"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueMove is a move waiting for the backend's answer, and the order of
// the queued jobs the screen shows meanwhile.
type queueMove struct {
	gen   int
	order []string
}

// Queued returns the queued jobs in the order workers take them: the
// lowest queue_order first. A move waiting for its answer already shows in
// its new place.
func (s *Store) Queued() []*Job {
	var out []*Job
	for _, j := range s.Jobs {
		if j.Status == domain.JobQueued {
			out = append(out, j)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return runsBefore(out[a], out[b]) })
	m := s.move
	if m == nil {
		return out
	}
	at := make(map[string]int, len(m.order))
	for i, id := range m.order {
		at[id] = i
	}
	// A job queued since the move goes after those it placed.
	sort.SliceStable(out, func(a, b int) bool {
		ia, oka := at[out[a].ID]
		ib, okb := at[out[b].ID]
		if oka && okb {
			return ia < ib
		}
		return oka && !okb
	})
	return out
}

// runsBefore orders queued jobs as the backend's workers take them. Values
// of queue_order only compare; a job without one goes last.
func runsBefore(a, b *Job) bool {
	switch qa, qb := a.QueueOrder, b.QueueOrder; {
	case qa != nil && qb != nil && *qa != *qb:
		return *qa < *qb
	case (qa == nil) != (qb == nil):
		return qa != nil
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

// MoveJob puts queued job id at place to among the other queued jobs, on
// screen at once, and asks the backend to: first, last, or before the job
// now at to. When the queue changed meanwhile (job_not_queued), the screen
// goes back to the backend's order.
func (s *Store) MoveJob(id string, to int) {
	queued := s.Queued()
	ids := make([]string, len(queued))
	for i, j := range queued {
		ids[i] = j.ID
	}
	from := slices.Index(ids, id)
	if from < 0 || s.client == nil {
		return
	}
	rest := slices.Delete(slices.Clone(ids), from, from+1)
	to = max(0, min(to, len(rest)))
	order := slices.Insert(slices.Clone(rest), to, id)
	if slices.Equal(order, ids) {
		return
	}
	var req domain.MoveRequest
	switch {
	case to == 0:
		req.To = "first"
	case to == len(rest):
		req.To = "last"
	default:
		req.Before = rest[to]
	}
	s.moveGen++
	s.move = &queueMove{gen: s.moveGen, order: order}
	gen, c, ctx := s.moveGen, s.client, s.ctx
	go func() {
		j, err := c.Move(ctx, id, req)
		s.postIf(ctx, func() { s.applyMove(gen, j, err) })
	}()
}

func (s *Store) applyMove(gen int, j domain.Job, err error) {
	// A later move keeps its order on screen until its own answer.
	if s.move != nil && s.move.gen == gen {
		s.move = nil
	}
	switch {
	case err == nil:
		s.upsert(j)
	case client.ErrorCode(err) == "job_not_queued":
		s.setNotice("The queue changed, so nothing moved", true)
		s.readQueue()
	default:
		s.setNotice(err.Error(), true)
	}
}

// readQueue reads the queued jobs again, as they stand on the backend.
func (s *Store) readQueue() {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	go func() {
		l, err := c.Downloads(ctx, client.ListQuery{Status: []domain.JobStatus{domain.JobQueued}, Sort: "queue_order", Ascending: true, Limit: client.SnapshotLimit})
		s.postIf(ctx, func() {
			// A failed read leaves the order to the feed's next upserts.
			if err != nil {
				return
			}
			for _, dj := range l.Downloads {
				s.upsert(dj)
			}
		})
	}()
}
