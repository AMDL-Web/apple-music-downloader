package events

import (
	"sync"
	"testing"
	"time"

	"amdl/internal/domain"
)

func received(ch <-chan struct{}) bool {
	select {
	case _, ok := <-ch:
		return ok
	default:
		return false
	}
}

func TestHubWakesSubscriberOfTheJob(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("job-1")
	defer cancel()
	other, cancelOther := hub.Subscribe("job-2")
	defer cancelOther()

	hub.Publish(domain.Event{ID: 1, JobID: "job-1", Type: domain.EventItemProgress})

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("subscriber of job-1 was not woken")
	}
	if received(other) {
		t.Fatal("subscriber of job-2 was woken by a job-1 event")
	}
}

func TestHubWakeWithoutAnEvent(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("job-1")
	defer cancel()
	all, cancelAll := hub.SubscribeAll()
	defer cancelAll()
	hub.Wake("job-1")
	if !received(ch) {
		t.Fatal("subscriber of job-1 was not woken")
	}
	if received(all) {
		t.Fatal("an overview subscriber was woken by a per-job wake")
	}
}

func TestHubCoalescesWakes(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("job-1")
	defer cancel()

	for i := 0; i < 5; i++ {
		hub.Publish(domain.Event{ID: int64(i), JobID: "job-1", Type: domain.EventItemProgress})
	}
	if !received(ch) {
		t.Fatal("no wake pending after five publishes")
	}
	if received(ch) {
		t.Fatal("five publishes left more than one pending wake")
	}
	hub.Publish(domain.Event{ID: 6, JobID: "job-1", Type: domain.EventItemCompleted})
	if !received(ch) {
		t.Fatal("a publish after the wake was consumed did not wake again")
	}
}

func TestHubOverviewWakesOnlyOnMilestones(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.SubscribeAll()
	defer cancel()

	hub.Publish(domain.Event{ID: 1, JobID: "job-1", Type: domain.EventItemProgress})
	hub.Publish(domain.Event{ID: 2, JobID: "job-1", Type: domain.EventCodecSelected})
	if received(ch) {
		t.Fatal("overview subscriber woken by a per-item detail event")
	}
	hub.Publish(domain.Event{ID: 3, JobID: "job-2", Type: domain.EventJobDeleted})
	if !received(ch) {
		t.Fatal("overview subscriber not woken by job_deleted")
	}
}

func TestHubCancelIsIdempotentAndClosesChannel(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("job-1")
	all, cancelAll := hub.SubscribeAll()
	cancel()
	cancel()
	cancelAll()
	cancelAll()

	if _, ok := <-ch; ok {
		t.Fatal("job channel still open after cancel")
	}
	if _, ok := <-all; ok {
		t.Fatal("overview channel still open after cancel")
	}
	// Publishing after cancel must not panic on a closed channel.
	hub.Publish(domain.Event{ID: 1, JobID: "job-1", Type: domain.EventJobFinished})
	if len(hub.subs) != 0 || len(hub.allSubs) != 0 {
		t.Fatalf("hub kept %d job maps and %d overview subscribers after cancel", len(hub.subs), len(hub.allSubs))
	}
}

func TestHubPublishNeverBlocksOnSlowSubscriber(t *testing.T) {
	hub := NewHub()
	// Never read from either channel: a slow SSE client.
	_, cancel := hub.Subscribe("job-1")
	defer cancel()
	_, cancelAll := hub.SubscribeAll()
	defer cancelAll()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			hub.Publish(domain.Event{ID: int64(i), JobID: "job-1", Type: domain.EventJobStarted})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that never reads")
	}
}

func TestHubConcurrentSubscribePublishCancel(t *testing.T) {
	hub := NewHub()
	const jobs = 8
	const perJob = 4

	var wg sync.WaitGroup
	for j := 0; j < jobs; j++ {
		jobID := string(rune('a' + j))
		for s := 0; s < perJob; s++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ch, cancel := hub.Subscribe(jobID)
				go func() {
					for range ch {
					}
				}()
				time.Sleep(time.Millisecond)
				cancel()
				cancel()
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				hub.Publish(domain.Event{ID: int64(i), JobID: jobID, Type: domain.EventItemCompleted})
			}
		}()
	}
	wg.Wait()

	hub.mu.RLock()
	remaining := len(hub.subs)
	hub.mu.RUnlock()
	if remaining != 0 {
		t.Fatalf("hub retained %d job subscription maps after cancel", remaining)
	}
}
