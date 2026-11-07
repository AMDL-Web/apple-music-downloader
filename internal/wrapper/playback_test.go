package wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
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
	manifests := newCounts()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/m3u8" {
			manifests.add("in flight", 1)
			<-gate
			manifests.add("in flight", -1)
			writeReply(w, 0, "SUCCESS", map[string]any{"m3u8": "#EXTM3U"})
			return
		}
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 3})
	}))
	waits := watchPermitWaits(c)

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
	manifests.await(t, "in flight", manifestRequestLimit)
	// The rest of the batch queues for the manifest permit.
	waits.await(t, c.manifestLimit, 8-manifestRequestLimit)

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
	if p := manifests.max("in flight"); p > manifestRequestLimit {
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
	held := holdPermits(t, c.playbackLimit, playbackRequestLimit)
	// Longer than every attempt's timeout and retry delay together (the delays
	// are zero here), which used to bound the permit wait as well, and shorter
	// than the permit wait's own bound.
	wait := c.timeout * time.Duration(len(c.retryDelays)+4)
	if wait >= playbackWaitTimeouts*c.timeout {
		t.Fatalf("wait %v is not inside the permit wait bound", wait)
	}
	time.AfterFunc(wait, held)

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

// A job's /key or /m3u8 that interactive requests keep overtaking for a
// permit still ends within its bound, with an error that says why, and the
// job's context needs no deadline of its own for that.
func TestPlaybackPermitWaitEndsWhileOvertaken(t *testing.T) {
	tests := []struct {
		name    string
		limit   func(*Client) *limits.Semaphore
		request func(context.Context, *Client) error
	}{
		{
			name:  "key template behind the playback permits",
			limit: func(c *Client) *limits.Semaphore { return c.playbackLimit },
			request: func(ctx context.Context, c *Client) error {
				session, _ := c.NewDecryptSession(ctx, "1")
				_, err := session.DecryptFragment("skd://k", [][]byte{make([]byte, 16)})
				return err
			},
		},
		{
			name:  "device manifest behind the manifest permit",
			limit: func(c *Client) *limits.Semaphore { return c.manifestLimit },
			request: func(ctx context.Context, c *Client) error {
				_, err := c.M3U8(ctx, "1")
				return err
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				writeReply(w, 0, "SUCCESS", map[string]any{"xor": 1, "m3u8": "#EXTM3U"})
			}))
			c.timeout = 20 * time.Millisecond
			bound := playbackWaitTimeouts * c.timeout
			stop := overtake(t, tt.limit(c))
			defer stop()

			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- tt.request(limits.WithPriority(context.Background(), 1), c) }()
			select {
			case err := <-done:
				if !errors.Is(err, errPlaybackWait) {
					t.Fatalf("overtaken request = %v, want errPlaybackWait", err)
				}
				if elapsed := time.Since(start); elapsed < bound {
					t.Fatalf("gave up after %v, before its %v bound", elapsed, bound)
				}
				t.Logf("error the job shows: %v", err)
			case <-time.After(bound + 5*time.Second):
				t.Fatalf("still waiting for a permit %v after its %v bound", 5*time.Second, bound)
			}
			stop()
			if n := calls.Load(); n != 0 {
				t.Fatalf("wrapper saw %d requests, want none from a request that never got a permit", n)
			}
		})
	}
}

// A template fetch whose every caller gave up stops: it neither keeps waiting
// for a permit nor sends its /key once one frees up, and the next caller starts
// a fetch of its own.
func TestAbandonedTemplateFetchStops(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		writeReply(w, 0, "SUCCESS", map[string]any{"xor": 5})
	}))
	waits := watchPermitWaits(c)
	release := holdPermits(t, c.playbackLimit, playbackRequestLimit)

	ctx, cancel := context.WithCancel(limits.WithPriority(context.Background(), 1))
	done := make(chan error, 1)
	go func() {
		session, _ := c.NewDecryptSession(ctx, "1")
		_, err := session.DecryptFragment("skd://k", [][]byte{make([]byte, 16)})
		done <- err
	}()
	waits.await(t, c.playbackLimit, 1) // the fetch queued for a permit
	c.templates.mu.Lock()
	fetch := c.templates.entries["skd://k"]
	c.templates.mu.Unlock()
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled caller = %v, want context.Canceled", err)
	}
	// The fetch ends while every permit is still taken: it gave up its wait
	// rather than sending a /key later.
	select {
	case <-fetch.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the fetch kept running after every caller left")
	}
	release()
	if n := calls.Load(); n != 0 {
		t.Fatalf("/key calls after every caller left = %d, want 0", n)
	}

	plain := bytes.Repeat([]byte{3}, 16)
	session, _ := c.NewDecryptSession(context.Background(), "2")
	got, err := session.DecryptFragment("skd://k", [][]byte{encrypt(plain, 5)})
	if err != nil {
		t.Fatalf("next caller: %v", err)
	}
	if !bytes.Equal(got[0], plain) {
		t.Fatalf("decrypted = %x, want %x", got[0], plain)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("/key calls = %d, want the next caller's own fetch", n)
	}
}

