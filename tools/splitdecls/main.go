// Command splitdecls moves top-level Go declarations between files of one
// package and proves afterwards that the package's declarations did not
// change. It exists so a large file can be split by responsibility as a
// reviewable no-op: moved code is copied byte for byte, never re-typed.
//
//	go run ./tools/splitdecls list   -file internal/api/server.go
//	go run ./tools/splitdecls move   -dir internal/api -src server.go -plan plan.json
//	go run ./tools/splitdecls verify -dir internal/api -base HEAD [-tests]
//
// A plan maps target file names to declaration keys:
//
//	{"httpjson.go": ["writeJSON", "Server.health", "maxJSONBodyBytes"]}
//
// A key is a function name, "Recv.Method" for a method (receiver type name
// without the pointer), or any name declared by a type/var/const group; the
// whole group moves. Comments directly above a declaration travel with it.
// Missing target files are created, existing ones are appended to, and the
// import blocks of every touched file are rebuilt and pruned.
//
// verify compares every top-level declaration, printed without comments, at
// the given git revision with the working tree, plus the multiset of comment
// texts. Production and _test.go files are compared separately (-tests).
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "list":
		err = runList(os.Args[2:])
	case "move":
		err = runMove(os.Args[2:])
	case "verify":
		err = runVerify(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "splitdecls:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  splitdecls list   -file <file.go>
  splitdecls move   -dir <pkgdir> -src <file.go> -plan <plan.json>
  splitdecls verify -dir <pkgdir> -base <git-rev> [-tests]`)
	os.Exit(2)
}
