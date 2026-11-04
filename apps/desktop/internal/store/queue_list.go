package store

import (
	"context"
	"sort"
	"time"

	"amdl/internal/client"
	"amdl/internal/domain"
)

// queuePage is how many items one read of a queue stream asks for.
const queuePage = 100

// failedFlags are the record flags the Failed tab lists: a record with any
// of them.
var failedFlags = []string{FilterFailed, FilterGivenUp, FilterMissing}

// The streams the queue merges below Up next: amdl's album and playlist
// records, and the runs of stations, which have no record.
const (
	streamAlbums = iota
	streamPlaylists
	streamStations
	streamCount
)

// QueueRow is one row of the queue below Up next: a record, or a station's
// run.
type QueueRow struct {
	Record *Record
	Run    *Job
}

// Key tells rows apart across reads.
func (r QueueRow) Key() string {
	if r.Record != nil {
		return string(r.Record.Ref.Kind) + ":" + r.Record.Ref.ID
	}
	return "run:" + r.Run.ID
}

// at is the row's place in the order: a record's last activity, a run's
// last update, as the backend orders them.
func (r QueueRow) at() time.Time {
	if r.Record != nil {
		return r.Record.ActivityAt
	}
	return r.Run.UpdatedAt
}

// queueStream is one list a tab reads a page at a time, newest activity
// first.
type queueStream struct {
	items []QueueRow
	total int
	// want is how many items a fresh read keeps: the pages read so far.
	want int
}

// ended reports whether the stream holds all its items.
func (st *queueStream) ended() bool { return len(st.items) >= st.total }

// last is the activity of the stream's oldest item read.
func (st *queueStream) last() time.Time {
	if len(st.items) == 0 {
		return time.Time{}
	}
	return st.items[len(st.items)-1].at()
}

func (st *queueStream) sort() {
	sort.SliceStable(st.items, func(a, b int) bool { return st.items[a].at().After(st.items[b].at()) })
}

// tabList is what one tab lists below Up next, as last read.
type tabList struct {
	streams [streamCount]queueStream
	// read is set once a read landed; stale once the records changed in a
	// way the list may not show, so the tab reads again when shown.
	read, stale bool
	Status      SearchStatus
	Err         string
	// More is the next page being read, or the last that failed.
	More PageState
	// views count the fresh reads of each stream that landed, so a next
	// page read before one is dropped.
	views [streamCount]int
	// busy is set while a read is out, again when another was asked for
	// meanwhile.
	busy, again bool
}

// QueueList is what the queue lists below Up next, per tab: amdl's records
// and the stations' runs, newest activity first, read a page at a time
// and kept across reads; the counts of its tabs; the library summary the
// disk check bar reads; and the adds the backend could not read.
type QueueList struct {
	tabs [TabFailed + 1]tabList
	// Summary is nil until read.
	Summary *client.LibrarySummary
	// Adds are the adds the backend could not read ("Couldn't add").
	Adds []domain.PendingAdd
	// done and failed count the Done and Failed tabs' rows, adds included;
	// counted once read.
	done, failed int
	counted      bool
	countBusy    bool
	countAgain   bool
	// countsIn and listIn are set once the first read of the counts, and
	// of a tab, landed or failed.
	countsIn, listIn bool
	open             bool
}

func (q *QueueList) reset() { *q = QueueList{} }

// List is how tab t's list stands.
func (q *QueueList) List(t Tab) *tabList { return &q.tabs[t] }

// OpenQueue follows the queue's lists: it reads the counts and the shown
// tab, and the other tabs once, so a tab shows at once when chosen.
func (s *Store) OpenQueue(shown Tab) {
	q := &s.Queue
	if q.open || s.client == nil {
		return
	}
	q.open = true
	q.count(s)
	for t := TabAll; t <= TabFailed; t++ {
		if t != TabActive && (!q.tabs[t].read || q.tabs[t].stale || t == shown) {
			q.readTab(s, t)
		}
	}
}

// ShowQueueTab reads tab t again when it may be behind, keeping what it
// shows until the answer.
func (s *Store) ShowQueueTab(t Tab) {
	q := &s.Queue
	if q.open && t != TabActive && (!q.tabs[t].read || q.tabs[t].stale) {
		q.readTab(s, t)
	}
}

