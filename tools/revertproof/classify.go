package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const (
	verdictProved          = "proved"
	verdictNotRed          = "not-red"
	verdictRedNotAssertion = "red-not-assertion"
	verdictNotGreen        = "not-green"
	verdictFlaky           = "flaky"
	verdictError           = "error"
	tailLines              = 15
	commandTest            = "command"
)

// assertionError marks a failed assertion in a -cmd command's output: Node's
// assert module, or a Go test failure.
var assertionError = regexp.MustCompile(`AssertionError|ERR_ASSERTION|--- FAIL: `)

// Tally counts one top-level test's results across the iterations of a run.
// A panicking test counts in Panic, not Fail: a panic is not an assertion.
type Tally struct {
	Pass  int `json:"pass,omitempty"`
	Fail  int `json:"fail,omitempty"`
	Panic int `json:"panic,omitempty"`
	Skip  int `json:"skip,omitempty"`
}

// Run is one test invocation.
type Run struct {
	Args  []string          `json:"args"`
	Exit  int               `json:"exit"`
	Tests map[string]*Tally `json:"tests,omitempty"`
	// BuildFailed: a test binary did not compile.
	BuildFailed bool `json:"buildFailed,omitempty"`
	// PackageFailed: a package failed outside any test, from a panic in init
	// or TestMain's exit code.
	PackageFailed bool `json:"packageFailed,omitempty"`
	// TimedOut: the run passed -timeout, go test's own or, for -cmd, the
	// tool's.
	TimedOut bool     `json:"timedOut,omitempty"`
	Tail     []string `json:"tail,omitempty"`
}

type event struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Output     string
	OutputType string
}

// parseGoTest reads a `go test -json` stream into r.
func parseGoTest(r *Run, stream string) {
	if r.Tests == nil {
		r.Tests = map[string]*Tally{}
	}
	panicking := map[string]bool{}
	testFailed := map[string]bool{}
	sc := bufio.NewScanner(strings.NewReader(stream))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Text()
		var e event
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &e) != nil {
			r.addTail(line)
			continue
		}
		top, _, sub := strings.Cut(e.Test, "/")
		switch e.Action {
		case "build-output":
			r.addTail(e.Output)
		case "build-fail":
			r.BuildFailed = true
		case "output":
			out := e.Output
			if strings.HasPrefix(out, "panic: test timed out after ") {
				r.TimedOut = true
			}
			if strings.HasPrefix(out, "panic: ") || strings.HasPrefix(out, "fatal error: ") {
				panicking[e.Package+" "+top] = true
				r.addTail(out)
			} else if e.OutputType == "error" || (e.Test == "" && e.OutputType == "" && strings.TrimSpace(out) != "") {
				r.addTail(out)
			} else if strings.HasPrefix(strings.TrimSpace(out), "--- FAIL") {
				r.addTail(out)
			}
		case "run":
			if e.Test != "" && !sub {
				delete(panicking, e.Package+" "+top)
			}
		case "pass", "fail", "skip":
			if e.Test == "" {
				if e.Action == "fail" && !testFailed[e.Package] && !r.BuildFailed {
					r.PackageFailed = true
				}
				continue
			}
			if sub {
				continue
			}
			t := r.tally(top)
			switch {
			case e.Action == "pass":
				t.Pass++
			case e.Action == "skip":
				t.Skip++
			case panicking[e.Package+" "+top]:
				t.Panic++
				testFailed[e.Package] = true
			default:
				t.Fail++
				testFailed[e.Package] = true
			}
		}
	}
}

// parseCommand records one run of a -cmd command as a test named "command".
func parseCommand(r *Run, exit int, output string) {
	t := r.tally(commandTest)
	switch {
	case exit == 0:
		t.Pass++
	case assertionError.MatchString(output):
		t.Fail++
	default:
		r.PackageFailed = true
	}
	if exit != 0 {
		for _, l := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
			r.addTail(l)
		}
	}
}

func (r *Run) tally(name string) *Tally {
	if r.Tests == nil {
		r.Tests = map[string]*Tally{}
	}
	if r.Tests[name] == nil {
		r.Tests[name] = &Tally{}
	}
	return r.Tests[name]
}

