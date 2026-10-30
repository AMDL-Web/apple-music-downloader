// Package cli is the amdl command line: one command per capability of
// internal/capability, results as JSON on stdout, errors as the API's error
// object on stderr, and an exit status per class of error. Commands that only
// pass a request through are built from internal/api/openapi.yaml; the rest
// are written out in commands.go.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"amdl/internal/capability"
	"amdl/internal/client"
	"amdl/internal/local"
)

// IO is where a run reads and writes; Main uses the process's own.
type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// OutTTY, ErrTTY and InTTY say whether each stream is a terminal.
	OutTTY, ErrTTY, InTTY bool
	Getenv                func(string) string
}

// env is one command's run: its IO, the global flags and the server.
type env struct {
	IO
	ctx     context.Context
	out     *printer
	server  string
	wrapper string
	target  string
	// running is the backend this run started for --server embedded.
	running *local.Running
	quiet   bool
	yes     bool
	dry     bool
	cmd     capability.Capability
	c       *client.Client
}

// command is one CLI command: setup declares its flags and returns the runner,
// which gets the positional arguments.
type command struct {
	capability.Capability
	setup func(fs *flag.FlagSet) func(e *env, args []string) error
	// dryRun marks a command that honours --dry-run.
	dryRun bool
	// noServer marks a command that never talks to a server.
	noServer bool
}

// Run runs one amdl invocation and returns its exit status.
func Run(ctx context.Context, args []string, io IO) int {
	if io.Getenv == nil {
		io.Getenv = os.Getenv
	}
	cmds := commands()
	if len(args) == 0 && io.InTTY && io.OutTTY {
		args = []string{"tui"}
	}
	if len(args) == 0 {
		return rootHelp(io, cmds)
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		return rootHelp(io, cmds)
	}
	cmd, rest, ok := match(cmds, args)
	if !ok {
		return fail(io, &cliError{Code: "usage_error", exit: exitInput,
			Message: fmt.Sprintf("unknown command %q", strings.Join(leadingWords(args), " ")),
			Details: mustJSON(map[string]any{"did_you_mean": suggest(cmds, strings.Join(leadingWords(args), " "))}),
			Hint:    `amdl cli search "<what you want to do>"`})
	}
	e := &env{IO: io, ctx: ctx, cmd: cmd.Capability}
	fs := flag.NewFlagSet("amdl "+cmd.Command, flag.ContinueOnError)
	fs.SetOutput(io.Stderr)
	var fields string
	fs.StringVar(&e.server, "server", "", "server URL, saved context name or embedded (default: $AMDL_URL, the current context, this machine's backend, then http://127.0.0.1:18080)")
	fs.StringVar(&e.wrapper, "wrapper", "", "the wrapper address a local backend needs on its first start (--server embedded, serve --local, service install)")
	fs.StringVar(&fields, "fields", "", "comma-separated keys to keep in each result")
	fs.BoolVar(&e.quiet, "quiet", false, "write nothing to stderr but errors")
	if cmd.Safety == capability.Destructive || cmd.Command == "download" {
		fs.BoolVar(&e.yes, "yes", false, "confirm a destructive action without asking")
	}
	if cmd.dryRun {
		fs.BoolVar(&e.dry, "dry-run", false, "print the request instead of sending it")
	}
	runner := cmd.setup(fs)
	help := false
	fs.BoolVar(&help, "help", false, "show this help")
	fs.Usage = func() {}
	pos, err := parseInterspersed(fs, rest)
	if err != nil {
		return fail(io, usageErr("%v", err))
	}
	if help {
		commandHelp(io.Stdout, cmd, fs)
		return exitOK
	}
	e.out = &printer{w: io.Stdout, pretty: io.OutTTY, color: io.OutTTY && io.Getenv("NO_COLOR") == ""}
	if fields != "" {
		e.out.fields = splitList(fields)
	}
	if !cmd.noServer {
		c, err := e.connect()
		if err != nil {
			return fail(io, err)
		}
		e.c = c
	}
	err = runner(e, pos)
	if e.running != nil {
		e.note("stopping the embedded backend; unfinished jobs resume on its next start")
		if serr := e.running.Stop(); serr != nil && err == nil {
			err = serr
		}
	}
	if err != nil {
		if ctx.Err() != nil && errors.Is(err, context.Canceled) {
			return exitInterrupted
		}
		return fail(io, err)
	}
	return exitOK
}

// match finds the longest command path at the start of args.
func match(cmds []command, args []string) (command, []string, bool) {
	best, bestLen := command{}, 0
	for _, c := range cmds {
		words := strings.Fields(c.Command)
		if len(words) > len(args) || len(words) <= bestLen {
			continue
		}
		ok := true
		for i, w := range words {
			if args[i] != w {
				ok = false
				break
			}
		}
		if ok {
			best, bestLen = c, len(words)
		}
	}
	if bestLen == 0 {
		return command{}, nil, false
	}
	return best, args[bestLen:], true
}

