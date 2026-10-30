package repocheck

// The checks below scan every Markdown file git would show (tracked, plus
// untracked files that are not ignored), skipping .github/release-notes/
// (history by nature) and nested worktrees under .claude/worktrees/.
//
//   - Relative links and #anchors resolve (GitHub heading slug rules).
//   - Backticked tokens that start with a top-level repo entry (internal/,
//     docs/, Makefile, AGENTS.md, ...) name a path that exists on disk or is
//     git-ignored, and a "path#Symbol" pointer names something that is
//     still there: a Go declaration (file or package directory), a YAML key,
//     a Markdown heading, or for other files a whole word in the text.
//   - Every AGENTS.md has a sibling CLAUDE.md that is exactly "@AGENTS.md\n";
//     the root AGENTS.md stays within rootAgentsMaxBytes, nested ones within
//     nestedAgentsMaxBytes.
//   - Every docs/*.md page except docs/README.md declares "> Type: <type>"
//     as the first line after its H1, is listed in docs/README.md with the
//     same type, and a record page carries a "Verified at <sha>" stamp.

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"unicode"
)

const (
	rootAgentsMaxBytes   = 12000
	nestedAgentsMaxBytes = 5000
	claudeImport         = "@AGENTS.md\n"
)

var docTypes = map[string]bool{"reference": true, "runbook": true, "decision": true, "record": true}

// skippedMarkdownPrefixes are scanned by nothing: release notes record what
// was true at release time, and nested worktrees are other checkouts.
var skippedMarkdownPrefixes = []string{".github/release-notes/", ".claude/worktrees/"}

type repo struct {
	root  string
	files map[string]bool // existing files git lists, slash-separated
	dirs  map[string]bool // every parent directory of those files
	top   map[string]bool // first path segment of every listed file
	md    []string
	text  map[string]string
	mu    sync.Mutex
}

var (
	loadOnce   sync.Once
	loadedRepo *repo
	loadErr    error
)

func loadRepo(t *testing.T) *repo {
	t.Helper()
	loadOnce.Do(func() { loadedRepo, loadErr = newRepo() })
	if loadErr != nil {
		t.Fatalf("repocheck: %v", loadErr)
	}
	return loadedRepo
}

func newRepo() (*repo, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			return nil, fmt.Errorf("no go.mod above %s", wd)
		}
		root = parent
	}
	out, err := runGit(root, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("list repository files (these checks need git and a checkout with .git): %w", err)
	}
	r := &repo{root: root, files: map[string]bool{}, dirs: map[string]bool{}, top: map[string]bool{}, text: map[string]string{}}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" || r.files[rel] {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			continue // deleted in the working tree but still in the index
		}
		r.files[rel] = true
		first, _, _ := strings.Cut(rel, "/")
		r.top[first] = true
		for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
			r.dirs[dir] = true
		}
		if strings.HasSuffix(rel, ".md") && !hasAnyPrefix(rel, skippedMarkdownPrefixes) {
			r.md = append(r.md, rel)
		}
	}
	sort.Strings(r.md)
	r.touchInputs()
	return r, nil
}

// touchInputs reads every listed directory and .gitignore file. The file
// list comes from a git subprocess, which the go test cache cannot see; the
// cache does track directories and files a test opens itself, so this makes
// a new, renamed or newly ignored Markdown file invalidate a cached pass.
func (r *repo) touchInputs() {
	_, _ = os.ReadDir(r.root)
	for dir := range r.dirs {
		_, _ = os.ReadDir(filepath.Join(r.root, filepath.FromSlash(dir)))
	}
	for rel := range r.files {
		if path.Base(rel) == ".gitignore" {
			_, _ = os.ReadFile(filepath.Join(r.root, filepath.FromSlash(rel)))
		}
	}
}

func runGit(root string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func (r *repo) read(rel string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.text[rel]; ok {
		return s, nil
	}
	raw, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	r.text[rel] = string(raw)
	return r.text[rel], nil
}

func (r *repo) listed(rel string) bool {
	return r.files[rel] || r.dirs[rel]
}

func (r *repo) onDisk(rel string) bool {
	_, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(rel)))
	return err == nil
}

