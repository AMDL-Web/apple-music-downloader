---
name: amdl-refactor
description: Split a large Go file in amdl-backend, or move declarations between files of one package, as a provably move-only change with tools/splitdecls (list, plan, move, verify production and test files, build, vet, race, one signed-off commit per package, recorded in .git-blame-ignore-revs). Use when a change touches a source file over about 20 KB, or when asked to "split this file", "reorganize the package" or "move these functions".
---

# Move-only refactor

The split is its own commit with no code change, so reviewers and `git blame` can
skip it. `tools/splitdecls` copies declarations byte for byte and proves afterwards
that only their location changed. Behavior changes go in a separate commit.

Pick one responsibility per new file, named for it, and keep the original file as
the entry point for the rest. If there is no natural seam, don't split; say so.

```bash
go run ./tools/splitdecls list -file internal/api/server.go
```

prints each top-level declaration key: a function name, `Recv.Method`, or any name
of a type, var or const group (the whole group moves). Write a plan in the session
scratchpad mapping target files to keys, for example
`{"httpjson.go": ["writeJSON", "Server.health"]}`. Comments directly above a
declaration move with it; test files get their own plan (`-src server_test.go`).

```bash
go run ./tools/splitdecls move -dir internal/api -src server.go -plan <scratchpad>/plan.json
go run ./tools/splitdecls verify -dir internal/api -base HEAD           # production files
go run ./tools/splitdecls verify -dir internal/api -base HEAD -tests    # test files
gofmt -l internal/api
go build ./... && go vet ./internal/api/ && go test -race -count=1 ./internal/api/
```

`move` creates or appends to targets and rebuilds the touched import blocks. Both
`verify` runs must pass; a failure means something other than a move happened, so
undo it rather than explain it away. `gofmt -l` prints nothing.

Commit one package at a time, signed off and marked move-only:
`git commit -s -m "refactor(api): split server.go by resource (move-only)"`. After
it lands on `dev`, add its full sha with a one-line comment to
`.git-blame-ignore-revs` (create the file if missing) in a follow-up commit. Docs
should point at `package#Symbol`, which survives the split;
`go test -count=1 ./internal/repocheck/` finds stale pointers.