// QueueCount is the count of the queue's tab t: the queued and running
// runs, or the rows of Done or Failed with the failed adds.
func (s *Store) QueueCount(t Tab) int {
	switch t {
	case TabActive:
		return s.Count(TabActive)
	case TabDone:
		return s.Queue.done
	case TabFailed:
		return s.Queue.failed
	}
	return 0
}

// SetCounts sets the counts of Done and Failed as a read would; tests use
// it.
func (q *QueueList) SetCounts(done, failed int) { q.done, q.failed, q.counted = done, failed, true }

// Counting reports whether a read of the counts is out.
func (q *QueueList) Counting() bool { return q.countBusy }

// Ready reports whether the queue has what its first frame shows: the
// counts and a tab's list, read or failed. A queue that follows no backend
// is ready.
func (q *QueueList) Ready() bool { return !q.open || (q.countsIn && q.listIn) }

// noteLoaded sets Loaded once the snapshot and the queue's first reads are
// in; it stays set.
func (s *Store) noteLoaded() { s.Loaded = s.Loaded || (s.snapshotIn && s.Queue.Ready()) }

// QueueRows are the rows tab t lists after Up next, newest activity first:
// as far as every stream not read to its end goes, so a row read later
// never lands above one shown. more reports whether a next page waits.
// On All, a record whose run is queued or running is in Up next instead,
// and so is a station's run under way.
func (s *Store) QueueRows(t Tab) (rows []QueueRow, more bool) {
	if t == TabActive {
		return nil, false
	}
	l := &s.Queue.tabs[t]
	var edge time.Time
	for i := range l.streams {
		st := &l.streams[i]
		if !st.ended() {
			more = true
			if at := st.last(); at.After(edge) {
				edge = at
			}
		}
	}
	for i := range l.streams {
		for _, r := range l.streams[i].items {
			if r.at().Before(edge) {
				break
			}
			if r.Run != nil {
				if live := s.byID[r.Run.ID]; live != nil {
					r.Run = live
				}
				if t == TabAll && !r.Run.Status.IsTerminal() {
					continue
				}
			} else if t == TabAll && s.LiveRun(r.Record) != nil {
				continue
			}
			rows = append(rows, r)
		}
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].at().After(rows[b].at()) })
	return rows, more
}

// Find is record ref as a tab's list last read it, or nil.
func (q *QueueList) Find(ref domain.ResourceRef) *Record {
	for t := range q.tabs {
		for i := range q.tabs[t].streams[:streamStations] {
			for _, r := range q.tabs[t].streams[i].items {
				if r.Record.Ref == ref {
					return r.Record
				}
			}
		}
	}
	return nil
}

// LiveRun is the queued or running run of record r the store holds, or
// nil: by the record's newest mark, which a download or reset sets before
// the feed sends the record.
func (s *Store) LiveRun(r *Record) *Job {
	if r == nil {
		return nil
	}
	id := r.ActiveJobID
	if m, ok := s.Records.marks[r.Ref]; ok {
		id = m.ActiveJobID
	}
	if j := s.byID[id]; id != "" && j != nil && !j.Status.IsTerminal() {
		return j
	}
	return nil
}

// LastRun is the newest run of record ref the store holds, or nil.
func (s *Store) LastRun(ref domain.ResourceRef) *Job {
	for _, j := range s.Jobs {
		if j.ResourceKind == ref.Kind && j.ResourceID == ref.ID {
			return j
		}
	}
	return nil
}

// MoreQueue reads the next page of tab t's stream whose last item read is
// the newest, the one that holds the list's end back.
func (s *Store) MoreQueue(t Tab) {
	q := &s.Queue
	l := &q.tabs[t]
	if s.client == nil || t == TabActive || !l.read || l.More == PageLoading {
		return
	}
	pick := -1
	for i := range l.streams {
		st := &l.streams[i]
		if !st.ended() && (pick < 0 || st.last().After(l.streams[pick].last())) {
			pick = i
		}
	}
	if pick < 0 {
		return
	}
	st := &l.streams[pick]
	offset := len(st.items)
	l.More, st.want = PageLoading, offset+queuePage
	c, ctx, view := s.client, s.ctx, l.views[pick]
	go func() {
		items, total, err := readStream(ctx, c, pick, t, offset, queuePage)
		s.postIf(ctx, func() { l.applyMore(view, pick, offset, items, total, err) })
	}()
}

