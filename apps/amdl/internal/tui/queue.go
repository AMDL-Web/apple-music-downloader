package tui

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// queueEdit is the /queue panel: the queued jobs in the order the server
// starts them, reordered with the keys of /quality.
type queueEdit struct {
	id string // the job the cursor is on, so it stays on it as the queue changes
	at int    // the cursor's row, for when that job leaves the queue
}

// queueMoves are the moves the server has yet to take. While any is in
// flight, and until the queue is read back after the last, the cards and
// the panel show order, the queue as the person left it: the server may
// renumber the whole queue for one move, and the feed brings the new values
// one job at a time.
type queueMoves struct {
	order   []string
	pending []pendingMove // oldest first
	busy    bool          // a move, or the read after the last one, is in flight
}

type pendingMove struct {
	id   string
	down bool
}

type movedMsg struct {
	id  string
	job domain.Job
	err error
}

type queueReadMsg struct {
	jobs []domain.Job
	err  error
}

// queuedIDs are the queued jobs in the order the server starts them, or as
// the person left them while moves are in flight.
func (m *model) queuedIDs() []string {
	var ids []string
	for id, st := range m.jobs {
		if st.job.Status == domain.JobQueued {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, k int) bool {
		x, y := m.jobs[ids[i]].job.QueueOrder, m.jobs[ids[k]].job.QueueOrder
		switch {
		case x != nil && y != nil && *x != *y:
			return *x < *y
		case (x == nil) != (y == nil):
			return x != nil
		}
		return m.num[ids[i]] < m.num[ids[k]]
	})
	if m.moves.order == nil {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range m.moves.order {
		if slices.Contains(ids, id) {
			out = append(out, id)
		}
	}
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id) // queued since, behind the rest
		}
	}
	return out
}

func (m *model) openQueue() tea.Cmd {
	ids := m.queuedIDs()
	switch len(ids) {
	case 0:
		return m.say("nothing is queued")
	case 1:
		return m.say(m.ref(ids[0]) + " is the only job queued")
	}
	m.queue, m.panel = &queueEdit{id: ids[0]}, panelQueue
	return nil
}

// row is the cursor's row in ids.
func (q *queueEdit) row(ids []string) int {
	if i := slices.Index(ids, q.id); i >= 0 {
		return i
	}
	return max(0, min(q.at, len(ids)-1))
}

// queueKey moves through the /queue panel: ↑↓ the row, home and end the
// first and last, shift+↑↓ moves a job one place, shift+home and shift+end
// to the front or the back, esc or enter closes. A move goes to the server
// at once.
func (m *model) queueKey(key string) (tea.Cmd, bool) {
	q := m.queue
	if m.input.Value() != "" {
		return nil, false
	}
	ids := m.queuedIDs()
	at := q.row(ids)
	to := at
	switch key {
	case "up", "shift+up":
		to = max(0, at-1)
	case "down", "shift+down":
		to = min(len(ids)-1, at+1)
	case "home", "shift+home":
		to = 0
	case "end", "shift+end":
		to = len(ids) - 1
	case "esc", "enter":
		m.queue, m.panel = nil, panelNone
		return nil, true
	default:
		return nil, false
	}
	if len(ids) == 0 {
		return nil, true
	}
	if !strings.HasPrefix(key, "shift+") {
		q.id, q.at = ids[to], to
		return nil, true
	}
	q.id, q.at = ids[at], to
	if to == at {
		return nil, true
	}
	order := slices.Insert(slices.Delete(slices.Clone(ids), at, at+1), to, ids[at])
	m.moves.order = order
	p := pendingMove{id: ids[at], down: to > at}
	if i := slices.IndexFunc(m.moves.pending, func(x pendingMove) bool { return x.id == p.id }); i >= 0 {
		m.moves.pending[i] = p
	} else {
		m.moves.pending = append(m.moves.pending, p)
	}
	return m.nextMove(), true
}