func (r *Run) addTail(line string) {
	line = strings.TrimRight(line, "\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	r.Tail = append(r.Tail, line)
	if len(r.Tail) > tailLines {
		r.Tail = r.Tail[len(r.Tail)-tailLines:]
	}
}

func (r *Run) total() Tally {
	var sum Tally
	for _, t := range r.Tests {
		sum.Pass += t.Pass
		sum.Fail += t.Fail
		sum.Panic += t.Panic
		sum.Skip += t.Skip
	}
	return sum
}

func (r *Run) failedTests() []string {
	var names []string
	for n, t := range r.Tests {
		if t.Fail > 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

// redVerdict judges the run with the fix reverted. An empty verdict means it
// went red on an assertion.
func redVerdict(r Run) (verdict, reason string) {
	sum := r.total()
	switch {
	case r.TimedOut:
		return verdictError, "the run without the fix passed -timeout, which shows no assertion either way"
	case sum.Fail > 0:
		return "", ""
	case r.BuildFailed:
		return verdictRedNotAssertion, "the tests do not build without the fix: they use something it declares"
	case sum.Panic > 0:
		return verdictRedNotAssertion, "a test panicked without the fix, rather than failing an assertion"
	case r.PackageFailed || r.Exit != 0:
		return verdictRedNotAssertion, fmt.Sprintf("exit %d without a failed test: a panic in init or TestMain", r.Exit)
	case sum.Pass == 0 && sum.Skip > 0:
		return verdictRedNotAssertion, "every matched test skipped"
	case sum.Pass == 0:
		return verdictRedNotAssertion, "no test matched"
	}
	return verdictNotRed, "the tests pass without the fix"
}

// finalVerdict combines the red and green verdicts. Flakiness comes first:
// while a test fails by chance, neither its red nor its green run shows
// anything.
func finalVerdict(redV, redReason, greenV, greenReason string) (verdict, reason string) {
	switch {
	case redV == verdictError:
		return redV, redReason
	case greenV == verdictError:
		return greenV, greenReason
	case greenV == verdictFlaky && redV != "":
		return verdictFlaky, greenReason + "; the red run: " + redReason
	case greenV == verdictFlaky:
		return verdictFlaky, greenReason
	case redV != "":
		return redV, redReason
	}
	return greenV, greenReason
}

// greenVerdict judges the runs at HEAD together. A test that failed in some
// runs and passed in others is flaky; one that never passed is not green.
func greenVerdict(runs []Run) (verdict, reason string) {
	merged := map[string]*Tally{}
	var flaky, broken []string
	for _, r := range runs {
		switch {
		case r.TimedOut:
			return verdictError, fmt.Sprintf("the run %s timed out", strings.Join(r.Args, " "))
		case r.BuildFailed:
			return verdictNotGreen, "the tests do not build at HEAD"
		case r.PackageFailed:
			broken = append(broken, fmt.Sprintf("the package failed outside a test in %s", strings.Join(r.Args, " ")))
		}
		for n, t := range r.Tests {
			if merged[n] == nil {
				merged[n] = &Tally{}
			}
			m := merged[n]
			m.Pass, m.Fail, m.Panic, m.Skip = m.Pass+t.Pass, m.Fail+t.Fail, m.Panic+t.Panic, m.Skip+t.Skip
		}
	}
	names := make([]string, 0, len(merged))
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)
	passed := 0
	for _, n := range names {
		t := merged[n]
		passed += t.Pass
		bad := t.Fail + t.Panic
		switch {
		case bad == 0:
		case t.Pass == 0:
			broken = append(broken, fmt.Sprintf("%s failed all %d runs", n, bad))
		default:
			flaky = append(flaky, fmt.Sprintf("%s failed %d of %d runs", n, bad, bad+t.Pass))
		}
	}
	switch {
	case len(broken) > 0:
		return verdictNotGreen, strings.Join(append(broken, flaky...), "; ")
	case len(flaky) > 0:
		return verdictFlaky, strings.Join(flaky, "; ")
	case passed == 0:
		return verdictNotGreen, "no test passed at HEAD"
	}
	return verdictProved, ""
}
