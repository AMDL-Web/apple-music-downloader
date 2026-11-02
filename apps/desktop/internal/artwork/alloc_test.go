//go:build !race

// The race detector allocates on its own account.

package artwork

import (
	"errors"
	"net/http"
	"testing"
)

// refuse fails every request at once: the cover is asked for, not fetched.
type refuse struct{}

func (refuse) RoundTrip(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }

// The views ask for every cover they show on every frame: one already
// asked for is found without allocating.
func TestACoverAskedForAgainIsFoundWithoutAllocating(t *testing.T) {
	// post drops what the fetch hands back, so the cover stays on its way.
	c := NewWith(func(func()) {}, refuse{})
	const template = "https://is1-ssl.mzstatic.com/image/thumb/x/{w}x{h}bb.{f}"
	c.Get(template, 320)
	if n := testing.AllocsPerRun(100, func() { c.Get(template, 320) }); n != 0 {
		t.Errorf("Get allocates %.0f times for a cover it has", n)
	}
}