// nextMove sends the oldest pending move, one at a time so the server takes
// them in order: the job goes first or last at the ends, else before the job
// it went above or after the one it went below. Once none is left, the queue
// is read back.
func (m *model) nextMove() tea.Cmd {
	if m.moves.busy {
		return nil
	}
	ids := m.queuedIDs()
	for len(m.moves.pending) > 0 {
		p := m.moves.pending[0]
		m.moves.pending = m.moves.pending[1:]
		i := slices.Index(ids, p.id)
		if i < 0 {
			continue // started, or gone, since it moved
		}
		var req domain.MoveRequest
		switch {
		case i == 0:
			req.To = "first"
		case i == len(ids)-1:
			req.To = "last"
		case p.down:
			req.After = ids[i-1]
		default:
			req.Before = ids[i+1]
		}
		m.moves.busy = true
		ctx, c := m.ctx, m.c
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			j, err := c.Move(ctx, p.id, req)
			return movedMsg{id: p.id, job: j, err: err}
		}
	}
	m.moves.busy = true
	return readQueue(m.ctx, m.c)
}

// onMoved takes the server's answer to a move. A refusal drops the local
// order at once, back to the server's, and reads the queue again: a
// job_not_queued means the queue changed under the move.
func (m *model) onMoved(msg movedMsg) tea.Cmd {
	m.moves.busy = false
	if msg.err != nil {
		m.moves = queueMoves{}
		say := m.fail(msg.err)
		if isAPIError(msg.err, "job_not_queued") {
			say = m.say(m.ref(msg.id) + " did not move: the queue changed meanwhile")
		}
		return tea.Batch(say, m.nextMove())
	}
	if st := m.jobs[msg.id]; st != nil && st.job.Status == domain.JobQueued && msg.job.QueueOrder != nil {
		st.job.QueueOrder = msg.job.QueueOrder
	}
	return m.nextMove()
}

func readQueue(ctx context.Context, c *client.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		l, err := c.Downloads(ctx, client.ListQuery{Status: []domain.JobStatus{domain.JobQueued}, Sort: "queue_order", Ascending: true, Limit: 200})
		return queueReadMsg{jobs: l.Downloads, err: err}
	}
}

// onQueueRead takes the queue's values as the server has them now, and
// drops the local order unless the person moved a job during the read.
func (m *model) onQueueRead(msg queueReadMsg) tea.Cmd {
	m.moves.busy = false
	for _, j := range msg.jobs {
		if st := m.jobs[j.ID]; st != nil && j.Status == domain.JobQueued && st.job.Status == domain.JobQueued {
			st.job.QueueOrder = j.QueueOrder
		}
	}
	if len(m.moves.pending) > 0 {
		return m.nextMove()
	}
	m.moves.order = nil
	return nil
}

func (m *model) queueView(w, room int) string {
	t := m.theme
	ids := m.queuedIDs()
	what := fmt.Sprintf(" · %d waiting", len(ids))
	if len(ids) == 0 {
		what = " · nothing waiting"
	}
	lines := []string{"  " + t.style(t.fg).Render("queue") + t.style(t.dim).Render(what)}
	at := m.queue.row(ids)
	n := min(len(ids), max(1, room-1))
	start := max(0, at-n+1)
	for i := start; i < start+n && i < len(ids); i++ {
		j := m.jobs[ids[i]].job
		rs := rowStyle{t, i == at}
		mark := "  "
		if rs.sel {
			mark = "▸ "
		}
		ref := rs.s(t.dim).Render(" " + m.ref(j.ID))
		lead := "  " + rs.s(t.accent).Render(mark) + rs.s(t.dim).Render(pad(fmt.Sprint(i+1), 3))
		nameW := max(4, w-lipgloss.Width(lead)-lipgloss.Width(ref)-1)
		name := rs.bold(t.fg).Render(trunc(jobName(j), nameW))
		if j.ArtistName != "" && w >= mediumCols {
			used := min(lipgloss.Width(jobName(j)), nameW)
			if rest := nameW - used - 3; rest >= 8 {
				name += rs.s(t.dim).Render(" · " + trunc(j.ArtistName, rest))
			}
		}
		lines = append(lines, rs.fill(rs.fill(lead+name, w-lipgloss.Width(ref))+ref, w))
	}
	return strings.Join(lines, "\n")
}
