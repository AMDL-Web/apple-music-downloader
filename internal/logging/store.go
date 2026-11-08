package logging

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

// Entry is the stable structured representation exposed by the log API.
type Entry struct {
	Sequence   uint64         `json:"sequence"`
	Time       time.Time      `json:"time"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Filter selects retained records. After is an exclusive sequence cursor.
type Filter struct {
	After     uint64
	Levels    []string
	Component string
	RequestID string
	JobID     string
	Query     string
	Since     time.Time
	Until     time.Time
	Limit     int
}

// Page is one List result. NextCursor is the sequence to pass as After for
// the next page; OldestSequence is the oldest retained record (0 when none);
// Truncated reports that After predates the retained records, so records were
// lost between the two pages.
type Page struct {
	Entries        []Entry
	NextCursor     uint64
	OldestSequence uint64
	Truncated      bool
}

// Store keeps the most recent records in a fixed-size ring and fans new
// records out to live subscribers. Sequences are assigned on append, start at
// 1 and have no gaps, so the retained records always hold one contiguous
// sequence range.
type Store struct {
	mu       sync.RWMutex
	capacity int
	next     uint64
	// ring holds the retained records. It grows to capacity, after which
	// head indexes the oldest record and an append overwrites it.
	ring []stored
	head int
	subs map[chan Entry]query
}

// stored is a retained record plus its lowercased search text, computed once
// on append instead of on every Query match.
type stored struct {
	entry  Entry
	search string
}

// query is a Filter with its Query lowercased once.
type query struct {
	Filter
	needle string
}

func newQuery(filter Filter) query {
	return query{Filter: filter, needle: strings.ToLower(filter.Query)}
}

// NewStore returns a Store that retains up to capacity records; 0 retains
// none but still streams to subscribers.
func NewStore(capacity int) *Store {
	return &Store{capacity: capacity, ring: make([]stored, 0, max(capacity, 0)), subs: map[chan Entry]query{}}
}

func (s *Store) append(entry Entry) {
	item := stored{entry: entry, search: searchText(entry)}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	item.entry.Sequence = s.next
	if s.capacity > 0 {
		if len(s.ring) < s.capacity {
			s.ring = append(s.ring, item)
		} else {
			s.ring[s.head] = item
			s.head = (s.head + 1) % s.capacity
		}
	}
	for ch, q := range s.subs {
		if !matches(item, q) {
			continue
		}
		select {
		case ch <- item.entry:
		default:
			// Force a slow client to reconnect with its last received sequence;
			// silently dropping here would leave an undetectable hole in its stream.
			delete(s.subs, ch)
			close(ch)
		}
	}
}

// List returns matching records in chronological order. A first request tails
// the newest Limit records; a cursor request pages forward from After without
// skipping matches. Truncated reports that After predates the retained ring.
func (s *Store) List(filter Filter) Page {
	q := newQuery(filter)
	s.mu.RLock()
	defer s.mu.RUnlock()
	page := Page{NextCursor: s.next}
	if len(s.ring) > 0 {
		page.OldestSequence = s.at(0).entry.Sequence
		page.Truncated = filter.After > 0 && filter.After < page.OldestSequence-1
	} else if filter.After > 0 && filter.After < s.next {
		page.Truncated = true
	}
	if filter.After > 0 && filter.Limit > 0 {
		entries := s.oldestMatches(q, filter.Limit+1)
		if len(entries) > filter.Limit {
			entries = entries[:filter.Limit]
			page.NextCursor = entries[len(entries)-1].Sequence
		}
		page.Entries = entries
		return page
	}
	page.Entries = s.newestMatches(q, filter.Limit)
	return page
}

// Subscribe atomically returns the matching retained backlog and registers a
// best-effort live subscriber, so no record can fall between replay and live.
// Without a cursor the backlog is the newest Limit matches; with one it is
// every retained match after it.
func (s *Store) Subscribe(filter Filter) ([]Entry, <-chan Entry, func()) {
	q := newQuery(filter)
	limit := filter.Limit
	if filter.After > 0 {
		limit = 0
	}
	ch := make(chan Entry, 256)
	s.mu.Lock()
	backlog := s.newestMatches(q, limit)
	s.subs[ch] = q
	s.mu.Unlock()

	return backlog, ch, func() {
		s.mu.Lock()
		if _, ok := s.subs[ch]; ok {
			delete(s.subs, ch)
			close(ch)
		}
		s.mu.Unlock()
	}
}

// at returns the i-th oldest retained record. The caller holds mu.
func (s *Store) at(i int) stored {
	return s.ring[(s.head+i)%len(s.ring)]
}

// oldestMatches returns up to limit matches (all when limit <= 0) in
// chronological order, starting just after q.After. The caller holds mu.
func (s *Store) oldestMatches(q query, limit int) []Entry {
	start := 0
	if len(s.ring) > 0 {
		// Retained sequences are contiguous, so the first record after the
		// cursor sits at a computable offset.
		if oldest := s.at(0).entry.Sequence; q.After >= oldest {
			start = int(min(q.After-oldest+1, uint64(len(s.ring))))
		}
	}
	out := make([]Entry, 0, max(min(limit, len(s.ring)-start), 0))
	for i := start; i < len(s.ring) && (limit <= 0 || len(out) < limit); i++ {
		if item := s.at(i); matches(item, q) {
			out = append(out, item.entry)
		}
	}
	return out
}

// newestMatches returns the newest limit matches (all when limit <= 0) in
// chronological order. The caller holds mu.
func (s *Store) newestMatches(q query, limit int) []Entry {
	if limit <= 0 {
		return s.oldestMatches(q, 0)
	}
	out := make([]Entry, 0, min(limit, len(s.ring)))
	for i := len(s.ring) - 1; i >= 0 && len(out) < limit; i-- {
		item := s.at(i)
		if item.entry.Sequence <= q.After {
			break
		}
		if matches(item, q) {
			out = append(out, item.entry)
		}
	}
	slices.Reverse(out)
	return out
}

func matches(item stored, q query) bool {
	entry := item.entry
	if entry.Sequence <= q.After || (!q.Since.IsZero() && entry.Time.Before(q.Since)) || (!q.Until.IsZero() && entry.Time.After(q.Until)) {
		return false
	}
	if len(q.Levels) > 0 && !slices.Contains(q.Levels, entry.Level) {
		return false
	}
	if q.Component != "" && attributeString(entry, "component") != q.Component {
		return false
	}
	if q.RequestID != "" && attributeString(entry, "request_id") != q.RequestID {
		return false
	}
	if q.JobID != "" && attributeString(entry, "job_id") != q.JobID {
		return false
	}
	if q.needle != "" && !strings.Contains(item.search, q.needle) {
		return false
	}
	return true
}

func attributeString(entry Entry, key string) string {
	value, _ := entry.Attributes[key].(string)
	return value
}

// searchText is what Filter.Query matches against: the message and the
// attributes as JSON, lowercased. If the attributes do not marshal (a NaN
// float, say), only the message is searchable; the record itself is kept.
func searchText(entry Entry) string {
	raw, _ := json.Marshal(entry.Attributes)
	return strings.ToLower(entry.Message + " " + string(raw))
}
