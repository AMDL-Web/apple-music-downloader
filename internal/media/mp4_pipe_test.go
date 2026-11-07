package media

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"amdl/internal/config"
)

func writePipeTestCommand(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("pipe lifecycle test uses a POSIX helper script")
	}
	path := filepath.Join(t.TempDir(), "fake-ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newPipeTestProcessor(t *testing.T, command string) *MP4Processor {
	t.Helper()
	cfg := config.Config{}
	cfg.Download.TempDir = t.TempDir()
	cfg.Tools.FFmpeg = command
	return newMP4Processor(cfg)
}

func TestProduceToFlatFilePreservesProducerAndFFmpegFailures(t *testing.T) {
	command := writePipeTestCommand(t, "cat >/dev/null\nexit 23")
	p := newPipeTestProcessor(t, command)
	producerErr := errors.New("forced producer failure")
	var callbackCalls atomic.Int32

	err := p.produceToFlatFile(context.Background(), filepath.Join(t.TempDir(), "out.m4a"), func(w io.Writer) error {
		if _, err := io.WriteString(w, "partial fragmented mp4"); err != nil {
			return err
		}
		return producerErr
	}, func() { callbackCalls.Add(1) })
	if !errors.Is(err, producerErr) {
		t.Fatalf("pipeline error = %v, want producer error in chain", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("pipeline error = %v, want ffmpeg exit status 23 in chain", err)
	}
	if got := callbackCalls.Load(); got != 0 {
		t.Fatalf("afterInput called %d times after producer failure, want 0", got)
	}
}

func TestProduceToFlatFileCallbackPrecedesWaitAndCancellationReapsChild(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "waiting")
	// The marker is written only after stdin reaches EOF. exec replaces the
	// shell so CommandContext kills the actual sleeper and cmd.Wait can reap it
	// without leaving a descendant holding the stderr pipe open.
	command := writePipeTestCommand(t, "cat >/dev/null\nprintf ready > \""+marker+"\"\nexec sleep 30")
	p := newPipeTestProcessor(t, command)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	callback := make(chan struct{}, 1)
	done := make(chan error, 1)
	outPath := filepath.Join(t.TempDir(), "out.m4a")

	go func() {
		done <- p.produceToFlatFile(ctx, outPath, func(w io.Writer) error {
			_, err := io.WriteString(w, strings.Repeat("fragment", 1024))
			return err
		}, func() { callback <- struct{}{} })
	}()

	select {
	case <-callback:
	case err := <-done:
		t.Fatalf("pipeline returned before afterInput callback: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("afterInput callback was not published after stdin close")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake ffmpeg did not enter its post-input wait")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case err := <-done:
		t.Fatalf("pipeline returned before ffmpeg flush/wait completed: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled pipeline error = %v, want context.Canceled in chain", err)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("canceled pipeline error = %v, want reaped process error in chain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceled pipeline did not reap ffmpeg promptly")
	}
}

func TestProduceToFlatFileEarlyExitUnblocksProducer(t *testing.T) {
	command := writePipeTestCommand(t, "exit 17")
	p := newPipeTestProcessor(t, command)
	done := make(chan error, 1)
	outPath := filepath.Join(t.TempDir(), "out.m4a")

	go func() {
		done <- p.produceToFlatFile(context.Background(), outPath, func(w io.Writer) error {
			// More than a pipe buffer ensures a child that consumes nothing must
			// interrupt this write with EPIPE rather than let production finish.
			_, err := io.CopyN(w, zeroReader{}, 8<<20)
			return err
		}, nil)
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "exit status 17") {
			t.Fatalf("early-exit pipeline error = %v, want exit status 17", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("producer remained blocked after ffmpeg exited")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// The integrity check's error is what an operator reads on a failed item, so it
// must carry ffmpeg's own diagnosis.
func TestCheckIntegrityFileReportsFFmpegDiagnostics(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.m4a")
	if err := os.WriteFile(file, []byte("not audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		script string
		want   []string
	}{
		{
			name:   "decoder errors and failing exit",
			script: `echo "[aac @ 0x1] channel element 0.0 is not allocated" >&2; exit 1`,
			want:   []string{"ffmpeg decode: exit status 1", "channel element 0.0 is not allocated"},
		},
		{
			name:   "decoder errors with a clean exit",
			script: `echo "[alac @ 0x1] invalid samples per frame" >&2`,
			want:   []string{"ffmpeg decode reported errors", "invalid samples per frame"},
		},
		{
			name:   "failing exit without output",
			script: `exit 3`,
			want:   []string{"ffmpeg decode: exit status 3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newPipeTestProcessor(t, writePipeTestCommand(t, tt.script))
			err := p.checkIntegrityFile(context.Background(), file)
			if err == nil {
				t.Fatal("check passed, want an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}

	t.Run("clean decode", func(t *testing.T) {
		p := newPipeTestProcessor(t, writePipeTestCommand(t, "exit 0"))
		if err := p.checkIntegrityFile(context.Background(), file); err != nil {
			t.Fatalf("clean check = %v, want nil", err)
		}
	})

	t.Run("long output is capped", func(t *testing.T) {
		// 200 lines of 41 bytes is 8200 bytes of diagnostics.
		p := newPipeTestProcessor(t, writePipeTestCommand(t,
			`i=0; while [ $i -lt 200 ]; do echo "[aac @ 0x1] error decoding packet $i" >&2; i=$((i+1)); done; exit 1`))
		err := p.checkIntegrityFile(context.Background(), file)
		if err == nil {
			t.Fatal("check passed, want an error")
		}
		if len(err.Error()) > maxToolDiagnosticBytes+200 {
			t.Fatalf("error is %d bytes, want it capped near %d", len(err.Error()), maxToolDiagnosticBytes)
		}
		if !strings.Contains(err.Error(), "error decoding packet 0") || !strings.Contains(err.Error(), "more bytes)") {
			t.Fatalf("error = %q, want the first lines and a truncation note", err)
		}
	})
}
