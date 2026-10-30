package mode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"amdl/internal/client"
	"amdl/internal/config"
	"amdl/internal/local"
	"amdl/internal/wrapper"
)

// Check is one line of a connection test.
type Check struct {
	OK    bool
	Text  string
	Note  string
	Taken time.Duration
}

// testTimeout bounds a whole connection test.
const testTimeout = 15 * time.Second

// TestServer checks an amdl address: that it answers, its version, and
// whether its wrapper has an account signed in. It stops at the first check
// that fails.
func TestServer(ctx context.Context, address string) []Check {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	c, err := client.New(address)
	if err != nil {
		return []Check{{Text: err.Error()}}
	}
	var checks []Check
	start := time.Now()
	h, err := c.Health(ctx)
	if err != nil {
		return append(checks, Check{Text: "No answer from " + c.Base(), Note: shortErr(err)})
	}
	checks = append(checks, Check{OK: h.Status == "ok", Text: "Reached the server", Note: h.Status, Taken: time.Since(start)})
	start = time.Now()
	sys, err := c.System(ctx)
	if err != nil {
		return append(checks, Check{Text: "Could not read the server's version", Note: shortErr(err)})
	}
	note := "amdl " + sys.Version
	if sys.Simulate {
		note += " · simulate mode"
	}
	checks = append(checks, Check{OK: true, Text: "It is an amdl server", Note: note, Taken: time.Since(start)})
	start = time.Now()
	ws, err := c.WrapperStatus(ctx)
	if err != nil {
		return append(checks, Check{Text: "The server cannot reach its wrapper", Note: shortErr(err)})
	}
	return append(checks, accountCheck(ws.Ready, ws.LoggedIn, ws.Regions, ws.LoginSupported, time.Since(start)))
}

// TestWrapper checks a wrapper address the standalone backend would use:
// that it answers and has an account signed in.
func TestWrapper(ctx context.Context, address string) []Check {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	cfg := config.Default().Wrapper
	cfg.Address = local.WrapperURL(address)
	cfg.TimeoutSeconds = 10
	w, err := wrapper.NewClient(cfg)
	if err != nil {
		return []Check{{Text: err.Error()}}
	}
	defer w.Close()
	start := time.Now()
	st, err := w.Status(ctx)
	taken := time.Since(start)
	if err != nil {
		return []Check{{Text: "No answer from the wrapper", Note: shortErr(err)}}
	}
	return []Check{
		{OK: true, Text: "Reached the wrapper", Taken: taken},
		accountCheck(st.Ready, st.LoggedIn, st.Regions, st.LoginSupported, 0),
	}
}

func accountCheck(ready, signedIn bool, regions []string, manager bool, taken time.Duration) Check {
	kind := "wrapper-lite"
	if manager {
		kind = "wrapper-manager"
	}
	switch {
	case !ready:
		return Check{Text: "The wrapper is still starting", Note: kind, Taken: taken}
	case !signedIn:
		return Check{Text: "No account is signed in on the wrapper", Note: kind, Taken: taken}
	}
	return Check{OK: true, Text: "An account is signed in · storefronts " + strings.Join(regions, ", "), Note: kind, Taken: taken}
}

func shortErr(err error) string {
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i >= 0 && len(s) > 80 {
		s = s[i+2:]
	}
	if len(s) > 80 {
		s = s[:77] + "…"
	}
	return s
}

// String writes a check for logs.
func (c Check) String() string {
	mark := "✗"
	if c.OK {
		mark = "✓"
	}
	return fmt.Sprintf("%s %s (%s, %s)", mark, c.Text, c.Note, c.Taken.Round(time.Millisecond))
}
