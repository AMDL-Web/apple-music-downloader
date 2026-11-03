//go:build guestboot

package wrapperhost

// TestGuestBoot boots a real manager guest in QEMU the way the desktop app and
// amdl serve --local do (Supervisor, Args, the fw_cfg manager arguments), and
// waits until the guest's manager answers /status. It needs network access and
// QEMU, so it only builds with -tags guestboot; make guestboot runs it.
//
// It fails on the faults the fake-QEMU tests cannot see: manager arguments
// that never reach the guest's init, a guest with no network, a pinned payload
// that does not download or does not match its sha256, a manager or kernel
// panic, and a guest that restarts in a loop.
//
// Environment:
//
//	GUESTBOOT_PIN_FILE   a pin as JSON (the wrapper-pins file); default Builtin
//	GUESTBOOT_GUEST_DIR  boot the guest files in this directory (kernel,
//	                     initramfs, data.img) instead of downloading the pin's
//	                     guest; the pin's Lite payload is still the payload
//	GUESTBOOT_CACHE_DIR  keep the downloaded guest zip here between runs
//	GUESTBOOT_ACCEL      force an accelerator (tcg, kvm); default Accel()
//	GUESTBOOT_TIMEOUT    how long the guest may take to answer, e.g. 6m
//	GUESTBOOT_SUMMARY    a file to append the phase table to; default
//	                     $GITHUB_STEP_SUMMARY

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// bootPhase is one milestone of a guest boot, found in the guest's console.
type bootPhase struct {
	name string
	// match reports whether a console line reaches the phase.
	match func(line string) bool
	at    time.Duration
	line  string
}

// bootWatch collects the guest's console with the time each line arrived and
// stamps the phases.
type bootWatch struct {
	mu     sync.Mutex
	start  time.Time
	lines  []string
	phases []*bootPhase
	boots  int
	panics []string
}

func (w *bootWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\r\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		w.lines = append(w.lines, line)
		if strings.Contains(line, "[init] starting wrapper-manager with") {
			w.boots++
		}
		if strings.Contains(line, "Kernel panic") || strings.Contains(line, "panic:") || strings.Contains(line, "fatal error:") {
			w.panics = append(w.panics, line)
		}
		for _, ph := range w.phases {
			if ph.at == 0 && ph.match(line) {
				ph.at, ph.line = time.Since(w.start), line
			}
		}
	}
	return len(p), nil
}

func (w *bootWatch) tail(n int) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	from := max(0, len(w.lines)-n)
	return strings.Join(w.lines[from:], "\n")
}

// seen returns the line that reached the phase, or "".
func (w *bootWatch) seen(name string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ph := range w.phases {
		if ph.name == name {
			return ph.line
		}
	}
	return ""
}

// progress lists the phases in order, marking each reached or not.
func (w *bootWatch) progress() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var b strings.Builder
	for _, ph := range w.phases {
		mark := "not reached"
		if ph.at != 0 {
			mark = "reached after " + ph.at.Round(100*time.Millisecond).String()
		}
		fmt.Fprintf(&b, "  %s: %s\n", ph.name, mark)
	}
	return b.String()
}

func (w *bootWatch) console() (text string, boots int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.lines, "\n"), w.boots
}

func has(sub string) func(string) bool {
	return func(line string) bool { return strings.Contains(line, sub) }
}

// noCarrier reports whether init's eth0 line shows no link.
func noCarrier(line string) bool {
	return line != "" && strings.TrimSpace(strings.TrimPrefix(line, "[init] eth0 carrier:")) != "1"
}