// ignored reports which of paths git would ignore. Exit status 1 from
// check-ignore means "none of them"; any other failure is a real error.
func (r *repo) ignored(paths []string) (map[string]bool, error) {
	hits := map[string]bool{}
	if len(paths) == 0 {
		return hits, nil
	}
	out, err := runGit(r.root, []byte(strings.Join(paths, "\x00")+"\x00"), "check-ignore", "--no-index", "-z", "--stdin")
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return hits, nil
		}
		return nil, err
	}
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			hits[p] = true
		}
	}
	return hits, nil
}

// ---- Markdown scanning ----------------------------------------------------

type mdLine struct {
	n       int
	text    string
	inFence bool
}

var fenceRE = regexp.MustCompile("^\\s*(`{3,}|~{3,})")

func scanLines(text string) []mdLine {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]mdLine, 0, len(lines))
	var fence string
	for i, line := range lines {
		m := fenceRE.FindStringSubmatch(line)
		if fence != "" {
			out = append(out, mdLine{i + 1, line, true})
			if m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) && strings.TrimSpace(line) == m[1] {
				fence = ""
			}
			continue
		}
		if m != nil {
			fence = m[1]
			out = append(out, mdLine{i + 1, line, true})
			continue
		}
		out = append(out, mdLine{i + 1, line, false})
	}
	return out
}

// splitInlineCode returns the code spans of a line and the rest of the line
// with each span blanked out, so links are not looked for inside code.
func splitInlineCode(line string) (spans []string, rest string) {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		run := 0
		for i+run < len(line) && line[i+run] == '`' {
			run++
		}
		closeAt := -1
		for j := i + run; j < len(line); j++ {
			if line[j] != '`' {
				continue
			}
			n := 0
			for j+n < len(line) && line[j+n] == '`' {
				n++
			}
			if n == run {
				closeAt = j
				break
			}
			j += n - 1
		}
		if closeAt == -1 {
			b.WriteString(strings.Repeat("`", run))
			i += run
			continue
		}
		spans = append(spans, strings.TrimSpace(line[i+run:closeAt]))
		b.WriteString(strings.Repeat(" ", closeAt+run-i))
		i = closeAt + run
	}
	return spans, b.String()
}

var (
	headingRE    = regexp.MustCompile(`^ {0,3}#{1,6}\s+(.*?)\s*#*\s*$`)
	mdLinkTextRE = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	htmlTagRE    = regexp.MustCompile(`<[^>]*>`)
	htmlAnchorRE = regexp.MustCompile(`\b(?:id|name)="([^"]+)"`)
)

// githubSlug mirrors the anchor GitHub generates for a heading: lowercase,
// drop everything but letters, marks, numbers, connector punctuation,
// hyphens and spaces, then turn spaces into hyphens.
func githubSlug(heading string) string {
	h := mdLinkTextRE.ReplaceAllString(heading, "$1")
	h = htmlTagRE.ReplaceAllString(h, "")
	h = strings.ToLower(strings.TrimSpace(h))
	var b strings.Builder
	for _, c := range h {
		switch {
		case c == ' ':
			b.WriteByte('-')
		case c == '-' || unicode.IsLetter(c) || unicode.IsMark(c) || unicode.IsNumber(c) || unicode.In(c, unicode.Pc):
			b.WriteRune(c)
		}
	}
	return b.String()
}

func collectAnchors(text string) map[string]bool {
	anchors := map[string]bool{}
	seen := map[string]int{}
	for _, line := range scanLines(text) {
		if line.inFence {
			continue
		}
		if m := headingRE.FindStringSubmatch(line.text); m != nil {
			slug := githubSlug(m[1])
			if n := seen[slug]; n > 0 {
				anchors[fmt.Sprintf("%s-%d", slug, n)] = true
			} else {
				anchors[slug] = true
			}
			seen[slug]++
		}
		for _, m := range htmlAnchorRE.FindAllStringSubmatch(line.text, -1) {
			anchors[strings.ToLower(m[1])] = true
		}
	}
	return anchors
}

func hasAnchor(anchors map[string]bool, fragment string) bool {
	if decoded, err := url.PathUnescape(fragment); err == nil {
		fragment = decoded
	}
	return anchors[strings.ToLower(fragment)]
}

