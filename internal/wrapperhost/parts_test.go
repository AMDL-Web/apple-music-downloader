package wrapperhost

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// slowConn limits what one connection writes, as the blob host does.
type slowConn struct {
	net.Conn
	rate int
}

func (c slowConn) Write(b []byte) (int, error) {
	total := 0
	for len(b) > 0 {
		k := min(len(b), 4096)
		n, err := c.Conn.Write(b[:k])
		total += n
		if err != nil {
			return total, err
		}
		b = b[k:]
		time.Sleep(time.Duration(k) * time.Second / time.Duration(c.rate))
	}
	return total, nil
}

type slowListener struct {
	net.Listener
	rate int
}

func (l slowListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil || l.rate <= 0 {
		return c, err
	}
	return slowConn{c, l.rate}, nil
}

// countingWriter counts what a request sends while its epoch is the
// current one, so that a canceled request still sending does not count into
// the next run.
type countingWriter struct {
	http.ResponseWriter
	a     *artifactServer
	epoch int64
}

func (c countingWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	if c.a.epoch.Load() == c.epoch {
		c.a.served.Add(int64(n))
	}
	return n, err
}

// artifactServer stands in for nightly.link and the blob host behind it.
type artifactServer struct {
	body    []byte
	noRange bool
	// inject may answer a blob request instead of the file: "500" fails it,
	// "cut" sends half of the range and drops the connection.
	inject func(start int64) string

	served   atomic.Int64
	epoch    atomic.Int64
	resolves atomic.Int64
	requests atomic.Int64
	token    atomic.Int64
	srv      *httptest.Server
}

func newArtifactServer(t *testing.T, body []byte, rate int) *artifactServer {
	t.Helper()
	a := &artifactServer{body: body}
	mux := http.NewServeMux()
	mux.HandleFunc("/WorldObservationLog/wrapper/actions/artifacts/7.zip", func(w http.ResponseWriter, r *http.Request) {
		a.resolves.Add(1)
		http.Redirect(w, r, fmt.Sprintf("/blob/%d?sig=secret", a.token.Load()), http.StatusFound)
	})
	mux.HandleFunc("/blob/{tok}", func(w http.ResponseWriter, r *http.Request) {
		a.requests.Add(1)
		if r.PathValue("tok") != fmt.Sprint(a.token.Load()) {
			http.Error(w, "<Error>AuthenticationFailed</Error>", http.StatusForbidden)
			return
		}
		var start, end int64
		if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err == nil && a.inject != nil {
			switch a.inject(start) {
			case "500":
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			case "cut":
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(a.body)))
				w.Header().Set("Content-Length", fmt.Sprint(end-start+1))
				w.WriteHeader(http.StatusPartialContent)
				half := (end - start + 1) / 2
				cw := countingWriter{w, a, a.epoch.Load()}
				cw.Write(a.body[start : start+half])
				w.(http.Flusher).Flush()
				panic(http.ErrAbortHandler)
			}
		}
		if a.noRange {
			r.Header.Del("Range")
		}
		http.ServeContent(countingWriter{w, a, a.epoch.Load()}, r, "", time.Time{}, bytes.NewReader(a.body))
	})
	a.srv = httptest.NewUnstartedServer(mux)
	a.srv.Listener = slowListener{a.srv.Listener, rate}
	a.srv.Start()
	t.Cleanup(a.srv.Close)
	return a
}

// resetServed starts counting the bytes served from zero.
func (a *artifactServer) resetServed() {
	a.epoch.Add(1)
	a.served.Store(0)
}

func (a *artifactServer) link() NightlyLink {
	return NightlyLink{Client: &http.Client{}, Base: a.srv.URL}
}

func (a *artifactServer) pin() Pin { return pinFor(a.body, "c") }

