package limits

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRequestGateHoldsConcurrencyUntilBodyClose(t *testing.T) {
	t.Parallel()
	gate := NewRequestGate(1, 1000, 1000)
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/one", nil)
	first, err := gate.Do(context.Background(), client, req, false)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/two", nil)
	if _, err := gate.Do(ctx, client, req, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Do error = %v, want deadline exceeded while first body is open", err)
	}
	if err := first.Body.Close(); err != nil {
		t.Fatal(err)
	}

	req, _ = http.NewRequest(http.MethodGet, "https://example.test/three", nil)
	third, err := gate.Do(context.Background(), client, req, false)
	if err != nil {
		t.Fatalf("Do after body close: %v", err)
	}
	third.Body.Close()
}

func TestRequestGateRateLimitIsContextAware(t *testing.T) {
	t.Parallel()
	gate := NewRequestGate(2, 1, 1)
	release, err := gate.Acquire(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	release()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := gate.Acquire(ctx, true); !errors.Is(err, context.DeadlineExceeded) {
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("rate-limited Acquire error = %v, want context cancellation", err)
		}
	}

	// Requests marked non-rate-limited share concurrency but bypass the token
	// bucket, so artwork and manifests are not serialized by catalog RPS.
	release, err = gate.Acquire(context.Background(), false)
	if err != nil {
		t.Fatalf("non-rate-limited Acquire: %v", err)
	}
	release()
}

func TestRequestGateSharedCooldownAndCancellation(t *testing.T) {
	t.Parallel()
	gate := NewRequestGate(2, 1000, 1000)
	gate.Penalize(50 * time.Millisecond)
	started := time.Now()
	release, err := gate.Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	release()
	if elapsed := time.Since(started); elapsed < 40*time.Millisecond {
		t.Fatalf("Acquire waited %v, want shared cooldown", elapsed)
	}

	gate.Penalize(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := gate.Acquire(ctx, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cooldown Acquire error = %v, want deadline exceeded", err)
	}
}

func TestRequestGateDoesNotPreconsumeRateTokensBehindConcurrency(t *testing.T) {
	t.Parallel()
	gate := NewRequestGate(1, 10, 1)
	first, err := gate.Acquire(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan time.Time, 2)
	for range 2 {
		go func() {
			release, err := gate.Acquire(context.Background(), true)
			if err != nil {
				return
			}
			acquired <- time.Now()
			release()
		}()
	}
	// Leave both callers queued long enough that the old ordering would have
	// admitted both rate tokens before either could enter the HTTP pool.
	time.Sleep(150 * time.Millisecond)
	first()
	t1 := <-acquired
	t2 := <-acquired
	if spacing := t2.Sub(t1); spacing < 70*time.Millisecond {
		t.Fatalf("queued starts were only %v apart, want token-bucket pacing", spacing)
	}
}

func TestRequestGateDoWith429RetryClosesAndPenalizes(t *testing.T) {
	t.Parallel()
	gate := NewRequestGate(1, 1000, 1000)
	var calls int
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		status := http.StatusOK
		if calls == 1 {
			status = http.StatusTooManyRequests
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("body"))}, nil
	})}
	req, _ := http.NewRequest(http.MethodGet, "https://example.test/manifest", nil)
	started := time.Now()
	resp, err := gate.DoWith429Retry(context.Background(), client, req, false, func(http.Header) time.Duration { return 30 * time.Millisecond })
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if calls != 2 || resp.StatusCode != http.StatusOK {
		t.Fatalf("calls/status = %d/%d, want 2/200", calls, resp.StatusCode)
	}
	if elapsed := time.Since(started); elapsed < 25*time.Millisecond {
		t.Fatalf("retry ignored shared cooldown: %v", elapsed)
	}
}

func cooldownRemaining(g *RequestGate) time.Duration {
	g.cooldownMu.Lock()
	defer g.cooldownMu.Unlock()
	return time.Until(g.cooldownUntil)
}

// A huge Retry-After must not freeze every Apple request for its full length:
// the shared cooldown is capped at 60 s and the clamp is logged.
func TestRequestGatePenalizeCapsCooldownAndLogs(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	gate := NewRequestGate(1, 1, 1)
	gate.Penalize(10 * time.Minute)
	if remaining := cooldownRemaining(gate); remaining > 60*time.Second || remaining < 59*time.Second {
		t.Fatalf("cooldown after a 10m penalty = %v, want about 60s", remaining)
	}
	if !strings.Contains(logs.String(), "clamping") || !strings.Contains(logs.String(), "requested=10m0s") {
		t.Fatalf("clamp not logged with the requested delay: %q", logs.String())
	}

	logs.Reset()
	short := NewRequestGate(1, 1, 1)
	short.Penalize(2 * time.Second)
	if remaining := cooldownRemaining(short); remaining > 2*time.Second || remaining < time.Second {
		t.Fatalf("cooldown after a 2s penalty = %v, want about 2s", remaining)
	}
	if logs.Len() != 0 {
		t.Fatalf("an in-range penalty must not log: %q", logs.String())
	}
}

func TestDefaultRetryDelay(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		retryAfter string
		want       time.Duration
	}{
		{name: "seconds", retryAfter: "5", want: 5 * time.Second},
		{name: "zero seconds", retryAfter: "0", want: 0},
		{name: "date in the past", retryAfter: "Wed, 21 Oct 2015 07:28:00 GMT", want: 0},
	} {
		header := http.Header{}
		header.Set("Retry-After", tt.retryAfter)
		if got := DefaultRetryDelay(header); got != tt.want {
			t.Errorf("%s: DefaultRetryDelay(%q) = %v, want %v", tt.name, tt.retryAfter, got, tt.want)
		}
	}

	// Without a usable hint the delay is 1s plus jitter in [0, 250ms); the
	// draws must actually vary, or concurrent retries would fire together.
	seen := map[time.Duration]bool{}
	for _, retryAfter := range append(slices.Repeat([]string{""}, 99), "soon") {
		header := http.Header{}
		if retryAfter != "" {
			header.Set("Retry-After", retryAfter)
		}
		got := DefaultRetryDelay(header)
		if got < time.Second || got >= time.Second+250*time.Millisecond {
			t.Fatalf("fallback delay = %v, want within [1s, 1.25s)", got)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("100 fallback delays were all %v; jitter is not random", seen)
	}
}
