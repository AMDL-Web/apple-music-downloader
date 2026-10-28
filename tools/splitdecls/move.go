package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"sort"
)

func runMove(args []string) error {
	fs := flag.NewFlagSet("move", flag.ExitOnError)
	dir := fs.String("dir", "", "package directory")
	srcName := fs.String("src", "", "source file name inside -dir")
	planPath := fs.String("plan", "", "JSON plan: target file -> declaration keys")
	_ = fs.Parse(args)
	if *dir == "" || *srcName == "" || *planPath == "" {
		usage()
	}
	planBytes, err := os.ReadFile(*planPath)
	if err != nil {
		return err
	}
	var plan map[string][]string
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		return fmt.Errorf("parse plan: %w", err)
	}
	return move(*dir, *srcName, plan)
}

func move(dir, srcName string, plan map[string][]string) error {
	srcPath := filepath.Join(dir, srcName)
	fset, f, src, err := parseFile(srcPath)
	if err != nil {
		return err
	}
	segs, _ := segments(fset, f, src)
	keyToSeg := map[string]int{}
	for i, s := range segs {
		for _, k := range s.keys {
			keyToSeg[k] = i
		}
	}
	targets := make([]string, 0, len(plan))
	for t := range plan {
		if t == srcName {
			return fmt.Errorf("plan target %s is the source file", t)
		}
		targets = append(targets, t)
	}
	sort.Strings(targets)
	segTarget := map[int]string{}
	for _, t := range targets {
		for _, k := range plan[t] {
			i, ok := keyToSeg[k]
			if !ok {
				return fmt.Errorf("declaration %q not found in %s", k, srcName)
			}
			if prev, dup := segTarget[i]; dup && prev != t {
				return fmt.Errorf("declaration %q is grouped with one already sent to %s", k, prev)
			}
			segTarget[i] = t
		}
	}

	srcImports := importsOf(f)
	bodies := map[string][][]byte{}
	var remain [][]byte
	for i, s := range segs {
		chunk := bytes.Trim(src[s.start:s.end], "\n")
		if t, ok := segTarget[i]; ok {
			bodies[t] = append(bodies[t], chunk)
		} else {
			remain = append(remain, chunk)
		}
	}
	if len(segs) > 0 {
		if tail := bytes.Trim(src[segs[len(segs)-1].end:], "\n"); len(tail) > 0 {
			remain = append(remain, tail)
		}
	}

	for _, t := range targets {
		path := filepath.Join(dir, t)
		prefix := []byte("package " + f.Name.Name + "\n")
		var existingBody []byte
		imports := append([]importSpec(nil), srcImports...)
		if _, statErr := os.Stat(path); statErr == nil {
			tfset, tf, tsrc, err := parseFile(path)
			if err != nil {
				return err
			}
			var tHeaderEnd int
			prefix, tHeaderEnd = splitHeader(tfset, tf, tsrc)
			existingBody = bytes.Trim(tsrc[tHeaderEnd:], "\n")
			imports = append(importsOf(tf), imports...)
		}
		if err := writeGoFile(path, prefix, imports, append([][]byte{existingBody}, bodies[t]...)); err != nil {
			return err
		}
	}
	if len(remain) == 0 {
		if err := os.Remove(srcPath); err != nil {
			return err
		}
	} else {
		prefix, _ := splitHeader(fset, f, src)
		if err := writeGoFile(srcPath, prefix, srcImports, remain); err != nil {
			return err
		}
	}
	fmt.Printf("moved %d declarations into %d files; %s keeps %d\n", len(segTarget), len(targets), srcName, len(remain))
	return nil
}

// splitHeader returns everything before the first import declaration
// (build tags, file doc, package clause) and the offset after the header.
func splitHeader(fset *token.FileSet, f *ast.File, src []byte) ([]byte, int) {
	tf := fset.File(f.Pos())
	headerEnd := lineEnd(src, tf.Offset(f.Name.End()))
	prefixEnd := headerEnd
	first := true
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.IMPORT {
			continue
		}
		if first {
			p := g.Pos()
			if g.Doc != nil {
				p = g.Doc.Pos()
			}
			prefixEnd = tf.Offset(p)
			first = false
		}
		headerEnd = lineEnd(src, tf.Offset(g.End()))
	}
	return bytes.TrimRight(src[:prefixEnd], "\n"), headerEnd
}