func randomBody(n int) []byte {
	r := rand.New(rand.NewPCG(1, 2))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// shrink makes chunks and backoffs small enough for a test's tiny files.
func shrink(t *testing.T) {
	t.Helper()
	c, a, b, m := chunkSize, chunkAttempts, chunkBackoff, maxBackoff
	chunkSize, chunkAttempts, chunkBackoff, maxBackoff = 64<<10, 4, time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { chunkSize, chunkAttempts, chunkBackoff, maxBackoff = c, a, b, m })
}

// streamOnly hides a fetcher's ranges, which leaves today's single stream.
type streamOnly struct{ Fetcher }

func sameFile(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s holds %d bytes (err %v), want the artifact's %d", path, len(got), err, len(want))
	}
}

func TestRangedDownloadBeatsOneStream(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(2<<20), 4<<20)
	dir := t.TempDir()

	start := time.Now()
	if err := Download(context.Background(), streamOnly{a.link()}, a.pin(), filepath.Join(dir, "one.zip"), nil); err != nil {
		t.Fatal(err)
	}
	one := time.Since(start)

	var inside atomic.Int32
	var last int64
	start = time.Now()
	err := Download(context.Background(), a.link(), a.pin(), filepath.Join(dir, "many.zip"), func(p Progress) {
		if inside.Add(1) != 1 {
			t.Error("report called from two goroutines at once")
		}
		defer inside.Add(-1)
		if p.Done < last {
			t.Errorf("Done went back from %d to %d", last, p.Done)
		}
		last = p.Done
	})
	if err != nil {
		t.Fatal(err)
	}
	many := time.Since(start)
	size := float64(len(a.body))
	t.Logf("one stream %v (%.0f KB/s), eight connections %v (%.0f KB/s)", one, size/1024/one.Seconds(), many, size/1024/many.Seconds())
	if many*3 > one {
		t.Errorf("eight connections took %v against %v for one stream; want at least 3 times faster", many, one)
	}
	sameFile(t, filepath.Join(dir, "many.zip"), a.body)
	if _, err := os.Stat(filepath.Join(dir, "many.zip.parts")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the parts outlived a finished download: %v", err)
	}
}

