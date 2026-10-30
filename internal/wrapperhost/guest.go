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
	"strings"
	"time"

	"amdl/internal/sysproc"
)

// guest is one running QEMU.
type guest struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	done  chan struct{} // closed once QEMU has exited
	err   error         // QEMU's exit, valid after done
}

// startGuest starts QEMU with o, handing each console line, without its line
// ending, to onLine from one goroutine. QEMU dies with this process.
func startGuest(qemu string, o RunOpts, onLine func(string)) (*guest, error) {
	cmd := exec.Command(qemu, Args(o)...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = pw, pw
	tieToParent(cmd)
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
	g := &guest{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	read := make(chan struct{})
	go func() {
		defer close(read)
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			onLine(strings.TrimRight(sc.Text(), "\r"))
		}
	}()
	go func() {
		<-read
		g.err = cmd.Wait()
		pr.Close()
		close(g.done)
	}()
	return g, nil
}

// stop kills QEMU and waits for it. The guest's lite runs as init, so there
// is no clean shutdown to ask for; its disk is ext4, whose journal keeps it
// consistent, and lite's tokens are rewritten on their next refresh.
func (g *guest) stop() {
	_ = g.cmd.Process.Kill() // already exited is fine
	<-g.done
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