// A key fetched ahead of need reaches the wrapper only after the /m3u8
// requests queued for the same job: a track cannot start its download before
// its stream is chosen, while a prefetched key is only needed once the download
// of its own track ends. Sharing the playback permits by track order instead
// made the wrapper alternate between the two, so with every track of a
// Hi-Res album prefetching, the last stream was chosen about twice as late.
func TestPrefetchedKeysWaitForQueuedManifests(t *testing.T) {
	wl := &fakeWrapper{serial: true, holdFirst: make(chan struct{})}
	c := newTestClient(t, wl)
	waits := watchPermitWaits(c)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() {
		if _, err := c.M3U8(trackCtx(0), "100"); err != nil {
			t.Error(err)
		}
	})
	wl.events().await(t, "arrived", 1)
	wg.Go(func() {
		if err := c.PrefetchKeys(trackCtx(0), "100", []string{"skd://k0"}); err != nil {
			t.Error(err)
		}
	})
	// The prefetch queues before the next track's manifest.
	waits.await(t, c.manifestLimit, 1)
	for i := 1; i <= 2; i++ {
		wg.Go(func() {
			if _, err := c.M3U8(trackCtx(i), fmt.Sprint(100+i)); err != nil {
				t.Error(err)
			}
		})
		waits.await(t, c.manifestLimit, 1+i)
	}
	close(wl.holdFirst)
	wg.Wait()
	want := []string{"/m3u8 100", "/m3u8 101", "/m3u8 102", "/key 100"}
	if got := wl.served(); !slices.Equal(got, want) {
		t.Fatalf("wrapper served %v, want %v", got, want)
	}
}

// A prefetched key still waiting behind manifests goes out at its track's own
// rank once a caller needs it now, as a key asked for only at decrypt time
// would: tracks whose downloads have finished must not wait for every stream
// still to be chosen in a long playlist.
func TestNeededKeyStopsWaitingForManifests(t *testing.T) {
	wl := &fakeWrapper{serial: true, holdFirst: make(chan struct{})}
	c := newTestClient(t, wl)
	waits := watchPermitWaits(c)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() {
		if _, err := c.M3U8(trackCtx(0), "100"); err != nil {
			t.Error(err)
		}
	})
	wl.events().await(t, "arrived", 1)
	wg.Go(func() {
		if err := c.PrefetchKeys(trackCtx(0), "100", []string{"skd://k0"}); err != nil {
			t.Error(err)
		}
	})
	waits.await(t, c.manifestLimit, 1)
	for i := 1; i <= 2; i++ {
		wg.Go(func() {
			if _, err := c.M3U8(trackCtx(i), fmt.Sprint(100+i)); err != nil {
				t.Error(err)
			}
		})
	}
	waits.await(t, c.manifestLimit, 3)
	wg.Go(func() {
		if err := c.PrepareKeys(trackCtx(0), "100", []string{"skd://k0"}); err != nil {
			t.Error(err)
		}
	})
	// Once needed, the key leaves the manifest lane, takes the free playback
	// permit and reaches the wrapper, which is still busy with the first
	// manifest.
	wl.events().await(t, "arrived", 2)
	close(wl.holdFirst)
	wg.Wait()
	want := []string{"/m3u8 100", "/key 100", "/m3u8 101", "/m3u8 102"}
	if got := wl.served(); !slices.Equal(got, want) {
		t.Fatalf("wrapper served %v, want %v", got, want)
	}
}

// holdPermits takes n of sem's permits and returns a function that releases
// them all.
func holdPermits(t *testing.T, sem *limits.Semaphore, n int) func() {
	t.Helper()
	var held []func()
	for range n {
		release, err := sem.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, release)
	}
	return func() {
		for _, release := range held {
			release()
		}
	}
}

// overtake keeps every permit of sem busy with interactive-rank requests, which
// outrank every job, and keeps more of them queued: a holder releases its
// permit only once another of them waits in sem's queue, so each release goes
// to one of them ahead of any job waiting. It returns once they are cycling,
// and the returned function stops them.
func overtake(t *testing.T, sem *limits.Semaphore) func() {
	t.Helper()
	stop := make(chan struct{})
	queued := make(chan struct{}, 64)
	cycling := make(chan struct{})
	var wg sync.WaitGroup
	var grants atomic.Int32
	for range 6 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				ctx := &queuedContext{Context: context.Background(), queued: func() { queued <- struct{}{} }}
				release, err := sem.Acquire(ctx)
				if err != nil {
					return
				}
				if grants.Add(1) == 12 {
					close(cycling)
				}
				select {
				case <-queued:
				case <-stop:
				}
				release()
			}
		})
	}
	var once sync.Once
	stopAll := func() { once.Do(func() { close(stop); wg.Wait() }) }
	// Every looper has been granted a permit twice, on average, so the
	// others queue behind each holder from now on.
	select {
	case <-cycling:
	case <-time.After(5 * time.Second):
		stopAll()
		t.Fatal("the interactive requests never started cycling")
	}
	return stopAll
}