func TestRangedDownloadResumesAfterCancel(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(2<<20), 2<<20)
	path := filepath.Join(t.TempDir(), "a.zip")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var atCancel int64
	err := Download(ctx, a.link(), a.pin(), path, func(p Progress) {
		if atCancel == 0 && p.Done >= int64(len(a.body))/2 {
			atCancel = p.Done
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled download: %v", err)
	}
	if entries, _ := os.ReadDir(path + ".parts"); len(entries) == 0 {
		t.Fatal("a canceled download keeps what it has")
	}
	a.resetServed()
	var first Progress
	err = Download(context.Background(), a.link(), a.pin(), path, func(p Progress) {
		if first == (Progress{}) {
			first = p
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Done < atCancel {
		t.Errorf("the resumed download starts at %d bytes, the canceled one had %d", first.Done, atCancel)
	}
	// A request the canceled run had sent may still reach the server and
	// count a piece into this run; a resume that started over would fetch
	// far more than the slack.
	const slack = 64 << 10
	if got, max := a.served.Load(), int64(len(a.body))-atCancel+slack; got > max {
		t.Errorf("the resume fetched %d bytes, at most the %d that were missing", got, max)
	}
	sameFile(t, path, a.body)
}

func TestRangedDownloadAdoptsAStreamsPartialFile(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(1<<20), 0)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.zip")
	prefix := 4 * chunkSize
	if err := os.WriteFile(path, a.body[:prefix], 0o600); err != nil {
		t.Fatal(err)
	}
	// A part a canceled ranged download was filling: the first one after the prefix.
	os.MkdirAll(path+".parts", 0o700)
	os.WriteFile(filepath.Join(path+".parts", fmt.Sprintf("%d-%d.tmp", prefix, prefix+chunkSize)), a.body[prefix:prefix+20000], 0o600)
	if err := Download(context.Background(), a.link(), a.pin(), path, nil); err != nil {
		t.Fatal(err)
	}
	if got, max := a.served.Load(), int64(len(a.body))-prefix-20000; got > max {
		t.Errorf("fetched %d bytes, at most the %d missing", got, max)
	}
	sameFile(t, path, a.body)
}

func TestRangedDownloadRetriesAChunk(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(1<<20), 0)
	var mu sync.Mutex
	seen := map[int64]int{}
	a.inject = func(start int64) string {
		mu.Lock()
		defer mu.Unlock()
		seen[start]++
		switch {
		case start == 0 && seen[start] <= 2:
			return "500"
		case start == 3*chunkSize && seen[start] == 1:
			return "cut"
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := Download(context.Background(), a.link(), a.pin(), path, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen[0] != 3 {
		t.Errorf("the first chunk was asked for %d times, want 3 (two failures, then success)", seen[0])
	}
	if seen[3*chunkSize] != 1 {
		t.Errorf("the cut chunk restarted from its start: %v", seen)
	}
	sameFile(t, path, a.body)
}

func TestRangedDownloadGivesUpOnAChunk(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(1<<20), 0)
	a.inject = func(start int64) string {
		if start == 0 {
			return "500"
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	err := Download(context.Background(), a.link(), a.pin(), path, nil)
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("a chunk that always fails: %v", err)
	}
	if entries, _ := os.ReadDir(path + ".parts"); len(entries) == 0 {
		t.Error("the chunks that did arrive are kept")
	}
}

func TestRangedDownloadResolvesAgainWhenTheURLExpires(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(2<<20), 8<<20)
	var n atomic.Int64
	a.inject = func(int64) string {
		if n.Add(1) == 6 {
			a.token.Add(1) // the signed URL handed out so far stops working
		}
		return ""
	}
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := Download(context.Background(), a.link(), a.pin(), path, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.resolves.Load(); got != 2 {
		t.Errorf("nightly.link was asked %d times, want 2: the first, and one for all workers after the expiry", got)
	}
	sameFile(t, path, a.body)
}

func TestRangedDownloadFallsBackWithoutRange(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(1<<20), 0)
	a.noRange = true
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := Download(context.Background(), a.link(), a.pin(), path, nil); err != nil {
		t.Fatal(err)
	}
	sameFile(t, path, a.body)
	if _, err := os.Stat(path + ".parts"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the parts outlived a finished download: %v", err)
	}
}

func TestRangedDownloadStillChecksTheSHA256(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(1<<20), 0)
	pin := a.pin()
	sum := sha256.Sum256(append([]byte("other"), a.body...))
	pin.SHA256 = hex.EncodeToString(sum[:])
	path := filepath.Join(t.TempDir(), "a.zip")
	if err := Download(context.Background(), a.link(), pin, path, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("a download that does not match its pin: %v", err)
	}
	if st, err := os.Stat(path); err != nil || st.Size() != 0 {
		t.Errorf("a bad download is discarded: %v %v", st, err)
	}
	if _, err := os.Stat(path + ".parts"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("bad parts are not worth resuming: %v", err)
	}
}

func TestRangedDownloadLeavesNoGoroutinesWhenCanceled(t *testing.T) {
	shrink(t)
	a := newArtifactServer(t, randomBody(2<<20), 256<<10)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	err := Download(ctx, a.link(), a.pin(), filepath.Join(t.TempDir(), "a.zip"), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a download past its deadline: %v", err)
	}
	a.srv.Close()
	for i := 0; i < 100 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		buf := make([]byte, 1<<16)
		t.Errorf("%d goroutines before, %d after the cancel:\n%s", before, after, buf[:runtime.Stack(buf, true)])
	}
}

func TestMissingChunks(t *testing.T) {
	shrink(t)
	d := parts{size: 5 * chunkSize / 2}
	got := d.missing([]span{{0, chunkSize}, {chunkSize / 2, chunkSize + 10}, {2 * chunkSize, 2*chunkSize + 5}})
	want := []span{{chunkSize + 10, 2 * chunkSize}, {2*chunkSize + 5, 5 * chunkSize / 2}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("missing %v, want %v", got, want)
	}
}
