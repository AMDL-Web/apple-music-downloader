package wrapperhost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"amdl/internal/sysproc"
)

// guest is one running QEMU, or a native manager process.
type guest struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	port  int           // the manager's loopback port
	done  chan struct{} // closed once QEMU has exited
	err   error         // QEMU's exit, valid after done
	// native is a manager running as a process here: it stops on SIGTERM,
	// and has no POST /shutdown (that powers a guest off).
	native bool

	// answered is set once the manager answered /status: only then can it
	// take POST /shutdown.
	answered atomic.Bool
	// spoke is set once the guest's init printed a line: QEMU and its
	// accelerator ran the guest.
	spoke atomic.Bool
	mu    sync.Mutex
	// accelErr is a line where QEMU, not the guest, names the accelerator.
	accelErr string
}

// startGuest starts QEMU with o, handing each console line, without its line
// ending, to onLine from one goroutine, and calls onExit once QEMU has
// exited. QEMU dies with this process.
func startGuest(qemu string, o RunOpts, onLine func(string), onExit func()) (*guest, error) {
	cmd := exec.Command(qemu, Args(o)...)
	tieToParent(cmd)
	return startProc(cmd, o.HostPort, false, onLine, onExit)
}

// startNative starts a native manager like startGuest a guest.
func startNative(cmd *exec.Cmd, port int, onLine func(string), onExit func()) (*guest, error) {
	tieNative(cmd)
	return startProc(cmd, port, true, onLine, onExit)
}

func startProc(cmd *exec.Cmd, port int, native bool, onLine func(string), onExit func()) (*guest, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	sysproc.NoConsole(cmd)
	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	pw.Close()
	if err := adopt(cmd.Process); err != nil {
		_ = cmd.Process.Kill() // it would outlive us
		pr.Close()
		return nil, err
	}
	g := &guest{cmd: cmd, stdin: stdin, port: port, native: native, done: make(chan struct{})}
	read := make(chan struct{})
	go func() {
		defer close(read)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			g.note(line)
			onLine(line)
		}
	}()
	go func() {
		<-read
		g.err = cmd.Wait()
		pr.Close()
		if onExit != nil {
			onExit()
		}
		close(g.done)
	}()
	return g, nil
}

// accelWords are what QEMU's own errors about an accelerator mention.
var accelWords = regexp.MustCompile(`(?i)\b(kvm|whpx|hvf|hv_[a-z_]+|accel(erator)?)\b`)

// note records what a console line says about who ran: the guest's init, or
// QEMU failing its accelerator.
func (g *guest) note(line string) {
	if strings.Contains(line, "[init]") {
		g.spoke.Store(true)
		return
	}
	if !g.spoke.Load() && accelWords.MatchString(line) {
		g.mu.Lock()
		if g.accelErr == "" {
			g.accelErr = line
		}
		g.mu.Unlock()
	}
}

// accelFailed returns, once QEMU has exited, QEMU's own line about the
// accelerator when that is why it exited: with an error, before the guest's
// init printed anything. A guest that ran and powered off, or panicked
// (panic=1 with -no-reboot), exits QEMU cleanly, and is no fault of the
// accelerator; nor is a QEMU that failed without naming it.
func (g *guest) accelFailed() string {
	if g.err == nil || g.spoke.Load() {
		return ""
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.accelErr
}

// Graceful stop bounds: the shutdown request, then the guest's power off.
var (
	shutdownAsk  = 2 * time.Second
	shutdownWait = 10 * time.Second
)

// stop powers the guest off and waits for QEMU. A guest whose manager
// answered is asked to sync and power off (POST /shutdown) and given
// shutdownWait to do it; one that does not, or has not answered yet, is
// killed. The data disk mounts with commit=1, so a kill loses at most the
// last second of writes.
func (g *guest) stop() {
	if g.native && !g.exited() && terminate(g.cmd.Process) == nil {
		select {
		case <-g.done:
			return
		case <-time.After(shutdownWait):
		}
	}
	if !g.native && g.answered.Load() && !g.exited() && g.askShutdown() {
		select {
		case <-g.done:
			return
		case <-time.After(shutdownWait):
		}
	}
	_ = g.cmd.Process.Kill() // already exited is fine
	<-g.done
}

// askShutdown sends POST /shutdown and reports whether the manager took it.
// A guest built before the endpoint answers 404, and is killed.
func (g *guest) askShutdown() bool {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownAsk)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/shutdown", g.port), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (g *guest) exited() bool {
	select {
	case <-g.done:
		return true
	default:
		return false
	}
}

// statusOf reads the guest's GET /status.
func statusOf(ctx context.Context, port int) (regions []string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/status", port), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	var body struct {
		Data struct {
			Regions []string `json:"regions"`
		} `json:"data"`
	}
	if err := jsonDecode(resp.Body, &body); err != nil {
		return nil, err
	}
	return body.Data.Regions, nil
}

// errExited means QEMU ended before the guest answered.
var errExited = errors.New("QEMU exited")

// waitReady waits until the guest answers /status, QEMU exits or timeout.
func (g *guest) waitReady(ctx context.Context, port int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if _, err := statusOf(ctx, port); err == nil {
			g.answered.Store(true)
			return nil
		}
		select {
		case <-g.done:
			return fmt.Errorf("%w: %v", errExited, g.err)
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("the wrapper did not answer within %s", timeout)
			}
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
