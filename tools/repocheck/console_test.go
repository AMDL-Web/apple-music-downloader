package repocheck

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"amdl/internal/progress"
)

// The checks on the web console's static files (apps/console/static, which
// the console package embeds as it is on disk) and on the values the files
// share with the Go code. The console's own tests (apps/console) cover what
// the handler serves.

// consoleStatic is apps/console/static, the files the console embeds.
var consoleStatic = filepath.Join("..", "..", "apps", "console", "static")

// The vendored Preact+htm bundle and its upstream checksum.
const (
	consoleVendorDir    = "vendor/"
	consoleVendorFile   = "vendor/htm-preact-standalone-3.1.1.mjs"
	consoleVendorSHA256 = "72284e8e9079c87817145df1110f74e8a2aa040b2fc384922e18dfcb46fc1fd7"
)

func consoleFS() fs.FS { return os.DirFS(consoleStatic) }

func readConsole(t *testing.T, name string) string {
	t.Helper()
	b, err := fs.ReadFile(consoleFS(), name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

func TestVendorChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte(readConsole(t, consoleVendorFile)))
	if got := hex.EncodeToString(sum[:]); got != consoleVendorSHA256 {
		t.Fatalf("%s sha256 = %s, want %s; the vendored file must stay byte-identical to upstream", consoleVendorFile, got, consoleVendorSHA256)
	}
}

var (
	attrURL    = regexp.MustCompile(`\b(?:src|href)="([^"]*)"`)
	importSpec = regexp.MustCompile(`(?m)(?:^\s*import\s+(?:[^'"]*?\s+from\s+)?|^\s*export\s+[^'"]*?\s+from\s+|\bimport\s*\(\s*)['"]([^'"]+)['"]`)
)

// TestIndexReferencesResolve checks that every relative src/href in
// index.html names an embedded file, so the page works under /console/.
func TestIndexReferencesResolve(t *testing.T) {
	files := consoleFS()
	matches := attrURL.FindAllStringSubmatch(readConsole(t, "index.html"), -1)
	checked := 0
	for _, m := range matches {
		ref := m[1]
		if ref == "" || strings.HasPrefix(ref, "data:") || strings.HasPrefix(ref, "../") || strings.Contains(ref, "://") || strings.HasPrefix(ref, "#") {
			continue
		}
		if strings.HasPrefix(ref, "/") {
			t.Errorf("index.html references %q with an absolute path; use a relative one so the console works under any prefix", ref)
			continue
		}
		if _, err := fs.Stat(files, ref); err != nil {
			t.Errorf("index.html references %q, which is not embedded", ref)
		}
		checked++
	}
	if checked < 3 {
		t.Fatalf("checked only %d references in index.html; the attribute pattern is broken", checked)
	}
}

func consoleJSFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(consoleFS(), "js", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := path.Ext(name); ext == ".js" || ext == ".mjs" {
			out[name] = readConsole(t, name)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 5 {
		t.Fatalf("found only %d JS files under static/js", len(out))
	}
	return out
}

// TestModuleImportsResolve checks that every import in static/js is a
// relative specifier naming an embedded file: there is no bundler and no
// import map, so a bare or broken specifier fails in the browser.
func TestModuleImportsResolve(t *testing.T) {
	files := consoleFS()
	total := 0
	for name, src := range consoleJSFiles(t) {
		for _, m := range importSpec.FindAllStringSubmatch(src, -1) {
			spec := m[1]
			total++
			if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
				t.Errorf("%s imports %q; only relative specifiers resolve without a bundler", name, spec)
				continue
			}
			target := path.Clean(path.Join(path.Dir(name), spec))
			if _, err := fs.Stat(files, target); err != nil {
				t.Errorf("%s imports %q, which resolves to %s and is not embedded", name, spec, target)
			}
		}
	}
	if total < 20 {
		t.Fatalf("found only %d imports; the import pattern is broken", total)
	}
}

var (
	colorLiteral    = regexp.MustCompile(`(?i)#[0-9a-f]{3}(?:[0-9a-f]{1,5})?\b|\b(?:rgba?|hsla?|hwb|oklch|oklab|lab|lch)\(`)
	transitionAll   = regexp.MustCompile(`(?i)transition(?:-property)?\s*:\s*all\b`)
	fontSizeDecl    = regexp.MustCompile(`font-size:\s*([^;]+);`)
	innerHTMLWrites = regexp.MustCompile(`\.(?:inner|outer)HTML\s*=|insertAdjacentHTML|dangerouslySetInnerHTML|document\.write`)
)

