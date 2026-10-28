// Package events is the in-process wake signal for event streams. A Hub
// carries no events: a subscriber is told only that something it watches
// changed, and re-reads the store, which is the source of truth.
package events

import (
	"sync"

	"amdl/internal/domain"
)

// Hub fans wake signals out to per-job and overview subscribers. The zero
// value is not usable; call NewHub.
type Hub struct {
	mu      sync.RWMutex
	subs    map[string]map[chan struct{}]struct{}
	allSubs map[chan struct{}]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{
		subs:    map[string]map[chan struct{}]struct{}{},
		allSubs: map[chan struct{}]struct{}{},
	}
}

// Subscribe registers a wake channel for events of jobID. The channel has one
// slot and wakes coalesce: any number of events published while the
// subscriber is busy leave exactly one pending wake, so a subscriber must
// drain everything newer than its cursor from the store each time it wakes.
// cancel unregisters and closes the channel; it is safe to call more than
// once.
func (h *Hub) Subscribe(jobID string) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = map[chan struct{}]struct{}{}
	}
	h.subs[jobID][ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if subs := h.subs[jobID]; subs != nil {
				delete(subs, ch)
				if len(subs) == 0 {
					delete(h.subs, jobID)
				}
			}
			close(ch)
		})
	}
}

// SubscribeAll registers an overview wake channel that wakes on events of any
// job whose type is an overview milestone (domain.IsOverviewMilestone). It
// coalesces and cancels like Subscribe.
func (h *Hub) SubscribeAll() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.allSubs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			delete(h.allSubs, ch)
			close(ch)
		})
	}
}

// Publish wakes the subscribers of ev's job and, for an overview milestone,
// the overview subscribers. Call it after ev is persisted. It never blocks: a
// subscriber that already has a wake pending is skipped.
func (h *Hub) Publish(ev domain.Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[ev.JobID] {
		wake(ch)
	}
	if domain.IsOverviewMilestone(ev.Type) {
		for ch := range h.allSubs {
			wake(ch)
		}
	}
}

// wake must run under h.mu (read or write) so it never races cancel's close.
func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
