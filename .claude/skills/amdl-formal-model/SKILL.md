---
name: amdl-formal-model
description: Hand-write a small TLA+ model of one amdl protocol and let TLC explore every interleaving, for bugs that look like a race (double processing, state going backwards, wrong order, a retry writing twice, "fixes itself later") or after changing a protocol with several actors (jobs, requeue, token refresh, feed resume, library sync). Use when a concurrency bug will not reproduce reliably, or when asked to "model this", "check the interleavings" or "run TLC".
---

# Model a protocol with TLA+

When a bug looks like an interleaving, or after a change to a protocol with several
actors, write a small TLA+ model of just that protocol, from the code. TLC explores
every interleaving and prints a counterexample trace when an invariant breaks.

The model checks the design, not the code. A counterexample counts as a bug only
once a test drives that interleaving in the real code and fails on it.

## Modelling rules

- 2 or 3 actors and tiny bounds, so TLC finishes in seconds to minutes.
- Atomicity follows the code: one step per critical section under one mutex, per SQL
  transaction, per HTTP call, per channel operation, per file write or rename.
- The header names each modelled `path#Symbol` and the commit it was read at.
- An invariant is the negation of a symptom. Mark whether it comes from a code
  comment or doc, or was inferred.
- Add `CONSTANT MUT`. `none` must pass; each other value removes one real guard and
  must fail. A mutant that passes means another guard protects it: name that guard.

## First-draft mistakes

Seen in every model written so far: an invariant stronger than what the
code promises, or one atomic step split into two. Before calling a counterexample a
bug, check each of its steps against the code at file and line.

## Running TLC

On the test host, a portable JRE and tla2tools sit in the tla directory under
amdl-dev in the home directory:

```
~/amdl-dev/tla/tlc.sh <dir> <Module> <cfg>
~/amdl-dev/tla/runall.sh <dir>
```

## Where models live

Outside the repository, in the maintainer's notes, not in git: keeping them in step
with the code is not worth it permanently. Existing models: the job lifecycle
(`internal/jobs`), requeue, media-user-token refresh (`internal/usertoken`), and the
downloads feed resume and database swap (`internal/client` follow.go, and the
console's stream.js). Next candidates: the Apple library sync and the library sync
monitor (`internal/librarysync`).

## Not for

Single-request logic, and CI or scheduled checks.

## Report

Which part was modelled, how many invariants, the TLC result, and whether any
counterexample reproduced in real code.
