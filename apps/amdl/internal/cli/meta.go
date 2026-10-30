package cli

import (
	"flag"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"amdl/apps/amdl/skill"
	"amdl/internal/capability"
)

// exitCodes lists the statuses a command can end with, for amdl schema.
func exitCodes(c capability.Capability) []int {
	codes := []int{exitOK, exitInput}
	if len(c.Ops) > 0 {
		codes = append(codes, exitUnavailable)
	}
	switch {
	case strings.HasPrefix(c.Command, "jobs ") && c.Command != "jobs list":
		codes = append(codes, exitNotFound, exitConflict)
	case strings.HasPrefix(c.Command, "wrapper "):
		codes = append(codes, exitAuth, exitConflict, exitNotFound)
	}
	if c.Command == "jobs wait" || c.Command == "download" {
		codes = append(codes, exitJobFailed, exitWaitTimeout)
	}
	if c.Safety == capability.Destructive || c.Command == "download" {
		codes = append(codes, exitConfirm)
	}
	sort.Ints(codes)
	return codes
}

func cliCommandsCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		type row struct {
			Command string            `json:"command"`
			Tool    string            `json:"tool,omitempty"`
			Safety  capability.Safety `json:"safety"`
			Summary string            `json:"summary"`
		}
		var rows []row
		for _, c := range commands() {
			rows = append(rows, row{"amdl " + c.Command + argSynopsis(c.Capability), c.Tool, c.Safety, c.Summary})
		}
		return e.out.result(rows)
	}
}

func schemaCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	view := fs.String("view", "summary", "summary (each operation's method, path and parameter names) or full (adds parameter descriptions, the request body and output JSON Schema)")
	return func(e *env, args []string) error {
		if len(args) == 0 {
			return usageErr("usage: amdl schema <command>, such as amdl schema jobs list")
		}
		if *view != "summary" && *view != "full" {
			return usageErr("--view is summary or full, not %q", *view)
		}
		cmds := commands()
		cmd, rest, ok := match(cmds, args)
		if !ok || len(rest) > 0 {
			return &cliError{Code: "usage_error", exit: exitInput, Message: "no command " + strings.Join(args, " "),
				Hint: "amdl cli commands"}
		}
		fs := flag.NewFlagSet(cmd.Command, flag.ContinueOnError)
		cmd.setup(fs)
		type flagInfo struct {
			Flag  string `json:"flag"`
			Usage string `json:"usage"`
		}
		var flags []flagInfo
		fs.VisitAll(func(f *flag.Flag) { flags = append(flags, flagInfo{"--" + f.Name, f.Usage}) })
		out := map[string]any{
			"command": "amdl " + cmd.Command + argSynopsis(cmd.Capability),
			"summary": cmd.Summary, "safety": cmd.Safety, "args": cmd.Args, "flags": flags,
			"exitCodes": exitCodes(cmd.Capability), "examples": cmd.Examples,
		}
		if cmd.Tool != "" {
			out["tool"] = cmd.Tool
		}
		if len(cmd.Ops) > 0 {
			var ops []capability.Operation
			for _, id := range cmd.Ops {
				op := capability.Lookup(id)
				if *view == "summary" {
					op = slimOperation(op)
				}
				ops = append(ops, op)
			}
			out["operations"] = ops
		}
		return e.out.result(out)
	}
}

// slimOperation is op without what makes amdl schema large: the parameters'
// descriptions, the request body schema and the output schema. The flags list
// has the descriptions; --view full has the rest.
func slimOperation(op capability.Operation) capability.Operation {
	op.Body, op.OutputSchema = nil, nil
	op.Params = slices.Clone(op.Params)
	for i := range op.Params {
		op.Params[i].Description = ""
	}
	return op
}

