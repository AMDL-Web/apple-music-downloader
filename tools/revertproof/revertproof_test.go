package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The streams are go test -json output from Go 1.27, trimmed.
const (
	assertionStream = `{"Action":"start","Package":"p/a"}
{"Action":"run","Package":"p/a","Test":"TestOK"}
{"Action":"pass","Package":"p/a","Test":"TestOK","Elapsed":0}
{"Action":"run","Package":"p/a","Test":"TestBad"}
{"Action":"run","Package":"p/a","Test":"TestBad/sub"}
{"Action":"output","Package":"p/a","Test":"TestBad/sub","Output":"    a_test.go:4: want 1, got 2\n","OutputType":"error"}
{"Action":"output","Package":"p/a","Test":"TestBad/sub","Output":"--- FAIL: TestBad/sub (0.00s)\n","OutputType":"frame"}
{"Action":"fail","Package":"p/a","Test":"TestBad/sub","Elapsed":0}
{"Action":"fail","Package":"p/a","Test":"TestBad","Elapsed":0}
{"Action":"output","Package":"p/a","Output":"FAIL\tp/a\t0.002s\n","OutputType":"frame"}
{"Action":"fail","Package":"p/a","Elapsed":0.003}
`
	buildFailStream = `{"ImportPath":"p/b [p/b.test]","Action":"build-output","Output":"# p/b [p/b.test]\n"}
{"ImportPath":"p/b [p/b.test]","Action":"build-output","Output":"b/b_test.go:3:28: undefined: undefined\n"}
{"ImportPath":"p/b [p/b.test]","Action":"build-fail"}
{"Action":"start","Package":"p/b"}
{"Action":"output","Package":"p/b","Output":"FAIL\tp/b [build failed]\n","OutputType":"frame"}
{"Action":"fail","Package":"p/b","Elapsed":0,"FailedBuild":"p/b [p/b.test]"}
`
	testPanicStream = `{"Action":"start","Package":"p/c"}
{"Action":"run","Package":"p/c","Test":"TestP"}
{"Action":"output","Package":"p/c","Test":"TestP","Output":"--- FAIL: TestP (0.00s)\n","OutputType":"frame"}
{"Action":"output","Package":"p/c","Test":"TestP","Output":"panic: assignment to entry in nil map [recovered, repanicked]\n"}
{"Action":"output","Package":"p/c","Test":"TestP","Output":"goroutine 7 [running]:\n"}
{"Action":"fail","Package":"p/c","Test":"TestP","Elapsed":0}
{"Action":"output","Package":"p/c","Output":"FAIL\tp/c\t0.004s\n","OutputType":"frame"}
{"Action":"fail","Package":"p/c","Elapsed":0.005}
`
	initPanicStream = `{"Action":"start","Package":"p/e"}
{"Action":"output","Package":"p/e","Output":"panic: boom in init\n"}
{"Action":"output","Package":"p/e","Output":"goroutine 1 [running]:\n"}
{"Action":"output","Package":"p/e","Output":"FAIL\tp/e\t0.004s\n","OutputType":"frame"}
{"Action":"fail","Package":"p/e","Elapsed":0.005}
`
	noTestsStream = `{"Action":"start","Package":"p/a"}
{"Action":"output","Package":"p/a","Output":"testing: warning: no tests to run\n"}
{"Action":"output","Package":"p/a","Output":"ok  \tp/a\t0.002s [no tests to run]\n"}
{"Action":"pass","Package":"p/a","Elapsed":0.003}
`
	skipStream = `{"Action":"run","Package":"p/f","Test":"TestNeedsFFmpeg"}
{"Action":"skip","Package":"p/f","Test":"TestNeedsFFmpeg","Elapsed":0}
{"Action":"pass","Package":"p/f","Elapsed":0.003}
`
	// Two iterations of -count=2: the first fails, the second passes.
	flakyStream = `{"Action":"run","Package":"p/a","Test":"TestFade"}
{"Action":"output","Package":"p/a","Test":"TestFade","Output":"    a_test.go:9: red 255\n","OutputType":"error"}
{"Action":"fail","Package":"p/a","Test":"TestFade","Elapsed":0}
{"Action":"run","Package":"p/a","Test":"TestFade"}
{"Action":"pass","Package":"p/a","Test":"TestFade","Elapsed":0}
{"Action":"fail","Package":"p/a","Elapsed":0.003}
`
	passStream = `{"Action":"run","Package":"p/a","Test":"TestFade"}
{"Action":"pass","Package":"p/a","Test":"TestFade","Elapsed":0}
{"Action":"pass","Package":"p/a","Elapsed":0.003}
`
)

func parsed(exit int, stream string) Run {
	r := Run{Exit: exit}
	parseGoTest(&r, stream)
	return r
}

