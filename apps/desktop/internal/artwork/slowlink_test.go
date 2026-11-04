//go:build !race

package artwork

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// uploaded is a picture as a private playlist's or an uploaded album's
// cover is served: 4000 pixels a side, about 1.1 MB, the size of the one
// measured on the maintainer's link.
var uploaded = sync.OnceValue(func() []byte {
	const side = 4000
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	r := rand.New(rand.NewPCG(4000, 1))
	for y := range side {
		for x := range side {
			i := img.PixOffset(x, y)
			n := byte(r.IntN(9))
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = byte(x/16)+n, byte(y/16)+n, byte((x+y)/32)+n, 255
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		panic(err)
	}
	return buf.Bytes()
})

// link is a CDN behind a slow link that its connections share, as the
// blobstore was on the maintainer's network: a presigned link measured
// 20 to 35 KB/s on its own. It serves pic, from a Range's start when asked
// with one and a matching If-Range.
type link struct {
	pic     []byte
	rate    int // bytes a second, all connections together
	mu      sync.Mutex
	next    time.Time
	ranges  []string
	served  atomic.Int64
	stallAt int // the first request stops sending after this many bytes, when > 0
	first   atomic.Bool
	stalled chan time.Time
}

const etag = `"cover-1"`

func (l *link) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	from := 0
	l.mu.Lock()
	l.ranges = append(l.ranges, r.Header.Get("Range"))
	l.mu.Unlock()
	if rg := r.Header.Get("Range"); rg != "" && r.Header.Get("If-Range") == etag {
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
		if err != nil || n >= len(l.pic) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		from = n
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(l.pic)-from))
	if from > 0 {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", from, len(l.pic)-1, len(l.pic)))
		w.WriteHeader(http.StatusPartialContent)
	}
	stall := l.stallAt > 0 && !l.first.Swap(true)
	const chunk = 8 << 10
	sent := 0
	for i := from; i < len(l.pic); i += chunk {
		if stall && sent >= l.stallAt {
			l.stalled <- time.Now()
			<-r.Context().Done()
			l.stalled <- time.Now()
			return
		}
		end := min(i+chunk, len(l.pic))
		if l.rate > 0 {
			l.mu.Lock()
			at := time.Now()
			if l.next.After(at) {
				at = l.next
			}
			l.next = at.Add(time.Duration(end-i) * time.Second / time.Duration(l.rate))
			l.mu.Unlock()
			time.Sleep(time.Until(at))
		}
		if _, err := w.Write(l.pic[i:end]); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		sent += end - i
		l.served.Add(int64(end - i))
	}
}

// Six uploaded covers over a slow link, loading beside each other, each
// take over 30 s; they all load, rather than being given up at a deadline
// on the whole picture and started again from nothing.
func TestSixUploadedCoversOverASlowLinkAllLoad(t *testing.T) {
	pic := uploaded()
	l := &link{pic: pic, rate: 200 << 10}
	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	win := newWindow(t, http.DefaultTransport)
	var covers []string
	for i := range 6 {
		covers = append(covers, fmt.Sprintf("%s/sq-mq-us-0%02d-000002/ec/5e/d2/%d/image?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&X-Amz-Signature=%x", srv.URL, 33+i, i, i*104729))
	}
	start := time.Now()
	shown := 0
	for time.Since(start) < 70*time.Second && shown < len(covers) {
		shown = win.frame(covers, 512)
	}
	took := time.Since(start)
	t.Logf("%d of %d covers of %d KB shown after %v; %d KB served", shown, len(covers), len(pic)>>10, took.Round(100*time.Millisecond), l.served.Load()>>10)
	if shown != len(covers) {
		t.Fatalf("after %v %d of %d uploaded covers over the slow link showed", took.Round(time.Second), shown, len(covers))
	}
	if took < 30*time.Second {
		t.Fatalf("the covers loaded in %v: the link is not slow enough to measure anything", took.Round(time.Second))
	}
	if served := l.served.Load(); served > int64(len(covers)*len(pic))*11/10 {
		t.Errorf("%d KB served for %d covers of %d KB: covers were downloaded again", served>>10, len(covers), len(pic)>>10)
	}
}

// A download that stops receiving bytes, as a stalled connection, is given
// up after stallAfter rather than at a deadline on the whole picture, and
// the next try asks only for the rest.
func TestAStalledDownloadIsGivenUpAndResumedWhereItStopped(t *testing.T) {
	pic := uploaded()
	l := &link{pic: pic, stallAt: 400 << 10, stalled: make(chan time.Time, 2)}
	srv := httptest.NewServer(l)
	t.Cleanup(srv.Close)
	win := newWindow(t, http.DefaultTransport)
	cover := []string{srv.URL + "/sq-mq-us-037-000002/99/76/61/997661f0/image?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&X-Amz-Signature=aaaa"}
	start := time.Now()
	var stopped, givenUp time.Time
	skipped := false
	for time.Since(start) < 50*time.Second {
		if win.frame(cover, 512) == 1 {
			break
		}
		select {
		case at := <-l.stalled:
			if stopped.IsZero() {
				stopped = at
			} else {
				givenUp = at
			}
		default:
		}
		if e := win.c.entries[coverKey{keyOf(cover[0]), 0}]; !skipped && e != nil && e.failures > 0 {
			// The retry waits retryFirst, which the test need not.
			skipped = true
			win.clock = win.clock.Add(retryFirst)
		}
	}
	if win.frame(cover, 512) != 1 {
		t.Fatalf("after %v the cover that stalled once does not show", time.Since(start).Round(time.Second))
	}
	if givenUp.IsZero() {
		t.Fatal("the stalled download was never given up")
	}
	// The 15 s of stallAfter, and some slack.
	if wait := givenUp.Sub(stopped); wait > 18*time.Second {
		t.Errorf("the stalled download was given up %v after its last byte", wait.Round(100*time.Millisecond))
	}
	l.mu.Lock()
	ranges := l.ranges
	l.mu.Unlock()
	if want := fmt.Sprintf("bytes=%d-", 400<<10); len(ranges) != 2 || ranges[1] != want {
		t.Errorf("the requests asked for ranges %q, want the whole picture then %q", ranges, want)
	}
	t.Logf("given up %v after the last byte; %d KB served for a %d KB picture", givenUp.Sub(stopped).Round(100*time.Millisecond), l.served.Load()>>10, len(pic)>>10)
}