// applyMore adds a next page read at offset of stream i, unless a fresh
// read of it landed meanwhile.
func (l *tabList) applyMore(view, i, offset int, items []QueueRow, total int, err error) {
	if view != l.views[i] {
		l.More = PageIdle
		return
	}
	st := &l.streams[i]
	if err != nil {
		l.More, st.want = PageFailed, len(st.items)
		return
	}
	l.More = PageIdle
	if len(st.items) != offset {
		return
	}
	seen := map[string]bool{}
	for _, r := range st.items {
		seen[r.Key()] = true
	}
	added := 0
	for _, r := range items {
		if !seen[r.Key()] {
			st.items = append(st.items, r)
			added++
		}
	}
	st.total = total
	if added == 0 {
		// The list ended early: no further page is asked for.
		st.total = len(st.items)
	}
}

// readTab reads tab t's streams again, as many items of each as it shows,
// keeping what it shows until the answer. A read asked for while one is
// out follows it.
func (q *QueueList) readTab(s *Store, t Tab) {
	l := &q.tabs[t]
	c, ctx := s.client, s.ctx
	if c == nil || t == TabActive {
		return
	}
	if l.busy {
		l.again = true
		return
	}
	l.busy, l.stale = true, false
	if !l.read {
		l.Status, l.Err = SearchLoading, ""
	}
	var want [streamCount]int
	for i := range want {
		want[i] = max(l.streams[i].want, queuePage)
	}
	go func() {
		var streams [streamCount]queueStream
		var err error
		for i := range streams {
			streams[i], err = readStreamPages(ctx, c, i, t, want[i])
			if err != nil {
				break
			}
		}
		s.postIf(ctx, func() {
			l.busy, q.listIn = false, true
			s.noteLoaded()
			switch {
			case err != nil && !l.read:
				l.Status, l.Err = SearchFailed, Reason(err)
			case err != nil:
				// What the tab shows stays; the next change reads again.
				l.stale = true
			default:
				for i := range streams {
					if want[i] < l.streams[i].want {
						// A next page read meanwhile asks for more than this
						// read holds: the stream stays as it is, read again.
						l.again = true
						continue
					}
					l.streams[i] = streams[i]
					l.views[i]++
				}
				l.read, l.Status, l.Err = true, SearchReady, ""
			}
			if l.again {
				l.again = false
				q.readTab(s, t)
			}
		})
	}()
}

// SeedQueue fills every tab's list as its reads would, from records and
// the runs the store holds that have no record, read to their ends, with
// the counts: tests seed a store without a backend with it.
func (s *Store) SeedQueue(records []Record) {
	q := &s.Queue
	q.open, q.counted, q.countsIn, q.listIn, q.done, q.failed = true, true, true, true, 0, 0
	for t := TabAll; t <= TabFailed; t++ {
		l := &q.tabs[t]
		*l = tabList{read: true, Status: SearchReady}
		if t == TabActive {
			continue
		}
		for _, r := range records {
			if recordOnTab(r, t) {
				rec := r
				i := streamAlbums
				if r.Ref.Kind == domain.ResourcePlaylist {
					i = streamPlaylists
				}
				l.streams[i].items = append(l.streams[i].items, QueueRow{Record: &rec})
			}
		}
		for _, j := range s.Jobs {
			if j.ResourceID == "" && (t == TabAll || runOnTab(j, t)) {
				l.streams[streamStations].items = append(l.streams[streamStations].items, QueueRow{Run: j})
			}
		}
		for i := range l.streams {
			st := &l.streams[i]
			st.sort()
			st.total = len(st.items)
			if t == TabDone {
				q.done += st.total
			}
			if t == TabFailed {
				q.failed += st.total
			}
		}
	}
}

// RetryQueue reads tab t again after its first read failed.
func (s *Store) RetryQueue(t Tab) { s.Queue.readTab(s, t) }

