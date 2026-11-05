// Command framecheck reads films of the desktop window.
//
//	framecheck sheet [-known <file>] <dir>
//	    the scene set's results under dir (TestScenes, $SCENE_OUT) as
//	    dir/sheet.jpg and dir/summary.txt; exits 1 when a scene did not run
//	    to its end or has a finding the known file does not list
//	framecheck film <dir>
//	    the PNG frames in dir, in name order, checked as one film by package
//	    filmcheck; exits 1 on a finding
//	framecheck rec [-threshold 60] [-light] [-least 2] [-out <dir>] <movie> <W>x<H>+<X>+<Y>
//	    one element of a screen recording, at the recording's own frames:
//	    a table of where its ink lies in the region, its snaps and jumps
//	    (filmcheck.Steps, the check the scene set runs on every window), and
//	    a strip of the frames around each in dir; exits 1 on a step
//
// A line of the known file is "<scene>/<theme or *> <segment> <kind>
// <why>", one finding of the current tree the maintainer has been told
// of; "#" starts a comment. tools/lookround runs sheet after the scene set
// with apps/desktop/internal/ui/testdata/scenes-known.txt.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"amdl/apps/desktop/internal/filmcheck"
)

const usage = `usage: framecheck sheet [-known <file>] <dir>
       framecheck film <dir>
       framecheck rec [-threshold 60] [-light] [-least 2] [-out <dir>] <movie> <W>x<H>+<X>+<Y>`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, errw io.Writer) int {
	bad := func() int { fmt.Fprintln(errw, usage); return 2 }
	if len(args) < 2 {
		return bad()
	}
	cmd, args := args[0], args[1:]
	if cmd == "rec" {
		return rec(args, out, errw)
	}
	known := ""
	if cmd == "sheet" && len(args) == 3 && args[0] == "-known" {
		known, args = args[1], args[2:]
	}
	if len(args) != 1 || (cmd != "sheet" && cmd != "film") || strings.HasPrefix(args[0], "-") {
		return bad()
	}
	dir := args[0]
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		fmt.Fprintf(errw, "framecheck: %s is not a directory\n", dir)
		return 2
	}
	var failed bool
	var err error
	if cmd == "sheet" {
		var list []knownEntry
		if known != "" {
			if list, err = readKnown(known); err != nil {
				fmt.Fprintln(errw, "framecheck:", err)
				return 2
			}
		}
		failed, err = sheet(dir, list, out)
	} else {
		failed, err = film(dir, out)
	}
	if err != nil {
		fmt.Fprintln(errw, "framecheck:", err)
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

// result is what TestScenes writes as result.json; keep in step with
// apps/desktop/internal/ui#sceneResult.
type result struct {
	Scene    string  `json:"scene"`
	Theme    string  `json:"theme"`
	Failed   bool    `json:"failed"`
	Error    string  `json:"error"`
	Seconds  float64 `json:"seconds"`
	Segments []struct {
		Name     string              `json:"name"`
		Frames   int                 `json:"frames"`
		Findings []filmcheck.Finding `json:"findings"`
		Cells    []struct {
			File    string `json:"file"`
			Caption string `json:"caption"`
			Bad     bool   `json:"bad"`
		} `json:"cells"`
		Settled string `json:"settled"`
	} `json:"segments"`
	Notes []string `json:"notes"`
	dir   string
}

// knownEntry is a line of the known file.
type knownEntry struct {
	scene, theme, segment, kind, why string
	line                             int
	seen                             bool
}

func readKnown(path string) ([]knownEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []knownEntry
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		scene, theme, ok := strings.Cut(fields[0], "/")
		if len(fields) < 4 || !ok || scene == "" || theme == "" {
			return nil, fmt.Errorf("%s:%d: want \"<scene>/<theme or *> <segment> <kind> <why>\"", path, n)
		}
		out = append(out, knownEntry{scene: scene, theme: theme, segment: fields[1], kind: fields[2],
			why: strings.Join(fields[3:], " "), line: n})
	}
	return out, sc.Err()
}

func match(list []knownEntry, r result, segment string, f filmcheck.Finding) *knownEntry {
	for i := range list {
		k := &list[i]
		if k.scene == r.Scene && (k.theme == "*" || k.theme == r.Theme) && k.segment == segment && k.kind == f.Kind {
			return k
		}
	}
	return nil
}

