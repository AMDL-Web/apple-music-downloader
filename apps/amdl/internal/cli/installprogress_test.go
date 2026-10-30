package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"amdl/internal/wrapperhost"
)

func feed(r *installReport, clock *time.Time, step time.Duration, doneMB ...int64) {
	for _, d := range doneMB {
		*clock = clock.Add(step)
		r.report(wrapperhost.Progress{Phase: wrapperhost.Downloading, Done: d << 20, Total: 100 << 20})
	}
	r.report(wrapperhost.Progress{Phase: wrapperhost.Verifying})
	r.report(wrapperhost.Progress{Phase: wrapperhost.Unpacking})
	r.finish()
}

func TestInstallProgressToALogIsOneLinePerTenPercent(t *testing.T) {
	var b bytes.Buffer
	clock := time.Unix(0, 0)
	r := &installReport{w: &b, now: func() time.Time { return clock }}
	// Chunks of 1 MB a second for 100 MB: the 15 s rule also fires, so go
	// in 5 MB steps a second (under 15 s per 10%).
	var steps []int64
	for d := int64(0); d <= 100; d += 5 {
		steps = append(steps, d)
	}
	feed(r, &clock, time.Second, steps...)
	want := "downloading: 0 of 100 MB (0%)\n" +
		"downloading: 10 of 100 MB (10%)\n" +
		"downloading: 20 of 100 MB (20%)\n" +
		"downloading: 30 of 100 MB (30%)\n" +
		"downloading: 40 of 100 MB (40%)\n" +
		"downloading: 50 of 100 MB (50%)\n" +
		"downloading: 60 of 100 MB (60%)\n" +
		"downloading: 70 of 100 MB (70%)\n" +
		"downloading: 80 of 100 MB (80%)\n" +
		"downloading: 90 of 100 MB (90%)\n" +
		"downloading: 100 of 100 MB (100%)\n" +
		"checking the download's sha256\n" +
		"unpacking the guest\n"
	if b.String() != want {
		t.Errorf("log:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestInstallProgressToALogAlsoTicksEveryFifteenSeconds(t *testing.T) {
	var b bytes.Buffer
	clock := time.Unix(0, 0)
	r := &installReport{w: &b, now: func() time.Time { return clock }}
	feed(r, &clock, 16*time.Second, 1, 2, 3)
	if got := strings.Count(b.String(), "downloading:"); got != 3 {
		t.Errorf("%d download lines, want one per 16 s tick:\n%s", got, b.String())
	}
}

func TestInstallProgressOnATerminalRewritesOneLine(t *testing.T) {
	var b bytes.Buffer
	clock := time.Unix(0, 0)
	r := &installReport{w: &b, tty: true, now: func() time.Time { return clock }}
	// 50 ms apart: only the first, one 200 ms later and the last are drawn.
	feed(r, &clock, 50*time.Millisecond, 1, 2, 3, 4, 5, 100)
	got := b.String()
	if strings.Count(got, "\n") != 3 || !strings.HasPrefix(got, "\rdownloading: 1 of 100 MB (1%)") {
		t.Errorf("terminal output %q", got)
	}
	if !strings.Contains(got, "\rdownloading: 100 of 100 MB (100%)\nchecking the download's sha256\nunpacking the guest\n") {
		t.Errorf("the finished line is not ended before the next step: %q", got)
	}
	if strings.Contains(got, "\rdownloading: 2 of") {
		t.Errorf("a redraw inside the interval: %q", got)
	}
}

func TestInstallProgressQuietWritesNothing(t *testing.T) {
	var b bytes.Buffer
	e := &env{IO: IO{Stderr: &b, ErrTTY: true}, quiet: true}
	r := e.installReport()
	r.report(wrapperhost.Progress{Phase: wrapperhost.Downloading, Done: 1, Total: 2})
	e.progressf("opening line")
	if b.Len() != 0 {
		t.Errorf("--quiet wrote %q", b.String())
	}
}

func TestProgressfWritesToAFileToo(t *testing.T) {
	var b bytes.Buffer
	e := &env{IO: IO{Stderr: &b}} // ErrTTY false: stderr is a log
	e.progressf("downloading the wrapper guest (%d MB)", 96)
	if b.String() != "downloading the wrapper guest (96 MB)\n" {
		t.Errorf("got %q", b.String())
	}
}

// After a wrapper uninstall nothing is installed, so "enabled" (whether an
// installed guest should run) has nothing to say and is left out.
func TestWrapperUninstallOmitsEnabled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	src := t.TempDir()
	for _, n := range []string{"vmlinuz-lite-qemu", "lite-initramfs.cpio.gz", "data.img"} {
		if err := os.WriteFile(filepath.Join(src, n), []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runHere := func(args ...string) result {
		var out, errb bytes.Buffer
		code := Run(context.Background(), args, IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }})
		return result{code, out.String(), errb.String()}
	}
	if r := runHere("wrapper", "import", src); r.code != exitOK || !strings.Contains(r.stdout, `"enabled":true`) {
		t.Fatalf("import: exit %d, stdout %s, stderr %s", r.code, r.stdout, r.stderr)
	}
	r := runHere("wrapper", "uninstall")
	if r.code != exitOK {
		t.Fatalf("uninstall: exit %d, stderr %s", r.code, r.stderr)
	}
	if strings.Contains(r.stdout, `"enabled"`) || strings.Contains(r.stdout, `"installed"`) {
		t.Errorf("uninstall still reports an installed guest: %s", r.stdout)
	}
}

// Outside a terminal, wrapper uninstall --purge asks for --yes, and the
// command it suggests is one that works; after the purge the result names
// no log, which went with the wrapper's directory.
func TestWrapperPurgeTakesYes(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	src := t.TempDir()
	for _, n := range []string{"vmlinuz-lite-qemu", "lite-initramfs.cpio.gz", "data.img"} {
		if err := os.WriteFile(filepath.Join(src, n), []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runHere := func(args ...string) result {
		var out, errb bytes.Buffer
		code := Run(context.Background(), args, IO{Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errb,
			Getenv: func(string) string { return "" }})
		return result{code, out.String(), errb.String()}
	}
	if r := runHere("wrapper", "import", src); r.code != exitOK {
		t.Fatalf("import: %+v", r)
	}
	if r := runHere("wrapper", "uninstall", "--purge"); r.code != exitConfirm || errorCode(t, r.stderr) != "confirmation_required" {
		t.Fatalf("without --yes: %+v", r)
	}
	r := runHere("wrapper", "uninstall", "--purge", "--yes")
	if r.code != exitOK || strings.Contains(r.stdout, `"log"`) || !strings.Contains(r.stdout, `"next"`) {
		t.Fatalf("with --yes: %+v", r)
	}
}
