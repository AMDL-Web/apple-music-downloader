package setup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/wrapperhost"
)

// harness runs a Setup whose posts queue up until the test drains them,
// as the UI thread would run them.
type harness struct {
	*Setup
	q chan func()
}

func newHarness(t *testing.T, deps Deps) *harness {
	t.Helper()
	h := &harness{q: make(chan func(), 1024)}
	if deps.Home.Dir == "" {
		deps.Home = wrapperhost.Home{Dir: filepath.Join(t.TempDir(), "wrapper")}
	}
	if deps.LookPath == nil {
		deps.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	}
	if deps.Exists == nil {
		deps.Exists = func(string) bool { return false }
	}
	if deps.FFmpeg == nil {
		deps.FFmpeg = func(string) (string, string, error) { return "", "", errors.New("ffmpeg not found") }
	}
	h.Setup = New(func(fn func()) { h.q <- fn }, deps)
	return h
}

// until runs posted functions until ok holds, failing after a few seconds.
func (h *harness) until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !ok() {
		select {
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatalf("waited for %s", what)
		}
	}
}

func files(names ...string) func(string) bool {
	return func(p string) bool {
		for _, n := range names {
			if p == n {
				return true
			}
		}
		return false
	}
}

func TestCheckOffersThePlatformsPackageManager(t *testing.T) {
	// From Finder, Homebrew is off PATH but where it installs itself.
	h := newHarness(t, Deps{GOOS: "darwin", Exists: files("/opt/homebrew/bin/brew")})
	h.Check("")
	if h.FFmpeg.State != Missing || h.FFmpeg.Install.Command != "brew install ffmpeg" ||
		strings.Join(h.FFmpeg.Install.Args, " ") != "/opt/homebrew/bin/brew install ffmpeg" {
		t.Errorf("ffmpeg on a Mac with Homebrew: %+v", h.FFmpeg)
	}
	if h.QEMU.State != Missing || h.QEMU.Install.Command != "brew install qemu" || len(h.QEMU.Install.Args) == 0 {
		t.Errorf("QEMU on a Mac with Homebrew: %+v", h.QEMU)
	}
	if h.Guest.State != Missing || h.Guest.Size != wrapperhost.Builtin.Size {
		t.Errorf("guest before any install: %+v", h.Guest)
	}

	h = newHarness(t, Deps{GOOS: "darwin"})
	h.Check("")
	if in := h.FFmpeg.Install; in.Args != nil || in.URL != "https://brew.sh" {
		t.Errorf("without Homebrew the app cannot install ffmpeg: %+v", in)
	}

	h = newHarness(t, Deps{GOOS: "windows", LookPath: func(n string) (string, error) {
		if n == "winget" {
			return `C:\Users\me\AppData\Local\Microsoft\WindowsApps\winget.exe`, nil
		}
		return "", errors.New("not found")
	}})
	h.Check("")
	if in := h.QEMU.Install; !strings.Contains(in.Command, "--accept-source-agreements") || !strings.HasSuffix(in.Args[0], "winget.exe") {
		t.Errorf("winget must run without prompts, as shown: %+v", in)
	}

	h = newHarness(t, Deps{GOOS: "linux", LookPath: func(n string) (string, error) {
		if n == "apt-get" {
			return "/usr/bin/apt-get", nil
		}
		return "", errors.New("not found")
	}})
	h.Check("")
	if in := h.FFmpeg.Install; in.Command != "sudo apt-get install ffmpeg" || in.Args != nil {
		t.Errorf("a sudo install is only shown: %+v", in)
	}
}

func TestCheckFindsInstalledTools(t *testing.T) {
	h := newHarness(t, Deps{GOOS: "darwin", Exists: files("/opt/homebrew/bin/qemu-system-x86_64"),
		FFmpeg: func(string) (string, string, error) { return "/opt/homebrew/bin/ffmpeg", "system", nil }})
	h.Check("")
	if h.FFmpeg.State != Ready || h.FFmpeg.Source != "system" || h.QEMU.State != Ready || h.QEMU.Path != "/opt/homebrew/bin/qemu-system-x86_64" {
		t.Errorf("ffmpeg %+v, QEMU %+v", h.FFmpeg, h.QEMU)
	}
}

