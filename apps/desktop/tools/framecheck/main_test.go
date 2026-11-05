package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGrey(t *testing.T, p string, v uint8) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 40))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.RGBA{v, v, v, 255}), image.Point{}, draw.Src)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// scenes is a results directory as TestScenes leaves it: one scene clean,
// one with a pop in its open segment.
func scenes(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.light", "result.json"), `{"scene":"a","theme":"light","segments":[
		{"name":"open","frames":3,"cells":[{"file":"cell.png","caption":"open 0"}]}]}`)
	writeGrey(t, filepath.Join(dir, "a.light", "cell.png"), 200)
	writeFile(t, filepath.Join(dir, "b.dark", "result.json"), `{"scene":"b","theme":"dark","segments":[
		{"name":"open","frames":3,"findings":[{"kind":"pop","frame":2,"frames":1,"detail":"a badge"}],
		 "cells":[{"file":"cell.png","caption":"open 2","bad":true}]}]}`)
	writeGrey(t, filepath.Join(dir, "b.dark", "cell.png"), 40)
	return dir
}

func TestSheetWritesTheFindingsPageWithTheFramesAround(t *testing.T) {
	dir := scenes(t)
	for i, v := range []uint8{40, 90, 40} {
		writeGrey(t, filepath.Join(dir, "b.dark", fmt.Sprintf("bad-%03d.png", i+1)), v)
	}
	var out, errw strings.Builder
	run([]string{"sheet", dir}, &out, &errw)
	raw, err := os.ReadFile(filepath.Join(dir, "findings.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	if !strings.Contains(page, "pop at frame 2: a badge") || !strings.Contains(page, "b · dark · 方向 open · 第 2 帧") ||
		strings.Count(page, `alt="前一帧，原大"`) != 1 || strings.Count(page, `alt="后一帧，原大"`) != 1 {
		t.Errorf("page lacks the finding or its three frames (%d bytes)", len(page))
	}
}

func TestSheetFailsOnAFindingAndPassesWhenItIsKnown(t *testing.T) {
	dir := scenes(t)
	var out, errw strings.Builder
	if code := run([]string{"sheet", dir}, &out, &errw); code != 1 {
		t.Fatalf("exit %d with a pop, want 1: %s%s", code, out.String(), errw.String())
	}
	if !strings.HasPrefix(out.String(), "FAIL: 1 of 2 scenes failed") || !strings.Contains(out.String(), "FAIL b/dark") {
		t.Errorf("summary %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "sheet.jpg")); err != nil {
		t.Errorf("no sheet: %v", err)
	}

	known := filepath.Join(t.TempDir(), "known.txt")
	writeFile(t, known, "# reported\nb/* open pop the badge lands late\na/light back flicker gone since\nc/light back flicker not filmed\n")
	out.Reset()
	if code := run([]string{"sheet", "-known", known, dir}, &out, &errw); code != 0 {
		t.Fatalf("exit %d with the pop known, want 0: %s%s", code, out.String(), errw.String())
	}
	for _, want := range []string{"PASS: 0 of 2 scenes failed, 1 known findings", "known: open: pop at frame 2", "line 3 of the known file"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary lacks %q: %q", want, out.String())
		}
	}
	// Scene c was not filmed, so its line is neither seen nor gone.
	if strings.Contains(out.String(), "line 4 of the known file") {
		t.Errorf("summary calls a scene that did not run unseen: %q", out.String())
	}
}

func TestBadArgumentsExit2(t *testing.T) {
	dir := scenes(t)
	bad := filepath.Join(t.TempDir(), "known.txt")
	writeFile(t, bad, "b/dark open\n")
	for _, args := range [][]string{
		nil,
		{"sheet"},
		{"draw", dir},
		{"sheet", "-known", dir},
		{"sheet", "-x", "y", dir},
		{"sheet", filepath.Join(dir, "missing")},
		{"sheet", "-known", bad, dir},
		{"sheet", "-known", filepath.Join(dir, "none.txt"), dir},
	} {
		var out, errw strings.Builder
		if code := run(args, &out, &errw); code != 2 {
			t.Errorf("framecheck %q: exit %d, want 2", args, code)
		}
	}
}