func leadingWords(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") || len(out) == 3 {
			break
		}
		out = append(out, a)
	}
	return out
}

// parseInterspersed parses flags wherever they appear among the positional
// arguments; "--" ends the flags.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// fail writes err to stderr, as JSON when stderr is not a terminal, and
// returns its exit status.
func fail(io IO, err error) int {
	ce := asCLIError(err)
	if io.ErrTTY {
		fmt.Fprintf(io.Stderr, "error: %s (%s)\n", ce.Message, ce.Code)
		if len(ce.Details) > 0 && string(ce.Details) != "null" {
			fmt.Fprintf(io.Stderr, "details: %s\n", ce.Details)
		}
		if ce.Hint != "" {
			fmt.Fprintf(io.Stderr, "hint: %s\n", ce.Hint)
		}
	} else {
		raw, _ := marshal(map[string]any{"error": ce}) // a struct of strings and raw JSON always marshals
		fmt.Fprintf(io.Stderr, "%s\n", raw)
	}
	return ce.exit
}

// note writes a line for people to stderr, only when it is a terminal.
func (e *env) note(format string, a ...any) {
	if e.ErrTTY && !e.quiet {
		fmt.Fprintf(e.Stderr, format+"\n", a...)
	}
}

// confirm asks before a destructive action: a terminal answers y or n, and
// anywhere else --yes is required.
func (e *env) confirm(what string) error {
	if e.yes || e.dry {
		return nil
	}
	hint := "amdl " + strings.Join(os.Args[1:], " ") + " --yes"
	if !e.InTTY || !e.ErrTTY {
		return &cliError{Code: "confirmation_required", exit: exitConfirm,
			Message: what + "; nothing was changed", Hint: hint}
	}
	fmt.Fprintf(e.Stderr, "%s. Continue? [y/N] ", what)
	line, _ := bufio.NewReader(e.Stdin).ReadString('\n') // EOF reads as no
	if a := strings.ToLower(strings.TrimSpace(line)); a == "y" || a == "yes" {
		return nil
	}
	return &cliError{Code: "confirmation_required", exit: exitConfirm, Message: "not confirmed; nothing was changed"}
}

func splitList(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' }) {
		out = append(out, f)
	}
	return out
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

// rootHelp is what amdl alone prints: JSON for an agent, text in a terminal.
func rootHelp(io IO, cmds []command) int {
	type entry struct {
		Command string `json:"command"`
		Summary string `json:"summary"`
	}
	var list []entry
	for _, c := range cmds {
		list = append(list, entry{"amdl " + c.Command + argSynopsis(c.Capability), c.Summary})
	}
	if !io.OutTTY {
		raw, _ := marshal(map[string]any{ // strings only
			"name": "amdl", "version": version(),
			"hint":     `Find the command for a task with: amdl cli search "<task>". Every result is JSON on stdout; errors are JSON on stderr with a non-zero exit status.`,
			"commands": list,
		})
		fmt.Fprintf(io.Stdout, "%s\n", raw)
		return exitOK
	}
	fmt.Fprintf(io.Stdout, "amdl %s — download music from Apple Music through an amdl server.\n\n", version())
	fmt.Fprintln(io.Stdout, `Agents: run amdl cli search "<task>" first; results are JSON on stdout.`)
	fmt.Fprintln(io.Stdout)
	width := 0
	for _, e := range list {
		width = max(width, len(e.Command))
	}
	for _, e := range list {
		fmt.Fprintf(io.Stdout, "  %-*s  %s\n", width, e.Command, e.Summary)
	}
	fmt.Fprintln(io.Stdout, "\nEvery command takes --help, --server, --fields and --quiet.")
	return exitOK
}

func argSynopsis(c capability.Capability) string {
	var b strings.Builder
	for _, a := range c.Args {
		switch a {
		case "urls":
			b.WriteString(" <url>…")
		case "keys":
			b.WriteString(" [key…]")
		case "changes":
			b.WriteString(" <key=value>…")
		case "job_id":
			if c.Command == "jobs watch" {
				b.WriteString(" [<job_id>]")
				continue
			}
			b.WriteString(" <job_id>")
		default:
			b.WriteString(" <" + a + ">")
		}
	}
	return b.String()
}

func commandHelp(w io.Writer, c command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "amdl %s%s\n\n%s.\n", c.Command, argSynopsis(c.Capability), c.Summary)
	fmt.Fprintf(w, "Safety: %s.", c.Safety)
	if c.Tool != "" {
		fmt.Fprintf(w, " MCP tool: %s.", c.Tool)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "\nFlags:")
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	sort.Strings(names)
	for _, n := range names {
		f := fs.Lookup(n)
		fmt.Fprintf(w, "  --%-22s %s\n", n, f.Usage)
	}
	if len(c.Examples) > 0 {
		fmt.Fprintln(w, "\nExamples:")
		for _, ex := range c.Examples {
			fmt.Fprintf(w, "  %s\n", ex)
		}
	}
}
