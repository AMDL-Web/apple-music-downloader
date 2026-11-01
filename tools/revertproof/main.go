// Command revertproof proves that a regression test guards the bug its change
// fixes. In a temporary git worktree at HEAD it puts every changed non-test file
// back to the base version and runs the named tests, which must fail on an
// assertion; then it runs them at HEAD, where they must pass, repeated and once
// on one CPU so that a flaky test does not pass by chance.
//
//	go run ./tools/revertproof -test internal/media/resume_test.go
//	go run ./tools/revertproof -test ./internal/media -run '^TestResume' -base origin/dev
//	go run ./tools/revertproof -test apps/console/jstest/album-progress.test.mjs \
//	    -cmd 'node --test apps/console/jstest/album-progress.test.mjs'
//
// The proof uses HEAD as committed and never touches the working tree or the
// index. The JSON result goes to stdout and a summary to stderr; the exit code
// is 0 only when the verdict is "proved".
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type strList []string

func (s *strList) String() string     { return strings.Join(*s, ",") }
func (s *strList) Set(v string) error { *s = append(*s, v); return nil }

const usageText = `usage: revertproof -test <pkg|file_test.go> [-test ...] [flags]

Puts every changed non-test file back to the base version in a temporary
worktree and runs the tests, which must fail an assertion; then runs them at
HEAD -count times and once with -cpu 1, where they must pass every time.

Test files stay at HEAD: *_test.go, testdata/, *.test.mjs. So do go.mod,
go.sum, the -keep paths and every file the change added (listed as addedKept:
a fix inside one cannot go red).

Verdicts, first match wins:
  error              git failed or a run timed out
  flaky              a test failed some of its runs at HEAD
  not-red            the tests pass without the fix, or nothing was reverted
  red-not-assertion  without the fix: a build failure, a panic, no test ran
  not-green          a test failed every run at HEAD
  proved             red on an assertion, then green in every run
Exit code: 0 proved, 1 any other verdict, 2 bad usage.

flags:
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("revertproof", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usageText)
		fs.PrintDefaults()
	}
	var o options
	var tests, keep strList
	fs.Var(&tests, "test", "package pattern (./internal/media) or _test.go file, from the repository root; repeatable (required)")
	fs.StringVar(&o.run, "run", "", "go test -run regexp; defaults to every Test function in the -test files, or all tests of a package")
	fs.StringVar(&o.base, "base", "origin/dev", "base ref; the proof reverts to its merge-base with HEAD")
	fs.IntVar(&o.count, "count", 10, "how many times the tests run at HEAD, before the one run with -cpu 1")
	fs.Var(&keep, "keep", "another file to keep at HEAD, such as a test helper; repeatable")
	fs.StringVar(&o.cmd, "cmd", "", "shell command to run instead of go test (a Node test, say), from the worktree root; red means a non-zero exit with an assertion error in its output")
	fs.DurationVar(&o.timeout, "timeout", 10*time.Minute, "limit for each test run")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	o.tests, o.keep = tests, keep
	if len(o.tests) == 0 || o.count < 1 || o.timeout <= 0 || fs.NArg() > 0 {
		fs.Usage()
		return 2
	}
	root, err := git(context.Background(), ".", "rev-parse", "--show-toplevel")
	if err != nil {
		fmt.Fprintln(os.Stderr, "revertproof: not in a git repository:", err)
		return 2
	}
	o.root = root

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	p := prove(ctx, o)
	if p.usage {
		fmt.Fprintln(os.Stderr, "revertproof:", p.Reason)
		return 2
	}
	summarize(os.Stderr, p)
	out, _ := json.MarshalIndent(p, "", "  ")
	fmt.Println(string(out))
	if p.Verdict != verdictProved {
		return 1
	}
	return 0
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
