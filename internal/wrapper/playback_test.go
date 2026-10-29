package wrapper

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/limits"
)

// A batch quality query sends one /m3u8 per Hi-Res track at interactive rank,
// ahead of every job. While the wrapper is slow to answer them, a download's
// /key must still get a playback permit instead of queueing behind the batch.
func TestKeyIsNotStarvedByQueuedManifests(t *testing.T) {
	gate := make(chan struct{})
	var manifestsInFlight, manifestPeak atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/m3u8" {
			n := manifestsInFlight.Add(1)
			for {
				p := manifestPeak.Load()
				if n <= p || manifestPeak.CompareAndSwap(p, n) {
					break
				}
			}
			<-gate
			manifestsInFlight.Add(-1)
			writeReply(w, 0, "SUCCESS", map[string]any{"m3u8": "#EXTM3U"})
			return
		}
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 3})
	}))

	var probes sync.WaitGroup
	for i := range 8 {
		probes.Go(func() {
			if _, err := c.M3U8(context.Background(), fmt.Sprint(i)); err != nil {
				t.Error(err)
			}
		})
	}
	defer probes.Wait()
	defer close(gate)
	waitFor(t, func() bool { return manifestsInFlight.Load() >= 1 })
	time.Sleep(50 * time.Millisecond) // let the rest of the batch queue

	jobCtx, cancel := context.WithTimeout(limits.WithPriority(context.Background(), 1), 2*time.Second)
	defer cancel()
	session, _ := c.NewDecryptSession(jobCtx, "1")
	plain := bytes.Repeat([]byte{7}, 16)
	got, err := session.DecryptFragment("skd://k", [][]byte{encrypt(plain, 3)})
	if err != nil {
		t.Fatalf("DecryptFragment while quality probes wait: %v", err)
	}
	if !bytes.Equal(got[0], plain) {
		t.Fatalf("decrypted = %x, want %x", got[0], plain)
	}
	if p := manifestPeak.Load(); p > manifestRequestLimit {
		t.Fatalf("peak /m3u8 requests in flight = %d, want at most %d", p, manifestRequestLimit)
	}
}

// The time a /key waits for a playback permit is not taken from the attempts'
// request timeouts: a fetch that waits longer than all its attempts could take
// still gets its full retry budget once it holds a permit.
func TestKeyPermitWaitIsNotChargedToItsRetryBudget(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 1})
	}))
	c.timeout = 100 * time.Millisecond
	var held []func()
	for range playbackRequestLimit {
		release, err := c.playbackLimit.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, release)
	}
	// Longer than every attempt's timeout and retry delay together (the delays
	// are zero here), which used to bound the permit wait as well.
	wait := 3 * c.timeout * time.Duration(len(c.retryDelays)+2)
	time.AfterFunc(wait, func() {
		for _, release := range held {
			release()
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), wait+5*time.Second)
	defer cancel()
	session, _ := c.NewDecryptSession(ctx, "1")
	if _, err := session.DecryptFragment("skd://k", [][]byte{make([]byte, 16)}); err != nil {
		t.Fatalf("DecryptFragment after a long permit wait: %v", err)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("/key calls = %d, want 1", n)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 5s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