func hasCJK(s string) (rune, bool) {
	for _, r := range s {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return r, true
		}
	}
	return 0, false
}

// colorFile reports whether a static file may contain color literals: the
// skin-neutral tokens and the skin files, which own every color.
func colorFile(name string) bool {
	return name == "css/tokens.css" || (strings.HasPrefix(name, "css/skins/") && path.Ext(name) == ".css")
}

// designViolations returns what the design guard rejects in one file:
// color literals outside css/tokens.css and css/skins/*.css, "transition:
// all", CJK characters in UI copy, raw HTML writes, and font sizes that
// bypass the type scale.
func designViolations(name, text string) []string {
	var out []string
	if !colorFile(name) {
		for _, m := range colorLiteral.FindAllString(text, -1) {
			out = append(out, "color literal "+m+" (colors live in css/tokens.css and css/skins/*.css)")
		}
	}
	if m := transitionAll.FindString(text); m != "" {
		out = append(out, `"`+m+`" (name the properties that transition)`)
	}
	if r, ok := hasCJK(text); ok {
		out = append(out, "CJK character "+string(r)+" (UI copy is English; Apple data arrives at runtime)")
	}
	if path.Ext(name) == ".js" {
		if m := innerHTMLWrites.FindString(text); m != "" {
			out = append(out, m+" (render through htm, which escapes data)")
		}
	}
	if path.Ext(name) == ".css" {
		for _, m := range fontSizeDecl.FindAllStringSubmatch(text, -1) {
			if !strings.HasPrefix(strings.TrimSpace(m[1]), "var(--fs-") {
				out = append(out, "font-size: "+m[1]+" (use a --fs-* token)")
			}
		}
	}
	return out
}

func TestDesignGuard(t *testing.T) {
	scanned := 0
	err := fs.WalkDir(consoleFS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(name, consoleVendorDir) {
			return err
		}
		scanned++
		for _, v := range designViolations(name, readConsole(t, name)) {
			t.Errorf("static/%s: %s", name, v)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 10 {
		t.Fatalf("design guard scanned only %d files", scanned)
	}
}

func TestDesignGuardCatchesViolations(t *testing.T) {
	cases := []struct {
		name, file, text string
	}{
		{"hex color", "css/base.css", "a { color: #e35e38; }"},
		{"short hex", "js/x.js", "const c = '#fff';"},
		{"rgb", "css/base.css", "a { color: rgb(0 0 0); }"},
		{"oklch", "css/base.css", "a { color: oklch(0.5 0.1 20); }"},
		{"transition all", "css/base.css", "a { transition: all 200ms; }"},
		{"cjk", "js/x.js", "const label = '下载';"},
		{"innerHTML", "js/x.js", "el.innerHTML = name;"},
		{"raw font size", "css/base.css", "a { font-size: 13px; }"},
		{"raw font size in a skin", "css/skins/music.css", "a { font-size: 13px; }"},
		{"color in a js file under a skins-like path", "js/skins/x.js", "const c = '#fff';"},
		{"color in a non-css skin file", "css/skins/notes.txt", "#ffffff"},
	}
	for _, c := range cases {
		if len(designViolations(c.file, c.text)) == 0 {
			t.Errorf("%s: guard accepted %q", c.name, c.text)
		}
	}
	clean := []struct{ file, text string }{
		{"css/tokens.css", "--x: light-dark(#ffffff, #111113);"},
		{"css/skins/music.css", ":root[data-skin=\"music\"] { --x: light-dark(#ffffff, rgba(0, 0, 0, 0.5)); }"},
		{"css/skins/music.css", "a { font-size: var(--fs-page-title); }"},
		{"js/x.js", "location.hash = '#/queue'; const bg = `#${hex}`;"},
		{"css/shell.css", "a { font-size: var(--fs-12); transition: opacity 120ms; }"},
	}
	for _, c := range clean {
		if v := designViolations(c.file, c.text); len(v) != 0 {
			t.Errorf("guard rejected clean %s %q: %v", c.file, c.text, v)
		}
	}
}

func TestIndexHasNoInlineCode(t *testing.T) {
	index := readConsole(t, "index.html")
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`<script(?:\s[^>]*)?>\s*[^<\s]`),
		regexp.MustCompile(`<style`),
		regexp.MustCompile(`\sstyle="`),
		regexp.MustCompile(`\son[a-z]+="`),
	} {
		if m := re.FindString(index); m != "" {
			t.Errorf("index.html contains %q; the CSP blocks inline scripts, styles and handlers", m)
		}
	}
}