func TestRedVerdict(t *testing.T) {
	for _, c := range []struct {
		name, stream string
		exit         int
		verdict      string
	}{
		{"assertion", assertionStream, 1, ""},
		{"build failure", buildFailStream, 1, verdictRedNotAssertion},
		{"panic in a test", testPanicStream, 1, verdictRedNotAssertion},
		{"panic in init", initPanicStream, 1, verdictRedNotAssertion},
		{"no tests", noTestsStream, 0, verdictRedNotAssertion},
		{"all skipped", skipStream, 0, verdictRedNotAssertion},
		{"passing", passStream, 0, verdictNotRed},
	} {
		t.Run(c.name, func(t *testing.T) {
			if verdict, reason := redVerdict(parsed(c.exit, c.stream)); verdict != c.verdict {
				t.Errorf("verdict %q (%s), want %q", verdict, reason, c.verdict)
			}
		})
	}
}

func TestParseKeepsTheFailureLines(t *testing.T) {
	r := parsed(1, assertionStream)
	if got := r.failedTests(); !slices.Equal(got, []string{"TestBad"}) {
		t.Errorf("failed tests %v, want [TestBad]: subtests count under their parent", got)
	}
	if !slices.Contains(r.Tail, "    a_test.go:4: want 1, got 2") {
		t.Errorf("tail %q lacks the assertion message", r.Tail)
	}
	if r = parsed(1, buildFailStream); !r.BuildFailed || !slices.Contains(r.Tail, "b/b_test.go:3:28: undefined: undefined") {
		t.Errorf("build failure: %+v", r)
	}
	if r = parsed(1, initPanicStream); !r.PackageFailed {
		t.Errorf("a panic in init is not a package failure: %+v", r)
	}
}

func TestGreenVerdict(t *testing.T) {
	for _, c := range []struct {
		name    string
		runs    []Run
		verdict string
	}{
		{"all pass", []Run{parsed(0, passStream), parsed(0, passStream)}, verdictProved},
		{"one test never passes", []Run{parsed(0, passStream), parsed(1, assertionStream)}, verdictNotGreen},
		{"fails some iterations", []Run{parsed(1, flakyStream), parsed(0, passStream)}, verdictFlaky},
		{"fails every run", []Run{parsed(1, assertionStream), parsed(1, assertionStream)}, verdictNotGreen},
		{"build failure", []Run{parsed(1, buildFailStream)}, verdictNotGreen},
		{"no tests", []Run{parsed(0, noTestsStream)}, verdictNotGreen},
	} {
		t.Run(c.name, func(t *testing.T) {
			if verdict, reason := greenVerdict(c.runs); verdict != c.verdict {
				t.Errorf("verdict %q (%s), want %q", verdict, reason, c.verdict)
			}
		})
	}
}

func TestFinalVerdict(t *testing.T) {
	for _, c := range []struct{ red, green, want string }{
		{"", verdictProved, verdictProved},
		{"", verdictNotGreen, verdictNotGreen},
		{verdictNotRed, verdictProved, verdictNotRed},
		{verdictNotRed, verdictFlaky, verdictFlaky},
		{"", verdictFlaky, verdictFlaky},
		{verdictRedNotAssertion, verdictNotGreen, verdictRedNotAssertion},
		{"", verdictError, verdictError},
	} {
		if got, _ := finalVerdict(c.red, "red", c.green, "green"); got != c.want {
			t.Errorf("red %q, green %q: %q, want %q", c.red, c.green, got, c.want)
		}
	}
}

func TestParseCommand(t *testing.T) {
	var r Run
	parseCommand(&r, 1, "not ok 1 - queue\n  AssertionError [ERR_ASSERTION]: 2 !== 3\n")
	if verdict, _ := redVerdict(r); verdict != "" {
		t.Errorf("a Node assertion failure is %q, not red by assertion: %+v", verdict, r)
	}
	r = Run{}
	parseCommand(&r, 1, "SyntaxError: Unexpected token\n")
	if verdict, _ := redVerdict(r); verdict != verdictRedNotAssertion {
		t.Errorf("a syntax error is %q, want %s", verdict, verdictRedNotAssertion)
	}
}

