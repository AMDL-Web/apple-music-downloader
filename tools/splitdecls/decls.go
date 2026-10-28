package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// segment is the byte range of one top-level declaration. It starts where
// the previous declaration's range ended, so free comments between two
// declarations travel with the one below them.
type segment struct {
	keys       []string
	start, end int
}

func declKeys(d ast.Decl) []string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return []string{recvName(d.Recv.List[0].Type) + "." + d.Name.Name}
		}
		return []string{d.Name.Name}
	case *ast.GenDecl:
		if d.Tok == token.IMPORT {
			return nil
		}
		var keys []string
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				keys = append(keys, s.Name.Name)
			case *ast.ValueSpec:
				for _, n := range s.Names {
					keys = append(keys, n.Name)
				}
			}
		}
		return keys
	}
	return nil
}

func recvName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return recvName(t.X)
	case *ast.IndexExpr:
		return recvName(t.X)
	case *ast.IndexListExpr:
		return recvName(t.X)
	case *ast.Ident:
		return t.Name
	}
	return "?"
}

func lineEnd(src []byte, off int) int {
	for off < len(src) && src[off] != '\n' {
		off++
	}
	if off < len(src) {
		off++
	}
	return off
}

// segments returns the declaration ranges of f and the offset where the
// header (package clause, file doc, imports) ends.
func segments(fset *token.FileSet, f *ast.File, src []byte) ([]segment, int) {
	tf := fset.File(f.Pos())
	headerEnd := lineEnd(src, tf.Offset(f.Name.End()))
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			headerEnd = lineEnd(src, tf.Offset(g.End()))
		}
	}
	prevEnd := headerEnd
	var segs []segment
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		end := lineEnd(src, tf.Offset(d.End()))
		segs = append(segs, segment{keys: declKeys(d), start: prevEnd, end: end})
		prevEnd = end
	}
	return segs, headerEnd
}

func parseFile(path string) (*token.FileSet, *ast.File, []byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return nil, nil, nil, err
	}
	return fset, f, src, nil
}

func runList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	file := fs.String("file", "", "Go source file")
	_ = fs.Parse(args)
	if *file == "" {
		usage()
	}
	fset, f, src, err := parseFile(*file)
	if err != nil {
		return err
	}
	segs, _ := segments(fset, f, src)
	tf := fset.File(f.Pos())
	for _, s := range segs {
		last := max(s.end-1, s.start)
		fmt.Printf("%5d-%5d %6dB  %s\n", tf.Line(tf.Pos(s.start)), tf.Line(tf.Pos(last)), s.end-s.start, strings.Join(s.keys, ","))
	}
	return nil
}