var (
	componentTag  = regexp.MustCompile(`"component",\s*"([a-z_]+)"`)
	logComponents = regexp.MustCompile(`export const LOG_COMPONENTS = \[([^\]]*)\]`)
	quotedWord    = regexp.MustCompile(`'([a-z_]+)'`)
)

// The Logs page offers a component filter; a tag the backend logs with but the
// list lacks can only be reached by editing the URL.
func TestLogComponentsMatchBackend(t *testing.T) {
	backend := map[string]bool{}
	for _, root := range []string{filepath.Join("..", "..", "apps", "amdl"), filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range componentTag.FindAllStringSubmatch(string(src), -1) {
				backend[m[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(backend) < 5 {
		t.Fatalf("found only %d component tags in the Go code; the pattern is broken", len(backend))
	}
	list := logComponents.FindStringSubmatch(readConsole(t, "js/lib/route.js"))
	if list == nil {
		t.Fatal("LOG_COMPONENTS not found in lib/route.js")
	}
	console := map[string]bool{}
	for _, m := range quotedWord.FindAllStringSubmatch(list[1], -1) {
		console[m[1]] = true
	}
	for _, name := range slices.Sorted(maps.Keys(backend)) {
		if !console[name] {
			t.Errorf("the backend logs component %q, but LOG_COMPONENTS in lib/route.js lacks it", name)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(console)) {
		if !backend[name] {
			t.Errorf("LOG_COMPONENTS lists %q, which no Go code logs as a component", name)
		}
	}
}

// TestWeightsMatchConsole keeps the console's STAGE_WEIGHTS (lib/status.js)
// and TICK_MAX (lib/album-progress.js) equal to internal/progress's Weights
// and TickMax, so the Go and JavaScript bars agree.
func TestWeightsMatchConsole(t *testing.T) {
	raw := readConsole(t, "js/lib/status.js")
	m := regexp.MustCompile(`STAGE_WEIGHTS = Object\.freeze\(\{([^}]*)\}\)`).FindStringSubmatch(raw)
	if m == nil {
		t.Fatal("STAGE_WEIGHTS not found in status.js")
	}
	keys := map[string]progress.Stage{"resolve": progress.StageResolve, "download": progress.StageDownload, "decrypt": progress.StageDecrypt,
		"remux": progress.StageRemux, "verify": progress.StageVerify, "tag": progress.StageTag, "save": progress.StageSave}
	seen := 0
	for _, kv := range regexp.MustCompile(`(\w+):\s*([0-9.]+)`).FindAllStringSubmatch(m[1], -1) {
		s, ok := keys[kv[1]]
		if !ok {
			t.Errorf("console stage %q has no Go stage", kv[1])
			continue
		}
		v, _ := strconv.ParseFloat(kv[2], 64)
		if v != progress.Weights[s] {
			t.Errorf("weight of %s: console %v, Go %v", kv[1], v, progress.Weights[s])
		}
		seen++
	}
	if seen != int(progress.StageCount) {
		t.Errorf("console lists %d stages, Go has %d", seen, progress.StageCount)
	}

	tm := regexp.MustCompile(`TICK_MAX = (\d+);`).FindStringSubmatch(readConsole(t, "js/lib/album-progress.js"))
	if tm == nil || tm[1] != strconv.Itoa(progress.TickMax) {
		t.Errorf("console TICK_MAX %q, Go %d", tm, progress.TickMax)
	}
}

var (
	varRef  = regexp.MustCompile(`var\(\s*(--[A-Za-z0-9_-]+)`)
	varDecl = regexp.MustCompile(`(?m)(?:^|[;{\s])(--[A-Za-z0-9_-]+)\s*:`)
	// Custom properties set per element from JS style objects or by the
	// role mapping, a job row without progress, the Queue's trailing slot
	// and a status head's title row in the base CSS itself, not by a skin;
	// --sort-* are a list box's resize, set by ui/sortable.js; --digits is
	// a progress legend's count width, set by JobProgress.
	runtimeVars = map[string]bool{
		"--art-bg": true, "--art-t1": true, "--art-t2": true, "--art-t3": true,
		"--w": true, "--f": true, "--v": true, "--n": true, "--swatch": true,
		"--role": true, "--role-tint": true, "--new-bar-h": true, "--job-prog": true, "--job-trail": true, "--cols": true,
		"--save-x": true, "--save-w": true, "--domain-title-h": true,
		"--hook-more-w": true, "--hook-controls-w": true,
		"--sort-y": true, "--sort-scale": true, "--sort-dur": true,
		"--digits": true,
	}
)

func cssNames(re *regexp.Regexp, text string) map[string]bool {
	out := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out[m[1]] = true
	}
	return out
}

// baseCSS returns the base stylesheets, every css/*.css except tokens.css:
// the structure under the skin, split by area.
func baseCSS(t *testing.T) []string {
	t.Helper()
	names, err := fs.Glob(consoleFS(), "css/*.css")
	if err != nil {
		t.Fatal(err)
	}
	out := slices.DeleteFunc(names, func(n string) bool { return n == "css/tokens.css" })
	if len(out) < 5 {
		t.Fatalf("found only %d base stylesheets under css/", len(out))
	}
	return out
}

// TestSkinsDeclareEveryBaseToken keeps the skin complete: every custom
// property a base stylesheet (css/*.css other than tokens.css) reads that
// tokens.css does not declare (colors, radii, shadows, fonts, display type,
// card shape) must be declared by each css/skins/*.css, so a token added to
// the base without a value gets a failing test instead of a transparent
// badge. It runs with the repository checks (make test, CI); the failure
// names the skin, the token and a base file that reads it.
func TestSkinsDeclareEveryBaseToken(t *testing.T) {
	files := consoleFS()
	neutral := cssNames(varDecl, readConsole(t, "css/tokens.css"))
	readBy := map[string]string{}
	for _, base := range baseCSS(t) {
		for name := range cssNames(varRef, readConsole(t, base)) {
			if _, seen := readBy[name]; !seen && !neutral[name] && !runtimeVars[name] {
				readBy[name] = base
			}
		}
	}
	required := slices.Sorted(maps.Keys(readBy))
	if len(required) < 20 {
		t.Fatalf("the base stylesheets read only %d skin tokens; the var() pattern is broken", len(required))
	}
	skins, err := fs.Glob(files, "css/skins/*.css")
	if err != nil {
		t.Fatal(err)
	}
	if len(skins) == 0 {
		t.Fatal("found no skin under css/skins")
	}
	index := readConsole(t, "index.html")
	for _, skin := range skins {
		declared := cssNames(varDecl, readConsole(t, skin))
		for _, name := range required {
			if !declared[name] {
				t.Errorf("static/%s does not declare %s, which %s reads", skin, name, readBy[name])
			}
		}
		if !strings.Contains(index, `href="`+skin+`"`) {
			t.Errorf("index.html does not link %s", skin)
		}
		scope := `:root[data-skin="` + strings.TrimSuffix(path.Base(skin), ".css") + `"]`
		if !strings.Contains(readConsole(t, skin), scope) {
			t.Errorf("static/%s has no %s block; a skin is scoped by its file name", skin, scope)
		}
	}
}

var stylesheetLink = regexp.MustCompile(`<link rel="stylesheet" href="([^"]+)">`)

// TestStylesheetsLinkedInOrder checks the cascade order index.html sets:
// tokens.css first, then every base stylesheet, then the skins, so a skin
// rule always follows the base rule it overrides. An embedded stylesheet
// that index.html does not link would ship without ever applying.
func TestStylesheetsLinkedInOrder(t *testing.T) {
	var links []string
	for _, m := range stylesheetLink.FindAllStringSubmatch(readConsole(t, "index.html"), -1) {
		links = append(links, m[1])
	}
	if len(links) == 0 || links[0] != "css/tokens.css" {
		t.Fatalf("index.html stylesheets = %v, want css/tokens.css first", links)
	}
	skins, err := fs.Glob(consoleFS(), "css/skins/*.css")
	if err != nil {
		t.Fatal(err)
	}
	all := append(append([]string{"css/tokens.css"}, baseCSS(t)...), skins...)
	for _, name := range all {
		if !slices.Contains(links, name) {
			t.Errorf("index.html does not link %s", name)
		}
	}
	firstSkin := slices.IndexFunc(links, func(l string) bool { return strings.HasPrefix(l, "css/skins/") })
	if firstSkin < 0 {
		t.Fatal("index.html links no skin")
	}
	for i, l := range links {
		if i > firstSkin && !strings.HasPrefix(l, "css/skins/") {
			t.Errorf("index.html links %s after the first skin; base stylesheets precede every skin", l)
		}
	}
}