var (
	inlineLinkRE = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	refDefRE     = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(?:\s+"[^"]*")?\s*$`)
	htmlRefRE    = regexp.MustCompile(`\b(?:src|href)\s*=\s*"([^"]+)"`)
	schemeRE     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

func linkTargets(rest string) []string {
	var targets []string
	for _, m := range inlineLinkRE.FindAllStringSubmatch(rest, -1) {
		targets = append(targets, m[1])
	}
	if m := refDefRE.FindStringSubmatch(rest); m != nil {
		targets = append(targets, m[1])
	}
	for _, m := range htmlRefRE.FindAllStringSubmatch(rest, -1) {
		targets = append(targets, m[1])
	}
	return targets
}

// resolveLink turns a link target into a repo-relative path. A leading "/"
// is repo-root relative (as on GitHub); anything else is relative to the
// Markdown file. ok is false when the target leaves the repository.
func resolveLink(mdFile, target string) (rel string, ok bool) {
	if decoded, err := url.PathUnescape(target); err == nil {
		target = decoded
	}
	var joined string
	if strings.HasPrefix(target, "/") {
		joined = path.Clean(strings.TrimLeft(target, "/"))
	} else {
		joined = path.Join(path.Dir(mdFile), target)
	}
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// ---- path#Symbol pointers ------------------------------------------------

var (
	tokenRejectRE = regexp.MustCompile(`[\s<>*{}|()=,;$"'\\^!?\[\]@]`)
	symbolRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)
	lineNumberRE  = regexp.MustCompile(`^L?\d+(?:-L?\d+)?$`)
	lineSuffixRE  = regexp.MustCompile(`:\d+(?:-\d+)?$`)
	goFileRE      = regexp.MustCompile(`\.go$`)
)

type pathToken struct {
	ref    string
	symbol string
}

// parsePathToken decides whether a backticked span is a repo path. Only
// spans whose first segment is an existing top-level entry count, so
// commands, URLs, placeholders and globs are left alone.
func parsePathToken(span string, top map[string]bool) (pathToken, bool) {
	tok := strings.TrimSpace(span)
	if tok == "" || tokenRejectRE.MatchString(tok) || strings.Contains(tok, "...") || strings.Contains(tok, "://") {
		return pathToken{}, false
	}
	if strings.HasPrefix(tok, "/") || strings.HasPrefix(tok, "~") || strings.HasPrefix(tok, "-") {
		return pathToken{}, false
	}
	tok = strings.TrimPrefix(tok, "./")
	ref, symbol, _ := strings.Cut(tok, "#")
	if ref == "" {
		return pathToken{}, false
	}
	first, _, _ := strings.Cut(ref, "/")
	if !top[first] {
		return pathToken{}, false
	}
	// A trailing slash stays: git check-ignore needs it to match a
	// directory-only pattern such as "keys/" for a path that does not exist.
	return pathToken{ref: ref, symbol: symbol}, true
}

// goDecls lists the top-level names declared in the given Go files: funcs,
// methods (as both "Method" and "Recv.Method"), types, vars and consts.
func goDecls(root string, files []string) (map[string]bool, error) {
	names := map[string]bool{}
	fset := token.NewFileSet()
	for _, rel := range files {
		f, err := parser.ParseFile(fset, filepath.Join(root, filepath.FromSlash(rel)), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				names[d.Name.Name] = true
				if d.Recv != nil && len(d.Recv.List) == 1 {
					if recv := receiverName(d.Recv.List[0].Type); recv != "" {
						names[recv+"."+d.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						names[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							names[n.Name] = true
						}
					}
				}
			}
		}
	}
	return names, nil
}

func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.IndexExpr:
		return receiverName(e.X)
	case *ast.IndexListExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// yamlHasKeys reports whether every dot-separated segment of symbol appears
// as a mapping key somewhere in the YAML text.
func yamlHasKeys(text, symbol string) bool {
	for _, seg := range strings.Split(symbol, ".") {
		re := regexp.MustCompile(`(?m)^\s*(?:-\s+)?["']?` + regexp.QuoteMeta(seg) + `["']?\s*:`)
		if !re.MatchString(text) {
			return false
		}
	}
	return true
}

func hasWord(text, word string) bool {
	return regexp.MustCompile(`(^|[^\w$-])` + regexp.QuoteMeta(word) + `($|[^\w$-])`).MatchString(text)
}

// checkSymbol returns "" when symbol is present in the pointed-at path, or a
// description of what is wrong.
func (r *repo) checkSymbol(ref, symbol string) string {
	if lineNumberRE.MatchString(symbol) {
		return fmt.Sprintf("#%s is a line number; line numbers rot, write path#Symbol instead", symbol)
	}
	full := filepath.Join(r.root, filepath.FromSlash(ref))
	info, err := os.Stat(full)
	if err != nil {
		return fmt.Sprintf("cannot read %s to look for %s", ref, symbol)
	}
	if strings.HasSuffix(ref, ".md") {
		text, err := r.read(ref)
		if err != nil {
			return err.Error()
		}
		if !hasAnchor(collectAnchors(text), symbol) {
			return fmt.Sprintf("%s has no heading with anchor #%s", ref, symbol)
		}
		return ""
	}
	if !symbolRE.MatchString(symbol) {
		return fmt.Sprintf("#%s is not an identifier; point at a declared name", symbol)
	}
	if info.IsDir() {
		entries, err := os.ReadDir(full)
		if err != nil {
			return err.Error()
		}
		var goFiles []string
		for _, e := range entries {
			if !e.IsDir() && goFileRE.MatchString(e.Name()) {
				goFiles = append(goFiles, path.Join(ref, e.Name()))
			}
		}
		if len(goFiles) == 0 {
			return fmt.Sprintf("%s is a directory without Go files, so #%s cannot be checked; point at a file", ref, symbol)
		}
		names, err := goDecls(r.root, goFiles)
		if err != nil {
			return err.Error()
		}
		if !names[symbol] {
			return fmt.Sprintf("no Go file in %s declares %s", ref, symbol)
		}
		return ""
	}
	if strings.HasSuffix(ref, ".go") {
		names, err := goDecls(r.root, []string{ref})
		if err != nil {
			return err.Error()
		}
		if !names[symbol] {
			return fmt.Sprintf("%s does not declare %s", ref, symbol)
		}
		return ""
	}
	text, err := r.read(ref)
	if err != nil {
		return err.Error()
	}
	if strings.HasSuffix(ref, ".yaml") || strings.HasSuffix(ref, ".yml") {
		if !yamlHasKeys(text, symbol) {
			return fmt.Sprintf("%s has no key %s", ref, symbol)
		}
		return ""
	}
	if !hasWord(text, symbol) {
		return fmt.Sprintf("%s no longer mentions %s", ref, symbol)
	}
	return ""
}

// ---- tests ----------------------------------------------------------------

func TestMarkdownLinks(t *testing.T) {
	r := loadRepo(t)
	for _, file := range r.md {
		text, err := r.read(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		own := collectAnchors(text)
		for _, line := range scanLines(text) {
			if line.inFence {
				continue
			}
			_, rest := splitInlineCode(line.text)
			for _, target := range linkTargets(rest) {
				if schemeRE.MatchString(target) || strings.HasPrefix(target, "//") {
					continue
				}
				target, _, _ = strings.Cut(target, "?")
				pathPart, fragment, _ := strings.Cut(target, "#")
				if pathPart == "" {
					if fragment != "" && !hasAnchor(own, fragment) {
						t.Errorf("%s:%d: link #%s has no matching heading in this file; fix the anchor or the heading", file, line.n, fragment)
					}
					continue
				}
				rel, ok := resolveLink(file, pathPart)
				if !ok {
					t.Errorf("%s:%d: link %s leaves the repository; link a tracked file or name the outside resource in plain text", file, line.n, pathPart)
					continue
				}
				if !r.listed(rel) {
					t.Errorf("%s:%d: link target %s does not exist (or is git-ignored, which GitHub cannot show); fix the path", file, line.n, pathPart)
					continue
				}
				if fragment != "" && strings.HasSuffix(rel, ".md") {
					other, err := r.read(rel)
					if err == nil && !hasAnchor(collectAnchors(other), fragment) {
						t.Errorf("%s:%d: %s has no heading with anchor #%s; fix the anchor", file, line.n, rel, fragment)
					}
				}
			}
		}
	}
}

func TestMarkdownPathPointers(t *testing.T) {
	r := loadRepo(t)
	type miss struct {
		file string
		line int
		ref  string
	}
	var missing []miss
	for _, file := range r.md {
		text, err := r.read(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		for _, line := range scanLines(text) {
			if line.inFence {
				continue
			}
			spans, _ := splitInlineCode(line.text)
			for _, span := range spans {
				tok, ok := parsePathToken(span, r.top)
				if !ok {
					continue
				}
				if !r.onDisk(tok.ref) {
					missing = append(missing, miss{file, line.n, tok.ref})
					continue
				}
				if tok.symbol == "" {
					continue
				}
				if problem := r.checkSymbol(tok.ref, tok.symbol); problem != "" {
					t.Errorf("%s:%d: `%s`: %s; update the pointer to the current name", file, line.n, span, problem)
				}
			}
		}
	}
	probes := make([]string, 0, len(missing))
	for _, m := range missing {
		probes = append(probes, m.ref)
	}
	ignored, err := r.ignored(probes)
	if err != nil {
		t.Fatalf("git check-ignore: %v", err)
	}
	for _, m := range missing {
		if ignored[m.ref] {
			continue
		}
		hint := "fix the path, or drop the backticks if it names something that does not exist yet"
		if lineSuffixRE.MatchString(m.ref) {
			hint = "line-number pointers rot; write path#Symbol instead"
		}
		t.Errorf("%s:%d: `%s` does not exist and is not git-ignored; %s", m.file, m.line, m.ref, hint)
	}
}

func TestAgentsFiles(t *testing.T) {
	r := loadRepo(t)
	found := false
	for _, file := range r.md {
		if path.Base(file) != "AGENTS.md" {
			continue
		}
		found = true
		text, err := r.read(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		limit, kind := nestedAgentsMaxBytes, "nested"
		if file == "AGENTS.md" {
			limit, kind = rootAgentsMaxBytes, "root"
		}
		if len(text) > limit {
			t.Errorf("%s: %d bytes, over the %d-byte limit for a %s AGENTS.md; move procedures into a skill under .claude/skills/, path-specific rules into .claude/rules/ or a nested AGENTS.md, and human explanations into README or docs/", file, len(text), limit, kind)
		}
		companion := path.Join(path.Dir(file), "CLAUDE.md")
		info, err := os.Lstat(filepath.Join(r.root, filepath.FromSlash(companion)))
		if err != nil {
			t.Errorf("%s: missing sibling %s; create it containing exactly %q", file, companion, claudeImport)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s: is a symlink; make it a regular file containing exactly %q (symlinks break on checkouts without core.symlinks)", companion, claudeImport)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(companion)))
		if err != nil {
			t.Errorf("%s: %v", companion, err)
			continue
		}
		if string(raw) != claudeImport {
			t.Errorf("%s: content is %q; it must be exactly %q so Claude Code imports the sibling AGENTS.md", companion, string(raw), claudeImport)
		}
	}
	if !found {
		t.Errorf("no AGENTS.md found; the root AGENTS.md is required")
	}
}

var (
	typeLineRE = regexp.MustCompile(`^> Type: ([a-z]+)\s*$`)
	stampRE    = regexp.MustCompile(`Verified at [0-9a-f]{7,40}\b`)
	docPageRE  = regexp.MustCompile(`^docs/[^/]+\.md$`)
)

// docType reads the "> Type:" line, which must be the first non-blank line
// after the H1, and returns the type plus the rest of that blockquote.
func docType(text string) (typ string, quote []string, problem string) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || !strings.HasPrefix(lines[0], "# ") {
		return "", nil, "first line is not an H1 (\"# Title\")"
	}
	i := 1
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return "", nil, "missing \"> Type: <type>\" line under the H1"
	}
	m := typeLineRE.FindStringSubmatch(lines[i])
	if m == nil {
		return "", nil, fmt.Sprintf("line %d must be \"> Type: reference|runbook|decision|record\" (the first line after the H1)", i+1)
	}
	for j := i + 1; j < len(lines) && strings.HasPrefix(lines[j], ">"); j++ {
		quote = append(quote, lines[j])
	}
	return m[1], quote, ""
}

func TestDocsTypesAndIndex(t *testing.T) {
	r := loadRepo(t)
	index, err := r.read("docs/README.md")
	if err != nil {
		t.Fatalf("docs/README.md: %v; it is the index of docs/ pages and their types", err)
	}
	rows := map[string]string{} // page -> index line that links it
	for _, line := range scanLines(index) {
		if line.inFence {
			continue
		}
		_, rest := splitInlineCode(line.text)
		for _, target := range linkTargets(rest) {
			pathPart, _, _ := strings.Cut(target, "#")
			if rel, ok := resolveLink("docs/README.md", pathPart); ok {
				rows[rel] = line.text
			}
		}
	}
	for _, file := range r.md {
		if !docPageRE.MatchString(file) || file == "docs/README.md" {
			continue
		}
		text, err := r.read(file)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		typ, quote, problem := docType(text)
		if problem != "" {
			t.Errorf("%s: %s", file, problem)
			continue
		}
		if !docTypes[typ] {
			t.Errorf("%s: type %q is not one of reference, runbook, decision, record", file, typ)
			continue
		}
		if typ == "record" && !stampRE.MatchString(strings.Join(quote, "\n")) {
			t.Errorf("%s: a record page needs \"> Verified at <short sha> on <date>; snapshot, not maintained.\" in the blockquote under its Type line", file)
		}
		row, listed := rows[file]
		if !listed {
			t.Errorf("%s: not listed in docs/README.md; add a row with its type", file)
			continue
		}
		if !regexp.MustCompile(`(^|[^a-z])` + typ + `([^a-z]|$)`).MatchString(row) {
			t.Errorf("docs/README.md: the row for %s does not say %q, but the page declares that type; make them agree", file, typ)
		}
	}
}

// ---- unit tests for the helpers -------------------------------------------

func TestGithubSlug(t *testing.T) {
	cases := map[string]string{
		"Merging with `--admin`":               "merging-with---admin",
		"This service has no auth, on purpose": "this-service-has-no-auth-on-purpose",
		"Rule -> enforced by":                  "rule---enforced-by",
		"架构":                                   "架构",
		"[Link](x.md) and <b>bold</b>":         "link-and-bold",
		"Two Apple hosts, not one":             "two-apple-hosts-not-one",
		"snake_case_kept 1.2":                  "snake_case_kept-12",
	}
	for in, want := range cases {
		if got := githubSlug(in); got != want {
			t.Errorf("githubSlug(%q) = %q, want %q", in, got, want)
		}
	}
	anchors := collectAnchors("# A\n## A\n```\n# not a heading\n```\n<a id=\"Custom\"></a>\n")
	for _, want := range []string{"a", "a-1", "custom"} {
		if !anchors[want] {
			t.Errorf("collectAnchors missing %q in %v", want, anchors)
		}
	}
	if anchors["not-a-heading"] {
		t.Errorf("collectAnchors read a heading inside a code fence")
	}
}

func TestParsePathToken(t *testing.T) {
	top := map[string]bool{"internal": true, "docs": true, "Makefile": true, ".github": true}
	cases := []struct {
		span   string
		ok     bool
		ref    string
		symbol string
	}{
		{"internal/api/openapi.yaml", true, "internal/api/openapi.yaml", ""},
		{"./internal/api/", true, "internal/api/", ""},
		{"internal/applemusic#apiBase", true, "internal/applemusic", "apiBase"},
		{"Makefile", true, "Makefile", ""},
		{"go test ./internal/api/", false, "", ""},
		{".github/release-notes/<version>.md", false, "", ""},
		{"docs/*.md", false, "", ""},
		{"internal/...", false, "", ""},
		{"/api/v1/downloads", false, "", ""},
		{"https://example.com/docs", false, "", ""},
		{"api.music.apple.com", false, "", ""},
		{"AMDL_<SECTION>_<KEY>", false, "", ""},
	}
	for _, c := range cases {
		got, ok := parsePathToken(c.span, top)
		if ok != c.ok || got.ref != c.ref || got.symbol != c.symbol {
			t.Errorf("parsePathToken(%q) = %+v, %v; want ref %q symbol %q ok %v", c.span, got, ok, c.ref, c.symbol, c.ok)
		}
	}
}

func TestDocType(t *testing.T) {
	typ, quote, problem := docType("# Bench\n\n> Type: record\n>\n> Verified at abc1234 on 2026-01-01; snapshot, not maintained.\n\nBody\n")
	if problem != "" || typ != "record" || !stampRE.MatchString(strings.Join(quote, "\n")) {
		t.Errorf("docType(record) = %q, %q, %q", typ, quote, problem)
	}
	if _, _, problem := docType("# Page\n\nIntro without a type.\n"); problem == "" {
		t.Errorf("docType accepted a page without a Type line")
	}
	if _, _, problem := docType("Intro\n# Page\n> Type: reference\n"); problem == "" {
		t.Errorf("docType accepted a page whose first line is not an H1")
	}
}
