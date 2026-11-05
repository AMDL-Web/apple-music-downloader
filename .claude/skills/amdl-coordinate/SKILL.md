---
name: amdl-coordinate
description: Run the amdl coordinator session - turn each batch of the maintainer's requests into checklist items the maintainer reviews, dispatch one writer per area (motion and look complaints are fixed directly with the maintainer), send finished items to independent acceptance (amdl-accept), keep decisions and status in the checklist rather than in context, run the testing round after a release candidate, and hold the release freeze. Use when acting as or starting the coordinator, when the maintainer sends a batch of requests, asks for progress, or a fresh coordinator takes over.
---

# Coordinate

The coordinator holds the checklist and the decisions. It does not write product
code: every change except motion and look goes to a writer, and every finished item to a verifier
([amdl-accept](../amdl-accept/SKILL.md)). Motion and look are the exception
([Motion and look](#motion-and-look)). Because its state lives in the checklist, a
fresh coordinator can take over at any time. A session ends when its feature is
done, not because it was compacted: it writes its handoff file and starts the next
session itself; the maintainer is not asked to switch. After a compaction a session
re-reads the rules and memories that govern its current work, then carries on.

## The checklist

One shared checklist (an Artifact with a database; its link is in agent memory) is
the only status board. Each item:

| Field | Holds |
| --- | --- |
| title | a short name for the item |
| version | the release it belongs to (`2.0`, `2.0.1`), or `流程` for process items |
| words | the maintainer's message, verbatim |
| words_at | when the maintainer said it |
| reading | the coordinator's one- or two-sentence restatement |
| kind | add, change, or remove/replace |
| surfaces | API, console, desktop, TUI, CLI, MCP, site: each one the item touches |
| rows | the acceptance rows from amdl-accept that apply |
| owner | the writer session or agent |
| status | todo, doing, accepting, done, blocked |
| decision | what was decided and why, with the maintainer's words when they decided |
| evidence | the verifier's report |
| commits, order, updated | the commits that landed it, its place in the queue, last change |

A `meta/board` document holds the board's phase, the coordinator session and when
it was last updated.

- A batch becomes items before any work starts. The maintainer reviews the items,
  not the code; a wrong reading is fixed there.
- A remove/replace item waits for the maintainer's yes. A new option goes next to
  the old one unless the maintainer says to drop the old.
- Copy, a single style value or a doc fix may skip the checklist.
- After a compaction or at the start of a session, read the checklist before
  answering anything. "What are you working on" and "progress" are answered from it.
- A withdrawn request is marked withdrawn in its item and removed from every brief.

## Dispatch

- One writer per area at a time (core, console, desktop, CLI/TUI/MCP, site, wrapper).
- An item that spans surfaces goes to one writer who owns all of its surfaces, so
  the concept changes once, not once per client.
- The brief carries the item (words, reading, surfaces, rows), the standing rules
  in `HUMAN.md`, and what the writer must not touch. The writer finishes with
  [amdl-verify](../amdl-verify/SKILL.md), then sets the item to accepting.
- The verifier is a separate linked session in fresh context, dispatched with the
  item only, not the writer's report ([amdl-accept](../amdl-accept/SKILL.md#who)).
- Nothing is installed on the maintainer's machine or shown to them until that
  build has an `accept.sh` record ([amdl-accept](../amdl-accept/SKILL.md#which-changes-need-a-verifier)),
  and it carries the whole round. Only `install-mac.sh`
  installing an accepted build opens AMDL.app on the maintainer's Mac; nothing
  else, including a research brief or a test, opens a window there. Windows on the
  build host are fine.
- A message to another session counts as sent only after its transcript shows it.

## Motion and look

A motion or look complaint is not a checklist item and is not relayed to a writer.
The session the maintainer is talking to handles it:

1. Measure the recording at its native frame rate, never downsampled or shrunk into
   a contact sheet: track the position of the element they name, and of its
   neighbours, frame by frame, and find the frame where it breaks (a step, a move
   after it had settled, a jump). Restate it in their words plus the measurement
   ("`2014)` settles at 0.43 s, then drops 5 pt at 0.62 s"). If nothing in the
   numbers matches, ask which frame; never report a guessed defect. From the Mac
   (ffmpeg and Go, nothing else):

   ```bash
   go run ./apps/desktop/tools/framecheck rec -out dist/review/rec <movie.mov> 260x110+1060+440
   ```

   The region is `<W>x<H>+<X>+<Y>` in the recording's pixels (Retina, twice the
   points). It prints, for every frame the recording keeps, its time and the
   bounds of the ink in the region (`-threshold 60` levels dark; `-light` for
   light text), then each snap (moved after resting two frames or more) and jump
   (one step over three times its neighbours), and writes the frames around each
   at full size as `step-<frame>.png`. The scene set runs the same check
   (`filmcheck.Steps`) on every part of the window.
2. Fix it.
3. Film the same interaction on the new build, both directions, at their window
   size with a title that wraps like theirs, and run the same measurement:
   nothing moves after it settles, no single-frame step, the end frame equals the
   still page. Report only the directions and entries that were filmed, with the
   before and after numbers. `tools/lookround/round.sh` films the scene set this
   way; a transition it lacks gets a scene in
   `apps/desktop/internal/ui/scenes_test.go`.
4. Install and let them judge and tune. No verifier session; timing and taste are
   theirs.

Before dispatching anything visual, ask whether the maintainer complained about it;
if not, report the finding and leave the motion alone
([desktop-motion](../../rules/desktop-motion.md)).

## After a release candidate

Once a candidate is published, testers use amdl the way a real user would, record
what is awkward, and the owning sessions fix it; then the testers go again.

- Three roles: a new user installing the binary, an agent user on the CLI and MCP,
  and a daily console user. The 1.x upgrade is not one of them unless asked.
- Testers only record friction. Each fix goes to the session that owns the area.
- Real downloads use the logged-in wl-lite on the test host and stay modest; heavy
  loops use simulate mode. Never log wl-lite out.
- No windows on the maintainer's Mac: browser checks run headless or on the test
  host.
- The final release stays the maintainer's call.

## Release freeze

From the first release candidate of a version until its release, only fixes and
acceptance work land. A new direction becomes an item for the next version. The
coordinator cuts release candidates when the open items are done; the final release
waits for the maintainer.

## Reporting

When the maintainer returns: what moved to done (with the verifier's verdict), what
is blocked and on what, and the items waiting for the maintainer. Nothing is called
done that the checklist does not show as accepted.