// diagnose names the faults the console shows so far, one per line.
func (w *bootWatch) diagnose() string {
	var b strings.Builder
	console, _ := w.console()
	if w.seen("link up") != "" && w.seen("arguments reach init") == "" {
		b.WriteString("  the manager arguments never reached init: it started the manager with its defaults\n")
		if strings.Contains(console, "fw_cfg") && strings.Contains(console, "warning") {
			b.WriteString("  (QEMU warned about the fw_cfg item's name; see the console)\n")
		}
	}
	if line := w.seen("link up"); noCarrier(line) {
		fmt.Fprintf(&b, "  the guest has no network link: %s\n", line)
	}
	if line := w.seen("network up"); badNetwork(line) {
		fmt.Fprintf(&b, "  the guest cannot resolve names: %s\n", line)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range w.panics {
		fmt.Fprintf(&b, "  panic: %s\n", l)
		break
	}
	return b.String()
}

// badNetwork reports whether init's DNS probe line says the lookup failed.
func badNetwork(line string) bool {
	for _, bad := range []string{"can't resolve", "NXDOMAIN", "timed out", "unreachable", "bad address", "no servers"} {
		if strings.Contains(line, bad) {
			return true
		}
	}
	return false
}

func pinForBoot(t *testing.T) Pin {
	t.Helper()
	path := os.Getenv("GUESTBOOT_PIN_FILE")
	if path == "" {
		return Builtin
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p Pin
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("read pin %s: %v", path, err)
	}
	return p
}

func TestGuestBoot(t *testing.T) {
	// A pin file for another guest has usually expired its artifact by the
	// time it is read; only a download needs it live.
	pin := pinForBoot(t)
	guestDir := os.Getenv("GUESTBOOT_GUEST_DIR")
	now := time.Now()
	if guestDir == "" {
		if err := pin.valid(now); err != nil {
			t.Fatalf("the pin to boot: %v", err)
		}
	} else if err := pin.Lite.valid(now); err != nil {
		t.Fatalf("the payload pin to boot: %v", err)
	}

	qemu, err := FindQEMU("", runtime.GOOS, exec.LookPath, func(p string) bool { _, err := os.Stat(p); return err == nil })
	if err != nil {
		t.Fatalf("%v: install qemu-system-x86", err)
	}
	accel := Accel(runtime.GOOS, runtime.GOARCH, KVMUsable())
	if a := os.Getenv("GUESTBOOT_ACCEL"); a != "" {
		accel = a
	}
	timeout := 4 * time.Minute
	if accel == "tcg" {
		timeout = 15 * time.Minute
	}
	if v := os.Getenv("GUESTBOOT_TIMEOUT"); v != "" {
		if timeout, err = time.ParseDuration(v); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("qemu %s, accelerator %s, up to %s to answer", qemu, accel, timeout)

	home := Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	report := map[string]time.Duration{}
	timed := func(name string, f func() error) {
		t.Helper()
		start := time.Now()
		if err := f(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		report[name] = time.Since(start)
	}
	if guestDir != "" {
		timed("guest files", func() error {
			if err := home.prepare(); err != nil {
				return err
			}
			_, err := home.unpack(guestDir, Installed{ID: "guestboot", Lite: pin.Lite, Source: "guestboot of " + guestDir, At: now})
			return err
		})
	} else {
		timed("guest download", func() error {
			cache := os.Getenv("GUESTBOOT_CACHE_DIR")
			if cache == "" {
				cache = t.TempDir()
			}
			if err := os.MkdirAll(cache, 0o755); err != nil {
				return err
			}
			// Download keeps a whole, matching file as it is, so a cache hit
			// costs only the checksum; Install then finds the zip in place.
			cached := filepath.Join(cache, "guest-"+pin.SHA256[:12]+".zip")
			f := NightlyLink{Client: &http.Client{}}
			if err := Download(t.Context(), f, pin, cached, nil); err != nil {
				return err
			}
			if err := home.prepare(); err != nil {
				return err
			}
			if err := copyFile(cached, home.path("download-"+pin.SHA256[:12]+".zip"), 0o600); err != nil {
				return err
			}
			_, err := home.Install(t.Context(), f, pin, now, nil)
			return err
		})
	}
	in, err := home.Current()
	if err != nil {
		t.Fatal(err)
	}
	if in.Kind != ManagerGuest {
		t.Fatalf("installed a %q guest, want %q", in.Kind, ManagerGuest)
	}

	watch := &bootWatch{phases: []*bootPhase{
		{name: "arguments reach init", match: has("[init] reading manager args from fw_cfg")},
		{name: "link up", match: has("[init] eth0 carrier:")},
		{name: "network up", match: has("[init] dns test:")},
		{name: "payload download starts", match: has("downloading wrapper-lite from")},
		{name: "payload downloaded and verified", match: has("wrapper-lite ready")},
		{name: "manager listening", match: has("wrapperManager running at")},
	}}
	sup, err := NewSupervisor(home, qemu, accel, watch)
	if err != nil {
		t.Fatal(err)
	}
	oldReady, oldPoll := readyTimeout, pollEvery
	readyTimeout, pollEvery = timeout, time.Second
	defer func() { readyTimeout, pollEvery = oldReady, oldPoll }()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	watch.start = time.Now()
	go func() { defer close(done); sup.Run(ctx) }()
	stop := func() { cancel(); <-done }
	defer stop()

	fail := func(format string, args ...any) {
		t.Helper()
		t.Errorf(format, args...)
		t.Errorf("diagnosis:\n%s", watch.diagnose())
		t.Errorf("phases:\n%s", watch.progress())
		t.Errorf("the guest's console, last 60 lines:\n%s", watch.tail(60))
		t.FailNow()
	}
	deadline := time.NewTimer(timeout + 30*time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	var served time.Duration
wait:
	for {
		m := sup.Managed()
		watch.mu.Lock()
		boots, panics := watch.boots, append([]string(nil), watch.panics...)
		watch.mu.Unlock()
		wantURL := in.Lite.URL()
		switch dl := watch.seen("payload download starts"); {
		case m.Mode == "serving":
			served = time.Since(watch.start)
			break wait
		case dl != "" && !strings.Contains(dl, wantURL):
			fail("the manager downloads a payload other than the pinned %s: its arguments did not reach it", wantURL)
		case watch.seen("arguments reach init") == "" && watch.seen("link up") != "":
			fail("the manager arguments did not reach init")
		case noCarrier(watch.seen("link up")), badNetwork(watch.seen("network up")):
			fail("the guest has no working network")
		case len(panics) > 0:
			fail("the guest panicked: %s", panics[0])
		case m.Mode == "failed":
			fail("the guest failed to start: %s", m.LastError)
		case boots > 1:
			fail("the guest restarted: %d boots", boots)
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			fail("the guest did not answer /status in %s (mode %s)", timeout, m.Mode)
		}
	}

	// serving means /status answered; check what led there, and that it holds.
	for _, ph := range watch.phases {
		if ph.at == 0 {
			fail("the guest answered, but its console never showed %q", ph.name)
		}
	}
	if m := sup.Managed(); m.AccelFallback {
		t.Errorf("%s could not start the guest, which fell back to tcg", accel)
	}
	console, _ := watch.console()
	// The guest prints the arguments one per line, a bare-lite init on one; only
	// their order and values matter.
	if want := "[init] starting wrapper-manager with: " + strings.Join(managerArgs(in.Lite), " "); !strings.Contains(strings.Join(strings.Fields(console), " "), want) {
		fail("init did not start the manager with the pinned payload: want %q", want)
	}
	time.Sleep(3 * time.Second)
	if _, err := statusOf(t.Context(), in.Port); err != nil {
		fail("/status stopped answering: %v", err)
	}
	if _, boots := watch.console(); boots != 1 {
		fail("%d boots, want 1", boots)
	}

	rows := [][2]string{}
	if d, ok := report["guest download"]; ok {
		rows = append(rows, [2]string{"guest download and unpack", d.Round(100 * time.Millisecond).String()})
	}
	for _, ph := range watch.phases {
		rows = append(rows, [2]string{ph.name, ph.at.Round(100 * time.Millisecond).String()})
	}
	rows = append(rows, [2]string{"/status answers", served.Round(100 * time.Millisecond).String()})
	var out strings.Builder
	fmt.Fprintf(&out, "### Guest boot (%s, %s)\n\n| Phase | Since QEMU started |\n| --- | --- |\n", accel, filepath.Base(in.ID))
	for _, r := range rows {
		fmt.Fprintf(&out, "| %s | %s |\n", r[0], r[1])
	}
	t.Log("\n" + out.String())
	summary := os.Getenv("GUESTBOOT_SUMMARY")
	if summary == "" {
		summary = os.Getenv("GITHUB_STEP_SUMMARY")
	}
	if summary != "" {
		if f, err := os.OpenFile(summary, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = io.WriteString(f, out.String()+"\n")
			f.Close()
		}
	}
}