func cliSearchCmd(fs *flag.FlagSet) func(e *env, args []string) error {
	limit := fs.Int("limit", 5, "how many commands to return")
	return func(e *env, args []string) error {
		if len(args) == 0 {
			return usageErr(`usage: amdl cli search "<what you want to do>"`)
		}
		type hit struct {
			Command string `json:"command"`
			Summary string `json:"summary"`
			Example string `json:"example,omitempty"`
		}
		var hits []hit
		for _, r := range rank(commands(), strings.Join(args, " ")) {
			if r.Command == "cli search" {
				continue
			}
			h := hit{Command: "amdl " + r.Command + argSynopsis(r.Capability), Summary: r.Summary}
			if len(r.Examples) > 0 {
				h.Example = r.Examples[0]
			}
			hits = append(hits, h)
		}
		if len(hits) > *limit {
			hits = hits[:*limit]
		}
		if hits == nil {
			hits = []hit{}
		}
		return e.out.result(hits)
	}
}

// synonyms widen a command's words with the ones people use for them.
var synonyms = map[string]string{
	"download": "get fetch save grab rip add queue", "search": "find look query catalog song album artist",
	"jobs": "job download downloads queue task tasks", "list": "show all failed running recent",
	"delete": "remove rm clear", "cancel": "stop abort", "retry": "again rerun restart failed",
	"wait": "until finish done block", "watch": "follow events stream progress live",
	"lookup": "preview info metadata link", "quality": "codec bitrate lossless alac atmos hires",
	"logs": "log error errors debug", "config": "setting settings option options", "wrapper": "login account apple id signin",
	"system": "status server version health disk space", "library": "finished completed downloaded collection",
	"artists": "artist discography albums", "context": "server address remote nas host",
	"wrapper login": "sign signin log authenticate password", "wrapper logout": "signout logoff",
	"wrapper 2fa": "code two step verification sms", "jobs get": "detail details tracks show file files path output location folder where",
}

func searchTerms(c command) []string {
	text := c.Command + " " + c.Summary + " " + strings.Join(c.Args, " ")
	for _, w := range strings.Fields(c.Command) {
		text += " " + synonyms[w]
	}
	text += " " + synonyms[c.Command]
	return tokens(text)
}

func tokens(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len(f) > 1 && !stop[f] {
			out = append(out, strings.TrimSuffix(f, "s"))
		}
	}
	return out
}

var stop = map[string]bool{"the": true, "a": true, "an": true, "of": true, "to": true, "and": true, "or": true,
	"in": true, "on": true, "for": true, "by": true, "with": true, "my": true, "me": true, "it": true, "is": true, "amdl": true}

// bm25 scores one command's words against the query.
func bm25(query, doc []string, df map[string]int, n int, avg float64) float64 {
	const k1, b = 1.2, 0.75
	tf := map[string]int{}
	for _, t := range doc {
		tf[t]++
	}
	score := 0.0
	for _, q := range query {
		f := float64(tf[q])
		if f == 0 {
			continue
		}
		idf := math.Log(1 + (float64(n)-float64(df[q])+0.5)/(float64(df[q])+0.5))
		score += idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(len(doc))/avg))
	}
	return score
}