func sheet(dir string, known []knownEntry, out io.Writer) (bool, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*", "result.json"))
	if err != nil {
		return false, err
	}
	if len(paths) == 0 {
		return false, errors.New("no scene results under " + dir)
	}
	var results []result
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return false, err
		}
		var r result
		if err := json.Unmarshal(raw, &r); err != nil {
			return false, fmt.Errorf("%s: %w", p, err)
		}
		r.dir = filepath.Dir(p)
		results = append(results, r)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].dir < results[j].dir })
	var b strings.Builder
	var rows []filmcheck.Row
	failed, frames, knownN := 0, 0, 0
	for _, r := range results {
		var lines []string
		bad := r.Error != ""
		n := 0
		row := filmcheck.Row{}
		for _, s := range r.Segments {
			n += s.Frames
			for _, f := range s.Findings {
				if k := match(known, r, s.Name, f); k != nil {
					k.seen = true
					knownN++
					lines = append(lines, fmt.Sprintf("    known: %s: %s (%s)", s.Name, f, k.why))
					continue
				}
				bad = true
				lines = append(lines, fmt.Sprintf("    %s: %s", s.Name, f))
			}
			for _, c := range s.Cells {
				img, err := readPNG(filepath.Join(r.dir, c.File))
				if err != nil {
					return false, err
				}
				row.Cells = append(row.Cells, filmcheck.Thumb{Img: img, Caption: c.Caption, Bad: c.Bad})
			}
		}
		frames += n
		verdict := "PASS"
		if bad {
			verdict, failed = "FAIL", failed+1
		}
		row.Title, row.Bad = fmt.Sprintf("%s %s/%s", verdict, r.Scene, r.Theme), bad
		fmt.Fprintf(&b, "%s %s/%s (%d frames, %.0f s)\n", verdict, r.Scene, r.Theme, n, r.Seconds)
		if r.Error != "" {
			fmt.Fprintf(&b, "    %s\n", r.Error)
		}
		for _, l := range lines {
			fmt.Fprintln(&b, l)
		}
		rows = append(rows, row)
	}
	for _, k := range known {
		if !k.seen {
			fmt.Fprintf(&b, "known finding not seen this run (line %d of the known file; drop it once it stays away): %s/%s %s %s\n", k.line, k.scene, k.theme, k.segment, k.kind)
		}
	}
	verdict := "PASS"
	if failed > 0 {
		verdict = "FAIL"
	}
	head := fmt.Sprintf("%s: %d of %d scenes failed, %d known findings, %d frames checked\n", verdict, failed, len(results), knownN, frames)
	summary := head + b.String()
	if err := os.WriteFile(filepath.Join(dir, "summary.txt"), []byte(summary), 0o644); err != nil {
		return false, err
	}
	if err := writeJPEG(filepath.Join(dir, "sheet.jpg"), filmcheck.Sheet(rows, 8)); err != nil {
		return false, err
	}
	fmt.Fprint(out, summary)
	return failed > 0, nil
}

func film(dir string, out io.Writer) (bool, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.png"))
	if err != nil {
		return false, err
	}
	sort.Strings(paths)
	if len(paths) < 3 {
		return false, fmt.Errorf("%s holds %d PNG frames; a film needs 3", dir, len(paths))
	}
	var f filmcheck.Film
	for _, p := range paths {
		img, err := readPNG(p)
		if err != nil {
			return false, err
		}
		f.Add(img, nil, false)
	}
	found := f.Finish()
	for _, x := range found {
		fmt.Fprintf(out, "%s (%s)\n", x, filepath.Base(paths[x.Frame]))
	}
	fmt.Fprintf(out, "%d frames, %d findings\n", len(paths), len(found))
	return len(found) > 0, nil
}

func readPNG(p string) (*image.RGBA, error) {
	fh, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	m, err := png.Decode(fh)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if r, ok := m.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r, nil
	}
	r := image.NewRGBA(image.Rect(0, 0, m.Bounds().Dx(), m.Bounds().Dy()))
	draw.Draw(r, r.Bounds(), m, m.Bounds().Min, draw.Src)
	return r, nil
}

// writeJPEG writes the sheet as a JPEG, a quarter of its PNG's size, to
// copy back over a slow link.
func writeJPEG(p string, img image.Image) error {
	fh, err := os.Create(p)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(fh, img, &jpeg.Options{Quality: 85}); err != nil {
		fh.Close()
		return err
	}
	return fh.Close()
}