// installing is a Setup on a Mac with Homebrew whose ffmpeg appears once
// found is set.
func installing(t *testing.T, run func(ctx context.Context, out func(string)) error) (*harness, *bool) {
	found := false
	var mu sync.Mutex
	h := newHarness(t, Deps{GOOS: "darwin", Exists: files("/opt/homebrew/bin/brew"),
		FFmpeg: func(string) (string, string, error) {
			mu.Lock()
			defer mu.Unlock()
			if found {
				return "/opt/homebrew/bin/ffmpeg", "system", nil
			}
			return "", "", errors.New("not found")
		},
		Exec: func(ctx context.Context, args []string, out func(string)) error {
			if args[0] != "/opt/homebrew/bin/brew" {
				return errors.New("ran " + args[0])
			}
			err := run(ctx, out)
			if err == nil {
				mu.Lock()
				found = true
				mu.Unlock()
			}
			return err
		}})
	h.Check("")
	return h, &found
}

func TestInstallStreamsTheOutputThenChecksAgain(t *testing.T) {
	h, _ := installing(t, func(_ context.Context, out func(string)) error {
		out("==> Fetching ffmpeg\n")
		out("####   10.0%\r#########  100.0%\n")
		out("==> Pouring ffmpeg")
		return nil
	})
	h.Install("ffmpeg", "")
	if h.Run == nil || h.Run.State != Working || h.FFmpeg.State != Working {
		t.Fatalf("run %+v, ffmpeg %+v", h.Run, h.FFmpeg)
	}
	h.until(t, "the run to end", func() bool { return h.Run.State != Working })
	want := []string{"==> Fetching ffmpeg", "#########  100.0%", "==> Pouring ffmpeg"}
	if h.Run.State != Ready || strings.Join(h.Run.Lines, "|") != strings.Join(want, "|") {
		t.Errorf("run %v, lines %q, want %q", h.Run.State, h.Run.Lines, want)
	}
	if h.FFmpeg.State != Ready {
		t.Errorf("ffmpeg after its install: %+v", h.FFmpeg)
	}
}

