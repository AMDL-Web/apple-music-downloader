package artwork

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A cover whose first load failed, as a request the network dropped, is
// tried again while its view keeps showing it, and shows once it loads.
func TestACoverThatFailedOnceShowsWhenItLoads(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		first := hits == 1
		mu.Unlock()
		if first {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	posted := make(chan func(), 16)
	c := NewWith(func(fn func()) { posted <- fn }, http.DefaultTransport)
	clock := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return clock }
	settle := func() {
		t.Helper()
		select {
		case fn := <-posted:
			fn()
		case <-time.After(10 * time.Second):
			t.Fatal("the cover never loaded nor failed")
		}
	}
	tpl := srv.URL + "/cover/{w}x{h}bb.jpg"
	if c.Get(tpl, 100) != nil {
		t.Fatal("a cover shows before it loads")
	}
	settle()
	// The view asks every frame; for the first seconds the failure stands.
	for range 10 {
		if c.Get(tpl, 100) != nil {
			t.Fatal("the failed cover shows")
		}
		clock = clock.Add(100 * time.Millisecond)
	}
	clock = clock.Add(retryFirst)
	c.Get(tpl, 100)
	settle()
	if c.Get(tpl, 100) == nil {
		t.Errorf("after %v the cover that failed once still does not show", retryFirst)
	}
}