// readStreamPages reads the first want items of stream i of tab t, all of
// them when it holds fewer.
func readStreamPages(ctx context.Context, c *client.Client, i int, t Tab, want int) (queueStream, error) {
	st := queueStream{want: want}
	for len(st.items) < want {
		items, total, err := readStream(ctx, c, i, t, len(st.items), min(queuePage, want-len(st.items)))
		if err != nil {
			return st, err
		}
		st.items, st.total = append(st.items, items...), total
		if len(items) == 0 || len(st.items) >= total {
			st.total = len(st.items)
			break
		}
	}
	return st, nil
}

// readStream reads limit items of stream i of tab t from offset, and how
// many the stream holds.
func readStream(ctx context.Context, c *client.Client, i int, t Tab, offset, limit int) ([]QueueRow, int, error) {
	if i == streamStations {
		q := client.ListQuery{Types: []string{"station"}, Sort: "updated_at", Offset: offset, Limit: limit}
		switch t {
		case TabDone:
			q.Status = []domain.JobStatus{domain.JobCompleted}
		case TabFailed:
			q.Status = []domain.JobStatus{domain.JobFailed}
		}
		l, err := c.Downloads(ctx, q)
		var rows []QueueRow
		for _, dj := range l.Downloads {
			// A run of a record is the record's row.
			if dj.ResourceID != "" {
				continue
			}
			j := &Job{Job: dj}
			j.recompute(time.Now())
			rows = append(rows, QueueRow{Run: j})
		}
		return rows, l.Total, err
	}
	kind := domain.ResourceAlbum
	if i == streamPlaylists {
		kind = domain.ResourcePlaylist
	}
	q := client.RecordQuery{Sort: "activity", Offset: offset, Limit: limit}
	switch t {
	case TabDone:
		q.State = []string{FilterComplete}
	case TabFailed:
		q.Flag = failedFlags
	}
	recs, total, err := readRecords(ctx, c, kind, q)
	rows := make([]QueueRow, len(recs))
	for k := range recs {
		rows[k] = QueueRow{Record: &recs[k]}
	}
	return rows, total, err
}

// count reads the counts of the Done and Failed tabs, the library summary
// and the failed adds. One asked for while one is out follows it.
func (q *QueueList) count(s *Store) {
	c, ctx := s.client, s.ctx
	if c == nil {
		return
	}
	if q.countBusy {
		q.countAgain = true
		return
	}
	q.countBusy = true
	go func() {
		sum, sumErr := c.LibrarySummary(ctx)
		adds, addsErr := c.PendingAdds(ctx)
		failed, failedErr := 0, error(nil)
		for _, kind := range []domain.ResourceKind{domain.ResourceAlbum, domain.ResourcePlaylist} {
			_, n, err := readRecords(ctx, c, kind, client.RecordQuery{Flag: failedFlags, Limit: 1})
			failed += n
			failedErr = firstErr(failedErr, err)
		}
		stations := map[domain.JobStatus]int{}
		for _, st := range []domain.JobStatus{domain.JobCompleted, domain.JobFailed} {
			l, err := c.Downloads(ctx, client.ListQuery{Types: []string{"station"}, Status: []domain.JobStatus{st}, Limit: 1})
			stations[st] = l.Total
			failedErr = firstErr(failedErr, err)
		}
		s.postIf(ctx, func() {
			q.countBusy, q.countsIn = false, true
			s.noteLoaded()
			if sumErr == nil {
				q.Summary = &sum
			}
			if addsErr == nil {
				q.Adds = q.Adds[:0]
				for _, p := range adds {
					if p.State == domain.PendingAddFailed {
						q.Adds = append(q.Adds, p)
					}
				}
			}
			if sumErr == nil && addsErr == nil && failedErr == nil {
				q.done = sum.Albums.States[domain.RecordComplete] + sum.Playlists.States[domain.RecordComplete] + stations[domain.JobCompleted]
				q.failed = failed + stations[domain.JobFailed] + len(q.Adds)
				q.counted = true
			}
			if q.countAgain {
				q.countAgain = false
				q.count(s)
			}
		})
	}()
}

func firstErr(was, err error) error {
	if was != nil {
		return was
	}
	return err
}

// recordsChanged reads again what shows amdl's records: the queue's lists
// and counts, and the Library's marks.
func (s *Store) recordsChanged() {
	q := &s.Queue
	for t := range q.tabs {
		q.tabs[t].stale = q.tabs[t].read
	}
	if q.open {
		q.count(s)
		for _, t := range []Tab{TabAll, TabDone, TabFailed} {
			if q.tabs[t].read {
				q.readTab(s, t)
			}
		}
	}
	s.Library.refresh(s)
}