// rank returns the commands that match a free-text task, best first.
func rank(cmds []command, text string) []command {
	docs := make([][]string, len(cmds))
	df := map[string]int{}
	avg := 0.0
	for i, c := range cmds {
		docs[i] = searchTerms(c)
		seen := map[string]bool{}
		for _, t := range docs[i] {
			if !seen[t] {
				df[t]++
				seen[t] = true
			}
		}
		avg += float64(len(docs[i]))
	}
	avg /= float64(len(cmds))
	query := tokens(text)
	type scored struct {
		c command
		s float64
	}
	var hits []scored
	for i, c := range cmds {
		if s := bm25(query, docs[i], df, len(cmds), avg); s > 0 {
			hits = append(hits, scored{c, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	out := make([]command, len(hits))
	for i, h := range hits {
		out[i] = h.c
	}
	return out
}

// suggest names up to three commands close to an unknown one: first those
// spelled almost the same (jobs lst for jobs list), then the best matches by
// meaning.
func suggest(cmds []command, text string) []string {
	out := []string{}
	add := func(c command) {
		name := "amdl " + c.Command
		if len(out) < 3 && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	type near struct {
		c    command
		cost int
	}
	var nearby []near
	words := strings.Fields(strings.ToLower(text))
	for _, c := range cmds {
		if cost, ok := spellingCost(strings.Fields(c.Command), words); ok {
			nearby = append(nearby, near{c, cost})
		}
	}
	sort.SliceStable(nearby, func(i, j int) bool { return nearby[i].cost < nearby[j].cost })
	for _, n := range nearby {
		add(n.c)
	}
	for _, c := range rank(cmds, text) {
		add(c)
	}
	return out
}

// spellingCost says whether typed is a misspelling of the command words, and
// how far off: each word equal, a prefix of the command's word, or one edit
// away (two for words over four letters).
func spellingCost(command, typed []string) (int, bool) {
	if len(command) != len(typed) || len(typed) == 0 {
		return 0, false
	}
	total := 0
	for i, w := range typed {
		switch d := editDistance(w, command[i]); {
		case d == 0:
		case len(w) >= 2 && strings.HasPrefix(command[i], w):
			total++
		case d <= 1 || d <= 2 && len(w) > 4:
			total += d
		default:
			return 0, false
		}
	}
	return total, true
}

// editDistance is the Levenshtein distance between a and b.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// agentsNote is the AGENTS.md or CLAUDE.md paragraph amdl cli setup prints.
const agentsNote = `## Apple Music downloads

Use the amdl CLI for downloading music from Apple Music. Find a command with
` + "`amdl cli search \"<task>\"`" + ` and read its flags and output with ` + "`amdl schema <command>`" + `.
Results are JSON on stdout; errors are JSON on stderr with an exit status per kind
of failure (2 change the input, 5 retry later, 10 needs the user's confirmation).
Destructive commands need --yes: ask the user first.`

// mcpServer is the --server a client's amdl mcp is given, so the snippet does
// not depend on what this shell's environment holds: the one named, else the
// URL this run resolves to, else the default address with a hint that nothing
// answers there. This machine's own backend gets none: mcp finds it itself,
// and its port can change between starts.
func (e *env) mcpServer() (server, hint string) {
	if e.server != "" {
		return e.server, ""
	}
	t, err := e.resolve()
	switch {
	case err != nil:
		return defaultURL, "no amdl server answers yet; start one (amdl service install), or rerun with --server <url> and use that snippet"
	case t.Embedded:
		return "embedded", ""
	case t.From == "desktop":
		return "", ""
	}
	return t.URL, ""
}

func cliSetupCmd(*flag.FlagSet) func(e *env, args []string) error {
	return func(e *env, args []string) error {
		if len(args) > 0 {
			return usageErr("usage: amdl cli setup [--server name|url]")
		}
		exe, err := os.Executable()
		if err != nil {
			exe = "amdl"
		}
		mcpArgs := []string{"mcp"}
		server, hint := e.mcpServer()
		if server != "" {
			mcpArgs = append(mcpArgs, "--server", server)
		}
		quoted := make([]string, len(mcpArgs))
		for i, a := range mcpArgs {
			quoted[i] = strconv.Quote(a)
		}
		out := map[string]any{
			"claude_code": "claude mcp add amdl -- " + exe + " " + strings.Join(mcpArgs, " "),
			"codex_toml":  "[mcp_servers.amdl]\ncommand = " + strconv.Quote(exe) + "\nargs = [" + strings.Join(quoted, ", ") + "]",
			"mcp_json":    map[string]any{"mcpServers": map[string]any{"amdl": map[string]any{"command": exe, "args": mcpArgs}}},
			// The agent skill, for ~/.claude/skills/amdl/SKILL.md or a project's
			// .claude/skills: amdl cli setup --fields skill | jq -r .skill
			"skill":     skill.Text,
			"agents_md": agentsNote,
		}
		if hint != "" {
			out["hint"] = hint
		}
		return e.out.result(out)
	}
}
