// These run covers through real-size decodes and the CDN's latency on a
// synctest bubble's clock: the bounds are on that clock, which stands
// still while a cover decodes, however slowly the machine or the race
// detector runs.

package artwork

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// A cover the window scrolled away from while the CDN stalled on it, as a
// request that hangs for its whole timeout, gives its slot to the covers
// the window shows now: they load, and the stalled requests are dropped.
func TestStalledCoversScrolledAwayGiveTheirSlotsToTheOnesShown(t *testing.T) {
	pic := photo(t, 600)
	synctest.Test(t, func(t *testing.T) { stalledCoversGiveWay(t, pic) })
}

func stalledCoversGiveWay(t *testing.T, pic []byte) {
	var mu sync.Mutex
	dropped := 0
	cdn := memLink{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/stalled/") {
			<-r.Context().Done()
			mu.Lock()
			dropped++
			mu.Unlock()
			return
		}
		w.Write(pic)
	})}
	win := newWindow(t, cdn)
	// As many as the cache loads at once.
	const stalls = 6
	tpls := func(dir string) []string {
		var s []string
		for i := range stalls {
			s = append(s, fmt.Sprintf("%s/%s/%02d/{w}x{h}bb.jpg", cdnURL, dir, i))
		}
		return s
	}
	win.frame(tpls("stalled"), 300)
	shown := tpls("shown")
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if win.frame(shown, 300) == len(shown) {
			break
		}
	}
	if n := win.frame(shown, 300); n != len(shown) {
		t.Fatalf("after %v %d of %d covers shown loaded: the ones scrolled away still hold the slots", time.Since(start).Round(time.Millisecond), n, len(shown))
	}
	t.Logf("the shown covers loaded in %v", time.Since(start).Round(time.Millisecond))
	// The stalled requests end as their slots go.
	synctest.Wait()
	mu.Lock()
	defer mu.Unlock()
	if dropped != stalls {
		t.Errorf("%d of %d stalled requests were dropped", dropped, stalls)
	}
}

// A library scrolled from its top to its end shows the covers at the end
// before it loads any it passed on the way: they do not wait behind the
// covers the scroll left in the queue. The library is the maintainer's
// kind: album covers asked for at a size, every fourth one a playlist's
// presigned picture as uploaded, larger than any view; the CDN answers in
// 120 ms.
func TestCoversShownAfterAScrollLoadBeforeTheOnesItPassed(t *testing.T) {
	album, playlist := photo(t, 1000), photo(t, 1290)
	synctest.Test(t, func(t *testing.T) { shownCoversGoFirst(t, album, playlist) })
}

func shownCoversGoFirst(t *testing.T, album, playlist []byte) {
	var mu sync.Mutex
	var asked []string
	cdn := memLink{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		time.Sleep(120 * time.Millisecond)
		if strings.HasSuffix(r.URL.Path, "/image") {
			w.Write(playlist)
			return
		}
		w.Write(album)
	})}
	const covers, view, step = 132, 12, 3
	tpl := func(i int) string {
		if i%4 == 3 {
			return fmt.Sprintf("%s/sq-mq-us-%03d/image?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Expires=3600&X-Amz-Signature=%x", cdnURL, i, i*7919)
		}
		return fmt.Sprintf("%s/album-%03d/{w}x{h}bb.jpg", cdnURL, i)
	}
	rows := func(from int) []string {
		var s []string
		for i := from; i < from+view; i++ {
			s = append(s, tpl(i))
		}
		return s
	}
	win := newWindow(t, cdn)
	// 40 frames from the top to the end, 3 covers a frame.
	last := 0
	for top := 0; top+view < covers; top += step {
		win.frame(rows(top), 344)
		last = top
	}
	end := rows(covers - view)
	mu.Lock()
	before := len(asked)
	mu.Unlock()
	start := time.Now()
	for time.Since(start) < 5*time.Second {
		if win.frame(end, 344) == view {
			break
		}
	}
	took := time.Since(start)
	if n := win.frame(end, 344); n != view {
		t.Fatalf("%v after the scroll stopped, %d of the %d covers shown loaded", took.Round(time.Millisecond), n, view)
	}
	// Six slots, 120 ms a request: the twelve covers shown take two rounds
	// and change. Behind the covers the scroll left in the queue they took
	// 2.6 s.
	if took > 1500*time.Millisecond {
		t.Errorf("the %d covers shown after the scroll took %v to load", view, took.Round(time.Millisecond))
	}
	mu.Lock()
	defer mu.Unlock()
	// A request's first path segment names its cover. One the last frame of
	// the scroll showed may have set off then and reach the CDN after.
	cover := func(path string) string { return strings.Split(path, "/")[1] }
	shownAt := map[string]bool{}
	for _, tpl := range append(end, rows(last)...) {
		shownAt[cover(strings.TrimPrefix(tpl, cdnURL))] = true
	}
	var passed []string
	for _, p := range asked[before:] {
		if !shownAt[cover(p)] {
			passed = append(passed, p)
			continue
		}
		if len(passed) > 0 {
			t.Errorf("after the scroll stopped, %d covers it passed were asked for before %s, one shown: %q", len(passed), p, passed)
			break
		}
	}
	t.Logf("shown in %v; after the stop the CDN was asked %d times, %d for covers passed", took.Round(time.Millisecond), len(asked)-before, len(passed))
}
