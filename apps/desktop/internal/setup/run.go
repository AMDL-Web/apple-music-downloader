package setup

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"amdl/internal/sysproc"
)

// Run is a package manager installing a tool.
type Run struct {
	// Tool is "ffmpeg" or "QEMU".
	Tool    string
	Command string
	// State is Working, then Ready or Failed; a canceled run is Failed
	// with Canceled set.
	State State
	// Lines are the last of what it printed, oldest first.
	Lines    []string
	Started  time.Time
	Ended    time.Time
	Err      string
	Canceled bool

	cancel context.CancelFunc
}

// runLimit bounds a package manager's install: Homebrew updates itself and
// builds dependencies first.
const runLimit = 30 * time.Minute

// keepLines is how much of a run's output stays.
const keepLines = 200

// postEvery spaces the output a run hands to the UI thread.
const postEvery = 100 * time.Millisecond

// Install runs t's install command, which the user agreed to, unless a run
// is under way or the app may not run it. It checks t again when the run
// ends.
func (s *Setup) Install(tool string, ffmpegOverride string) {
	t := s.tool(tool)
	if t == nil || len(t.Install.Args) == 0 || (s.Run != nil && s.Run.State == Working) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runLimit)
	r := &Run{Tool: tool, Command: t.Install.Command, State: Working, Started: s.deps.Now(), cancel: cancel}
	s.Run = r
	t.State = Working
	args := t.Install.Args

	var mu sync.Mutex
	var lines []string
	var partial strings.Builder
	pending := false
	flush := func() {
		mu.Lock()
		pending = false
		got := append([]string(nil), lines...)
		mu.Unlock()
		s.post(func() {
			if s.Run == r && r.State == Working {
				r.Lines = got
			}
		})
	}
	out := func(chunk string) {
		mu.Lock()
		lines = appendOutput(lines, &partial, chunk)
		schedule := !pending
		pending = true
		mu.Unlock()
		if schedule {
			time.AfterFunc(postEvery, flush)
		}
	}
	go func() {
		err := s.deps.Exec(ctx, args, out)
		canceled := errors.Is(ctx.Err(), context.Canceled)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = errors.New("did not finish within 30 minutes")
		}
		cancel()
		mu.Lock()
		if partial.Len() > 0 {
			lines = keep(append(lines, partial.String()))
			partial.Reset()
		}
		got := append([]string(nil), lines...)
		mu.Unlock()
		s.post(func() {
			if s.Run != r {
				return
			}
			r.Lines, r.Ended, r.cancel = got, s.deps.Now(), nil
			switch {
			case canceled:
				r.State, r.Canceled = Failed, true
			case err != nil:
				r.State, r.Err = Failed, err.Error()
			default:
				r.State = Ready
			}
			s.Recheck()
			s.Check(ffmpegOverride)
		})
	}()
}

// CancelRun stops the package manager under way.
func (s *Setup) CancelRun() {
	if s.Run != nil && s.Run.cancel != nil {
		s.Run.cancel()
	}
}

// DismissRun forgets a finished run's output.
func (s *Setup) DismissRun() {
	if s.Run != nil && s.Run.State != Working {
		s.Run = nil
	}
}

func (s *Setup) tool(name string) *Tool {
	switch name {
	case "ffmpeg":
		return &s.FFmpeg
	case "QEMU":
		return &s.QEMU
	}
	return nil
}

// ansi matches a terminal's escape sequences (colors, cursor moves), which
// winget and Homebrew print.
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

// appendOutput adds a chunk of output to lines, without escape sequences: a
// newline ends a line, and a carriage return starts it over, as a progress
// bar redraws itself.
func appendOutput(lines []string, partial *strings.Builder, chunk string) []string {
	for _, r := range ansi.ReplaceAllString(chunk, "") {
		switch r {
		case '\n':
			lines = keep(append(lines, partial.String()))
			partial.Reset()
		case '\r':
			partial.Reset()
		default:
			partial.WriteRune(r)
		}
	}
	return lines
}

func keep(lines []string) []string {
	if len(lines) > keepLines {
		return lines[len(lines)-keepLines:]
	}
	return lines
}

// execCommand runs args with no console window, its output to out. The
// program's directory goes first on PATH, so a package manager found off
// PATH finds its own tools.
func execCommand(ctx context.Context, args []string, out func(string)) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	sysproc.NoConsole(cmd)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(args[0])+string(os.PathListSeparator)+os.Getenv("PATH"))
	w := writerFunc(func(b []byte) (int, error) { out(string(b)); return len(b), nil })
	cmd.Stdout, cmd.Stderr = w, w
	// A package manager may leave a child holding the output open; stop
	// waiting for it once the manager itself is done.
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
