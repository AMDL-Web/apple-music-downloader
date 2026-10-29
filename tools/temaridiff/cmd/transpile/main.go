// Command transpile regenerates internal/fairplay/rounds_gen.go from the
// Temari version pinned in tools/temaridiff/go.mod. Run it from
// tools/temaridiff:
//
//	go run ./cmd/transpile -o ../../internal/fairplay/rounds_gen.go
package main

import (
	"flag"
	"fmt"
	"os"

	"amdl/tools/temaridiff/internal/transpile"
)

func main() {
	fs := flag.NewFlagSet("transpile", flag.ContinueOnError)
	out := fs.String("o", "", "output file (required)")
	if err := fs.Parse(os.Args[1:]); err != nil || *out == "" || fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: transpile -o <rounds_gen.go>")
		os.Exit(2)
	}
	src, version, err := transpile.Source()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code, err := transpile.File(src, version)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, code, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
