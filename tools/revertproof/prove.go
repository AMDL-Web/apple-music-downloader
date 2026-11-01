package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type options struct {
	root    string
	tests   []string
	run     string
	base    string
	count   int
	keep    []string
	cmd     string
	timeout time.Duration
}

// Proof is the tool's JSON result.
type Proof struct {
	Verdict       string   `json:"verdict"`
	Reason        string   `json:"reason,omitempty"`
	Base          string   `json:"base,omitempty"`
	Head          string   `json:"head,omitempty"`
	Dirty         bool     `json:"dirty,omitempty"`
	Tests         []string `json:"tests"`
	Run           string   `json:"run,omitempty"`
	Cmd           string   `json:"cmd,omitempty"`
	RevertedFiles []string `json:"revertedFiles"`
	AddedKept     []string `json:"addedKept"`
	KeptAtHead    []string `json:"keptAtHead,omitempty"`
	Red           *Run     `json:"red,omitempty"`
	Green         []Run    `json:"green,omitempty"`
	usage         bool
}

func prove(ctx context.Context, o options) Proof {
	p := Proof{Tests: o.tests, Cmd: o.cmd, RevertedFiles: []string{}, AddedKept: []string{}}
	fail := func(err error) Proof {
		p.Verdict, p.Reason = verdictError, err.Error()
		return p
	}
	head, err := git(ctx, o.root, "rev-parse", "HEAD")
	if err != nil {
		return fail(err)
	}
	base, err := git(ctx, o.root, "merge-base", o.base, head)
	if err != nil {
		return fail(err)
	}
	p.Base, p.Head = base, head
	status, err := git(ctx, o.root, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return fail(err)
	}
	p.Dirty = status != ""
	diff, err := git(ctx, o.root, "diff", "--name-status", "--no-renames", "-z", base, head)
	if err != nil {
		return fail(err)
	}
	changes, err := parseNameStatus(diff)
	if err != nil {
		return fail(err)
	}
	fp := planFiles(changes, o.keep)
	p.RevertedFiles = append(p.RevertedFiles, fp.reverted...)
	p.AddedKept = append(p.AddedKept, fp.addedKept...)
	p.KeptAtHead = fp.keptAtHead

	pkgs, run := o.tests, o.run
	if o.cmd == "" {
		if pkgs, run, err = resolveTests(ctx, o, head); err != nil {
			p.usage = true
			return fail(err)
		}
	}
	p.Run = run
	if len(fp.reverted) == 0 {
		p.Verdict, p.Reason = verdictNotRed, "no non-test file that exists at the base changed, so nothing was reverted"
		return p
	}

	tmp, err := os.MkdirTemp("", "revertproof-")
	if err != nil {
		return fail(err)
	}
	tree := filepath.Join(tmp, "tree")
	if _, err := git(ctx, o.root, "worktree", "add", "-q", "--detach", tree, head); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	defer func() {
		// Not ctx: after an interrupt the worktree still has to go.
		if _, err := git(context.Background(), o.root, "worktree", "remove", "--force", tree); err != nil {
			fmt.Fprintln(os.Stderr, "revertproof: the temporary worktree stays:", err)
		}
		os.Remove(tmp)
	}()

	if _, err := git(ctx, tree, append([]string{"checkout", base, "--"}, fp.reverted...)...); err != nil {
		return fail(err)
	}
	red := runTests(ctx, o, tree, pkgs, run, 1, false)
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	p.Red = &red
	redV, redReason := redVerdict(red)
	if redV == verdictError {
		p.Verdict, p.Reason = redV, redReason
		return p
	}
	if _, err := git(ctx, tree, "reset", "-q", "--hard", head); err != nil {
		return fail(err)
	}
	for _, oneCPU := range []bool{false, true} {
		count := o.count
		if oneCPU {
			count = 1
		}
		p.Green = append(p.Green, runTests(ctx, o, tree, pkgs, run, count, oneCPU))
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
	}
	greenV, greenReason := greenVerdict(p.Green)
	p.Verdict, p.Reason = finalVerdict(redV, redReason, greenV, greenReason)
	return p
}