func TestPlanFiles(t *testing.T) {
	changes, err := parseNameStatus("M\x00internal/media/resume.go\x00M\x00internal/media/resume_test.go\x00" +
		"A\x00internal/media/fsync.go\x00D\x00internal/media/old.go\x00A\x00internal/media/testdata/a.mp4\x00" +
		"M\x00go.sum\x00M\x00apps/console/jstest/queue.test.mjs\x00M\x00apps/console/jstest/helper.mjs\x00")
	if err != nil {
		t.Fatal(err)
	}
	fp := planFiles(changes, []string{"apps/console/jstest/helper.mjs"})
	if want := []string{"internal/media/resume.go", "internal/media/old.go"}; !slices.Equal(fp.reverted, want) {
		t.Errorf("reverted %v, want %v", fp.reverted, want)
	}
	if want := []string{"internal/media/fsync.go"}; !slices.Equal(fp.addedKept, want) {
		t.Errorf("added and kept %v, want %v", fp.addedKept, want)
	}
	if want := []string{"go.sum", "apps/console/jstest/helper.mjs"}; !slices.Equal(fp.keptAtHead, want) {
		t.Errorf("kept at HEAD %v, want %v", fp.keptAtHead, want)
	}
}

func TestTestNames(t *testing.T) {
	src := `package x
import "testing"
func TestMain(m *testing.M) {}
func TestA(t *testing.T) {}
func Testing(t *testing.T) {}
func helper() {}
func Test_b(t *testing.T) {}
`
	names, err := testNames("x_test.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"TestA", "Test_b"}; !slices.Equal(names, want) {
		t.Errorf("names %v, want %v", names, want)
	}
	if got := runRegexp(names); got != "^(TestA|Test_b)$" {
		t.Errorf("run regexp %q", got)
	}
}

// tinyRepo builds a module with a buggy Abs at its first commit, and a second
// commit that fixes Abs and adds the given test. It returns the root and the
// base commit.
func tinyRepo(t *testing.T, test string) (string, string) {
	t.Helper()
	for _, tool := range []string{"git", "go"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool, "is not installed")
		}
	}
	root := t.TempDir()
	write := func(name, src string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitDo := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com",
			"-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitDo("init", "-q")
	write("go.mod", "module tiny\n\ngo 1.22\n")
	write("calc/calc.go", "package calc\n\nfunc Abs(x int) int { return x }\n")
	gitDo("add", ".")
	gitDo("commit", "-q", "-m", "base")
	base := gitDo("rev-parse", "HEAD")
	write("calc/calc.go", "package calc\n\nfunc Abs(x int) int {\n\tif x < 0 {\n\t\treturn -x\n\t}\n\treturn x\n}\n")
	write("calc/calc_test.go", test)
	gitDo("add", ".")
	gitDo("commit", "-q", "-m", "fix")
	return root, base
}

func proveTiny(t *testing.T, test string) Proof {
	t.Helper()
	root, base := tinyRepo(t, test)
	p := prove(context.Background(), options{root: root, tests: []string{"calc/calc_test.go"}, base: base, count: 2, timeout: time.Minute})
	out, _ := exec.Command("git", "-C", root, "worktree", "list", "--porcelain").Output()
	if n := strings.Count(string(out), "worktree "); n != 1 {
		t.Errorf("%d worktrees left after the proof, want only the repository's own:\n%s", n, out)
	}
	return p
}

func TestProvesAFixedBug(t *testing.T) {
	t.Parallel()
	p := proveTiny(t, `package calc

import "testing"

func TestAbsOfANegative(t *testing.T) {
	if got := Abs(-2); got != 2 {
		t.Errorf("Abs(-2) = %d, want 2", got)
	}
}
`)
	if p.Verdict != verdictProved || !slices.Equal(p.RevertedFiles, []string{"calc/calc.go"}) || p.Run != "^(TestAbsOfANegative)$" {
		t.Errorf("proof %+v, want proved with calc/calc.go reverted", p)
	}
	if len(p.Green) != 2 || p.Green[0].Tests["TestAbsOfANegative"].Pass != 2 {
		t.Errorf("green runs %+v, want two iterations then one on one CPU", p.Green)
	}
}

func TestATestBesideTheBugIsNotRed(t *testing.T) {
	t.Parallel()
	p := proveTiny(t, `package calc

import "testing"

func TestAbsOfAPositive(t *testing.T) {
	if got := Abs(2); got != 2 {
		t.Errorf("Abs(2) = %d, want 2", got)
	}
}
`)
	if p.Verdict != verdictNotRed {
		t.Errorf("proof %+v, want not-red", p)
	}
}

func TestATestThatFailsOnOneCPUIsFlaky(t *testing.T) {
	t.Parallel()
	p := proveTiny(t, `package calc

import (
	"runtime"
	"testing"
)

func TestAbsOfANegative(t *testing.T) {
	if got := Abs(-2); got != 2 || runtime.GOMAXPROCS(0) == 1 {
		t.Errorf("Abs(-2) = %d on %d CPUs, want 2", got, runtime.GOMAXPROCS(0))
	}
}
`)
	if p.Verdict != verdictFlaky {
		t.Errorf("proof %+v, want flaky", p)
	}
}
