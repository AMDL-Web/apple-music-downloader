---
name: amdl-coordinate
description: Run the amdl coordinator session - turn each batch of the maintainer's requests into checklist items the maintainer reviews, dispatch one writer per area, send finished items to independent acceptance (amdl-accept), keep decisions and status in the checklist rather than in context, and hold the release freeze. Use when acting as or starting the coordinator, when the maintainer sends a batch of requests, asks for progress, or a fresh coordinator takes over.
---

# Coordinate

The coordinator holds the checklist and the decisions. It does not write product
code: every change goes to a writer, and every finished item to a verifier
([amdl-accept](../amdl-accept/SKILL.md)). Because its state lives in the checklist,
a fresh coordinator can take over at any time; start one instead of working on
through repeated compactions.

## The checklist

One shared checklist (an Artifact with a database; its link is in agent memory) is
the only status board. Each item:

| Field | Holds |
| --- | --- |
| words | the maintainer's message, verbatim |
| reading | the coordinator's one- or two-sentence restatement |
| kind | add, change, or remove/replace |
| surfaces | API, console, desktop, TUI, CLI, MCP, site: each one the item touches |
| rows | the acceptance rows from amdl-accept that apply |
| owner | the writer session or agent |
| status | todo, doing, accepting, done, blocked |
| decision | what was decided and why, with the maintainer's words when they decided |
| evidence | the verifier's report |

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
- The verifier is dispatched with the item only, not the writer's report.
- A message to another session counts as sent only after its transcript shows it.

## Release freeze

From the first release candidate of a version until its release, only fixes and
acceptance work land. A new direction becomes an item for the next version. The
coordinator cuts release candidates when the open items are done; the final release
waits for the maintainer.

## Reporting

When the maintainer returns: what moved to done (with the verifier's verdict), what
is blocked and on what, and the items waiting for the maintainer. Nothing is called
done that the checklist does not show as accepted.