// resolveTests turns -test values into go test packages. A _test.go file
// stands for its directory's package and, without -run, for its own Test
// functions as they are at HEAD.
func resolveTests(ctx context.Context, o options, head string) ([]string, string, error) {
	var pkgs, names []string
	files := 0
	for _, t := range o.tests {
		if !strings.HasSuffix(t, "_test.go") {
			if !slices.Contains(pkgs, t) {
				pkgs = append(pkgs, t)
			}
			continue
		}
		files++
		rel := path.Clean(filepath.ToSlash(t))
		src, err := git(ctx, o.root, "show", head+":"+rel)
		if err != nil {
			return nil, "", fmt.Errorf("%s is not a file at HEAD; give paths from the repository root", t)
		}
		ns, err := testNames(rel, []byte(src))
		if err != nil {
			return nil, "", err
		}
		if len(ns) == 0 {
			return nil, "", fmt.Errorf("%s declares no Test function", t)
		}
		names = append(names, ns...)
		pkg := "./" + path.Dir(rel)
		if path.Dir(rel) == "." {
			pkg = "."
		}
		if !slices.Contains(pkgs, pkg) {
			pkgs = append(pkgs, pkg)
		}
	}
	run := o.run
	if run == "" && files == len(o.tests) {
		run = runRegexp(names)
	}
	return pkgs, run, nil
}

func runTests(ctx context.Context, o options, dir string, pkgs []string, run string, count int, oneCPU bool) Run {
	ctx, cancel := context.WithTimeout(ctx, o.timeout+5*time.Minute)
	defer cancel()
	if o.cmd != "" {
		return runCommand(ctx, o.cmd, dir, count, oneCPU)
	}
	args := []string{"test", "-json", fmt.Sprintf("-count=%d", count), "-timeout", o.timeout.String()}
	if oneCPU {
		args = append(args, "-cpu", "1")
	}
	if run != "" {
		args = append(args, "-run", run)
	}
	args = append(args, pkgs...)
	r := Run{Args: append([]string{"go"}, args...)}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r.Exit = exitCode(cmd.Run(), &r)
	r.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
	parseGoTest(&r, stdout.String())
	if r.Exit != 0 {
		for _, l := range strings.Split(stderr.String(), "\n") {
			r.addTail(l)
		}
	}
	return r
}

// runCommand runs a -cmd command count times; oneCPU sets GOMAXPROCS=1,
// which a Go program in the command honors.
func runCommand(ctx context.Context, command, dir string, count int, oneCPU bool) Run {
	r := Run{Args: []string{"sh", "-c", command}}
	if oneCPU {
		r.Args = append([]string{"env", "GOMAXPROCS=1"}, r.Args...)
	}
	for range count {
		cmd := exec.CommandContext(ctx, r.Args[0], r.Args[1:]...)
		cmd.Dir = dir
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		exit := exitCode(cmd.Run(), &r)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			r.TimedOut = true
			return r
		}
		if exit != 0 {
			r.Exit = exit
		}
		parseCommand(&r, exit, out.String())
	}
	return r
}

func exitCode(err error, r *Run) int {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	r.addTail(err.Error())
	return -1
}

func summarize(w io.Writer, p Proof) {
	line := "revertproof: " + p.Verdict
	if p.Reason != "" {
		line += ": " + p.Reason
	}
	fmt.Fprintln(w, line)
	if p.Head == "" {
		return
	}
	fmt.Fprintf(w, "  base %.10s, head %.10s; reverted %d files, kept %d added files\n",
		p.Base, p.Head, len(p.RevertedFiles), len(p.AddedKept))
	if p.Dirty {
		fmt.Fprintln(w, "  the working tree has uncommitted changes; the proof used HEAD as committed")
	}
	if p.Red != nil {
		t := p.Red.total()
		fmt.Fprintf(w, "  red: %d passed, %d failed %v, %d panicked\n", t.Pass, t.Fail, p.Red.failedTests(), t.Panic)
		for _, l := range p.Red.Tail {
			fmt.Fprintln(w, "    "+l)
		}
	}
	for _, g := range p.Green {
		t := g.total()
		fmt.Fprintf(w, "  green: %d passed, %d failed: %s\n", t.Pass, t.Fail+t.Panic, strings.Join(g.Args, " "))
	}
}