func TestInstallFailsOrIsCanceled(t *testing.T) {
	h, _ := installing(t, func(_ context.Context, out func(string)) error {
		out("Error: no bottle available\n")
		return errors.New("exit status 1")
	})
	h.Install("ffmpeg", "")
	h.until(t, "the run to fail", func() bool { return h.Run.State != Working })
	if h.Run.State != Failed || h.Run.Err != "exit status 1" || h.Run.Lines[0] != "Error: no bottle available" || h.FFmpeg.State != Missing {
		t.Errorf("run %+v, ffmpeg %+v", h.Run, h.FFmpeg)
	}

	h, _ = installing(t, func(ctx context.Context, _ func(string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	h.Install("ffmpeg", "")
	h.CancelRun()
	h.until(t, "the run to stop", func() bool { return h.Run.State != Working })
	if !h.Run.Canceled || h.FFmpeg.State != Missing {
		t.Errorf("canceled run %+v, ffmpeg %+v", h.Run, h.FFmpeg)
	}
}

// guestZip is a qemu-assets artifact holding the guest's three files.
func guestZip(t *testing.T) ([]byte, wrapperhost.Pin) {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range []string{"vmlinuz-lite-qemu", "lite-initramfs.cpio.gz", "data.img"} {
		// Stored, not deflated, so the download takes many chunks.
		w, err := z.CreateHeader(&zip.FileHeader{Name: "qemu-assets/" + name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		w.Write(bytes.Repeat([]byte(name), 512))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), wrapperhost.Pin{Commit: "bde3335f47a30518e1f53f79cbae94bf76b27fb3", ArtifactID: 1,
		SHA256: hex.EncodeToString(sum[:]), Size: int64(buf.Len())}
}

// clock is a Now that moves a second each time it is read, so no progress
// report falls under reportEvery.
func clock() func() time.Time {
	var n atomic.Int64
	return func() time.Time { return time.Unix(n.Add(1), 0) }
}

// fetcher serves body from an offset in small chunks; hold, when set, is
// closed by the test to let the rest through.
type fetcher struct {
	body    []byte
	offsets []int64
	hold    chan struct{}
	mu      sync.Mutex
}

func (f *fetcher) Fetch(ctx context.Context, _ wrapperhost.Pin, offset int64, w io.Writer) error {
	f.mu.Lock()
	f.offsets = append(f.offsets, offset)
	f.mu.Unlock()
	for i := offset; i < int64(len(f.body)); i += 1024 {
		if f.hold != nil && i >= int64(len(f.body))/2 {
			select {
			case <-f.hold:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if _, err := w.Write(f.body[i:min(i+1024, int64(len(f.body)))]); err != nil {
			return err
		}
	}
	return nil
}

func TestInstallGuestDownloadsChecksAndInstalls(t *testing.T) {
	body, pin := guestZip(t)
	f := &fetcher{body: body}
	h := newHarness(t, Deps{GOOS: "darwin", Now: clock(), Fetcher: f, Pin: func(context.Context) (wrapperhost.Pin, error) { return pin, nil }})
	h.Check("")
	h.InstallGuest()
	sawProgress := false
	h.until(t, "the install", func() bool {
		if h.Guest.State == Working && h.Guest.Progress.Done > 0 {
			sawProgress = true
		}
		return h.Guest.State != Working
	})
	if h.Guest.State != Ready || h.Guest.Installed.Commit != pin.Commit || h.Guest.Installed.SHA256 != pin.SHA256 {
		t.Fatalf("guest %+v", h.Guest)
	}
	if !sawProgress || h.Guest.Size != pin.Size {
		t.Errorf("progress shown %v, size %d want %d", sawProgress, h.Guest.Size, pin.Size)
	}
	if _, err := os.Stat(h.deps.Home.ImagePath()); err != nil {
		t.Errorf("no account disk after the first install: %v", err)
	}
}

func TestInstallGuestRejectsABadDownload(t *testing.T) {
	body, pin := guestZip(t)
	body[len(body)/2] ^= 0xff
	h := newHarness(t, Deps{Fetcher: &fetcher{body: body}, Pin: func(context.Context) (wrapperhost.Pin, error) { return pin, nil }})
	h.InstallGuest()
	h.until(t, "the install to fail", func() bool { return h.Guest.State != Working })
	if h.Guest.State != Failed || !strings.Contains(h.Guest.Err, "does not match") {
		t.Errorf("guest %+v", h.Guest)
	}
}

func TestCancelGuestKeepsTheDownloadForTheNextTry(t *testing.T) {
	body, pin := guestZip(t)
	f := &fetcher{body: body, hold: make(chan struct{})}
	h := newHarness(t, Deps{Now: clock(), Fetcher: f, Pin: func(context.Context) (wrapperhost.Pin, error) { return pin, nil }})
	h.InstallGuest()
	h.until(t, "half the download", func() bool { return h.Guest.Progress.Done >= pin.Size/2-1024 })
	h.CancelGuest()
	h.until(t, "the cancel", func() bool { return h.Guest.State != Working })
	if h.Guest.State != Missing || h.Guest.Err != "" {
		t.Fatalf("canceled guest %+v", h.Guest)
	}
	close(f.hold)
	h.InstallGuest()
	h.until(t, "the resumed install", func() bool { return h.Guest.State != Working })
	if h.Guest.State != Ready || len(f.offsets) != 2 || f.offsets[1] == 0 {
		t.Errorf("guest %+v, fetched from %v; the second try should resume", h.Guest, f.offsets)
	}
}

func TestPinFailureFailsTheGuest(t *testing.T) {
	h := newHarness(t, Deps{Pin: func(context.Context) (wrapperhost.Pin, error) {
		return wrapperhost.Pin{}, errors.New("no current wrapper download")
	}})
	h.InstallGuest()
	h.until(t, "the failure", func() bool { return h.Guest.State != Working })
	if h.Guest.State != Failed || h.Guest.Err != "no current wrapper download" {
		t.Errorf("guest %+v", h.Guest)
	}
}
