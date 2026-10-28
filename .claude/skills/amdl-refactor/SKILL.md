---
name: amdl-refactor
description: Split a large Go file in amdl-backend, or move declarations between files of one package, as a provably move-only change with tools/splitdecls (list -> plan -> move -> verify production and tests -> build, vet, race -> one signed-off commit per package, recorded in .git-blame-ignore-revs). Use when a change touches a source file over about 20 KB, or when asked to "split this file", "break up server.go", "reorganize the package" or "move these functions".
---

# Move-only refactor

AGENTS.md asks you to split one responsibility out of any source file over about
20 KB that you touch. Do it as its own commit that changes no code at all, so
reviewers and `git blame` can skip it. `tools/splitdecls` copies declarations byte
for byte and proves afterwards that nothing but their location changed.

```
- [ ] 1 Pick the seam
- [ ] 2 Plan
- [ ] 3 Move
- [ ] 4 Verify move-only
- [ ] 5 Build, vet, race
- [ ] 6 Commit and record
```

## 1. Pick the seam

One responsibility per new file, named for it (`downloads.go`, `job_events.go`,
`schema.go`). Keep the original file as the entry point for what remains. If no
natural seam exists, don't split; say so in the report. Behavior changes never ride
along: do them in a separate commit before or after.

## 2. Plan

```bash
go run ./tools/splitdecls list -file internal/api/server.go
```

It prints every top-level declaration key: a function name, `Recv.Method` for a
method, or any name of a type/var/const group (the whole group moves). Write a
plan in the session scratchpad mapping target files to keys:

```json
{"httpjson.go": ["writeJSON", "Server.health", "maxJSONBodyBytes"]}
```

Comments directly above a declaration travel with it. Test files are split the same
way with their own plan (`-src server_test.go`).

## 3. Move

```bash
go run ./tools/splitdecls move -dir internal/api -src server.go -plan <scratchpad>/plan.json
```

Missing targets are created, existing ones appended to, and import blocks of every
touched file rebuilt and pruned.

## 4. Verify move-only

```bash
go run ./tools/splitdecls verify -dir internal/api -base HEAD           # production files
go run ./tools/splitdecls verify -dir internal/api -base HEAD -tests    # _test.go files
```

Done when both pass: every top-level declaration, printed without comments, is
identical to the base revision, and the multiset of comment texts is unchanged.
A failure means something other than a move happened; undo it rather than
explaining it away.

## 5. Build, vet, race

```bash
gofmt -l internal/api
go build ./... && go vet ./internal/api/ && go test -race -count=1 ./internal/api/
```

Done when `gofmt -l` prints nothing and the rest exits 0.

## 6. Commit and record

- One commit per package, signed off, subject marked move-only:
  `git commit -s -m "refactor(api): split server.go by resource (move-only)"`.
- After the commit lands on `dev`, add its full sha to `.git-blame-ignore-revs`
  (create the file if missing) with a one-line comment naming the split, in a
  follow-up signed-off commit. Locally, `git config blame.ignoreRevsFile .git-blame-ignore-revs`
  makes `git blame` skip it; GitHub reads the file automatically.
- Update docs that named the old file: pointers should name the package and symbol
  (`internal/api#Server.Routes`), which survive splits; `go test -count=1 ./internal/repocheck/`
  catches stale ones.
- Report: files created, the verify output for both runs, and the test results.