// recordFeed puts a record the feed sent into every tab's list that reads
// it: in its new place when shown, out of a tab it left, into one it
// joined when its activity is within what the tab shows. A change of tab
// reads the counts again.
func (q *QueueList) recordFeed(s *Store, r Record) {
	if !q.open {
		return
	}
	i := streamAlbums
	if r.Ref.Kind == domain.ResourcePlaylist {
		i = streamPlaylists
	}
	moved := false
	for t := TabAll; t <= TabFailed; t++ {
		l := &q.tabs[t]
		if t == TabActive || !l.read {
			continue
		}
		st := &l.streams[i]
		at := -1
		for k, row := range st.items {
			if row.Record.Ref == r.Ref {
				at = k
				break
			}
		}
		belongs := recordOnTab(r, t)
		switch {
		case at >= 0 && belongs:
			was := *st.items[at].Record
			rec := r
			st.items[at].Record = &rec
			st.sort()
			moved = moved || was.State != r.State || failedOf(was) != failedOf(r)
		case at >= 0:
			st.items = append(st.items[:at], st.items[at+1:]...)
			st.total = max(0, st.total-1)
			moved = true
		case belongs && (st.ended() || !r.ActivityAt.Before(st.last())):
			rec := r
			st.items = append(st.items, QueueRow{Record: &rec})
			st.total++
			st.sort()
			moved = true
		}
	}
	if moved {
		q.count(s)
	}
}

// runFeed puts a station's run the feed sent into every tab's list, as
// recordFeed does a record, and drops a deleted one (gone).
func (q *QueueList) runFeed(s *Store, j *Job, gone bool) {
	if !q.open || j.ResourceID != "" || j.Type != "station" {
		return
	}
	moved := false
	for t := TabAll; t <= TabFailed; t++ {
		l := &q.tabs[t]
		if t == TabActive || !l.read {
			continue
		}
		st := &l.streams[streamStations]
		at := -1
		for k, row := range st.items {
			if row.Run.ID == j.ID {
				at = k
				break
			}
		}
		belongs := !gone && runOnTab(j, t)
		switch {
		case at >= 0 && belongs:
			st.items[at].Run = j
			st.sort()
		case at >= 0:
			st.items = append(st.items[:at], st.items[at+1:]...)
			st.total = max(0, st.total-1)
			moved = true
		case belongs:
			st.items = append([]QueueRow{{Run: j}}, st.items...)
			st.total++
			st.sort()
			moved = true
		}
	}
	if moved || gone || j.Status.IsTerminal() {
		q.count(s)
	}
}

// runGone drops a deleted station run the store held only in a list.
func (q *QueueList) runGone(s *Store, id string) {
	for t := range q.tabs {
		for _, r := range q.tabs[t].streams[streamStations].items {
			if r.Run.ID == id {
				q.runFeed(s, r.Run, true)
				return
			}
		}
	}
}

// dropRecord takes a removed record out of every tab's list.
func (q *QueueList) dropRecord(ref domain.ResourceRef) {
	for t := range q.tabs {
		for i := range q.tabs[t].streams[:streamStations] {
			st := &q.tabs[t].streams[i]
			for k, r := range st.items {
				if r.Record.Ref == ref {
					st.items = append(st.items[:k], st.items[k+1:]...)
					st.total = max(0, st.total-1)
					break
				}
			}
		}
	}
}

// recordOnTab reports whether record r belongs on tab t's list.
func recordOnTab(r Record, t Tab) bool {
	switch t {
	case TabDone:
		return r.State == domain.RecordComplete
	case TabFailed:
		return failedOf(r)
	}
	return t == TabAll
}

// failedOf reports whether r has a failed, given-up or missing track.
func failedOf(r Record) bool { return r.TracksFailed+r.TracksGivenUp+r.TracksMissing > 0 }

// runOnTab reports whether a station's run j belongs on tab t's list.
func runOnTab(j *Job, t Tab) bool {
	switch t {
	case TabDone:
		return j.Status == domain.JobCompleted
	case TabFailed:
		return j.Status == domain.JobFailed
	}
	return t == TabAll
}
