---
name: amdl-accept
description: Acceptance of an amdl change before it is installed - for records, queue, downloads, API, DB, config or wrapper changes a verifier in its own linked session, which did not write the change, writes its criteria before reading the diff, checks the real app build on real-scale data against the acceptance rows for its change type (entry points x surfaces, state combinations, filmed motion, ui-audit, real runs over an existing install), returns PASS/FAIL with evidence, and records an accepted build with accept.sh; look and motion changes are checked by their author with tools/lookround/round.sh, which films a fixed scene set and installs. Only an accepted build counts as done or is installed for the maintainer. Use when a checklist item is ready, when dispatching or acting as the verifier, and before telling the maintainer something user-facing is finished.
---

# Accept a change

The author's own checks ([amdl-verify](../amdl-verify/SKILL.md)) prove the code does
what the author meant. Acceptance proves it does what the maintainer asked, on every
surface the maintainer will touch.

## Which changes need a verifier

- **Records, queue, library state, downloads, API, DB, config, wrapper, upgrades:**
  a verifier in another session, as below.
- **Look and motion only** (layout, copy, colour, radius, a transition, a control
  wired to an existing action): no verifier. The author commits the round and runs,
  from the Mac:

  ```bash
  tools/lookround/round.sh --scene head-complete/light       # one scene, while fixing
  tools/lookround/round.sh                                   # check HEAD
  tools/lookround/round.sh --install --session <your session> # then install it
  ```

  It films the scene set (`apps/desktop/internal/ui#TestScenes`: the album page in
  every head state, search, queue and artist to album and back, a second album
  right after a first, every tab switch, the first search) at the maintainer's
  window size in light and dark, opening and going back, the queue hero's title
  wrapping as a long one does; it tracks what each part of the window shows frame
  by frame and flags a part that moves after it settled or takes a one-frame step
  out of line (a snap, a jump), a pop, flicker, blank frame, a text gone for a
  frame or the previous page's content (`apps/desktop/internal/filmcheck`), runs
  the tests of the packages the round touched, and leaves
  `dist/review/look-<sha>/sheet.jpg` and `summary.txt`, plus `findings.html` when
  there is any finding: every finding, new or known, with the frames before, at and
  after it to flip through, shown to the maintainer as this page, published as an
  Artifact by the session that ran the round. The author opens the sheet
  and the settled frames of the screens it changed, then reruns with `--install`,
  which reuses the passed check, records the build with `accept.sh` (the summary
  as criteria) and installs it. A finding the author judges wrong is waived with `--waive "<why>"`, which the
  record keeps. A screen or transition the set lacks gets a scene in
  `scenes_test.go`, not a throwaway film. The whole desktop suite starts in the
  background on the build host and runs on CI; a failure there is fixed in the
  next round.

A round that mixes both goes to a verifier, scoped to the non-look commits plus the
smoke pass.

## Who

- A verifier that did not write the change: a separate linked session started in
  fresh context, never a subagent of the author or a session forked from it. One
  verifier, not a panel.
- It gets the checklist item only: the maintainer's words, the restatement, the
  surfaces, the acceptance rows, and the commit. Not the author's report.
- Sonnet at high effort by default; Opus for motion and record/queue state changes.
- The verifier may read code to find entry points but changes none. It reports gaps;
  the author fixes them; the verifier runs the failed rows again on the new build.

## Order

1. Write the criteria first, from the item alone: what the maintainer should see,
   on which surfaces, in which states. Only then read the diff, and only to find
   entry points; a criterion is never dropped because the diff makes it look safe.
2. Build the real app from the commit with the development machine's fast loop
   (agent memory). Demo data, a mock backend or a dev server stand in for nothing
   the maintainer touches.
3. Run it on real-scale data: a library and queue the size of the maintainer's,
   with long titles and missing artwork, started from a copy of an existing install.
4. Judge UI on frames, not on code or a single screenshot. For a motion or flicker
   complaint that came with the maintainer's recording, extract the frames they
   point at first and reproduce that in a film before checking the fix.
5. Record the verdict (Evidence below). On PASS, run `accept.sh` on the development
   machine for that exact build; its installer refuses any binary without the
   record. A FAIL records nothing.

## Rows by change type

Every row that matches the change applies. The checklist item lists them when it is
written, so the verifier does not choose its own scope.

Acceptance covers the round being installed, not the whole app: the rows for what
its commits touch, plus one smoke pass at the maintainer's window size in light and
dark (open an album from search and from the queue, switch every tab, first search)
looking only for a pop, flash, blank or vanishing frame: `tools/lookround/round.sh
<commit>` is that pass. Areas the round did not
touch are covered by the full test suite, not by re-running their rows; a verifier
reused from an earlier item drops that item's rows.

### Records, runs, queue, library state

Each entry point of the action, against every surface that shows the thing:

| Entry points | Surfaces that must agree afterwards |
| --- | --- |
| album page, run page, queue row menu, search card, Library, console drawer, menu and bulk select, CLI, MCP | queue rows and tab counts, hero and Recently finished, sidebar counts, album page and search marks, Library, console Activity and Overview |

Then the state combinations the change can meet, at least: one track first then
its album, the album then one track, a record deleted then downloaded again, the
overwrite setting on and off, a file deleted on disk, zero accounts and several,
an install upgraded from the previous release. A combination left out is named in
the report.

### Motion and view transitions (desktop)

The scene set (`tools/lookround/round.sh`) first; a transition it does not cover
gets a scene in `apps/desktop/internal/ui/scenes_test.go`. Frames filmed some other
way are checked with `go run ./apps/desktop/tools/framecheck film <dir of PNGs>`.
What a scene cannot assert goes in a film test like
`apps/desktop/internal/ui/flight_film_test.go`: 60 Hz frames, light and dark themes,
motion on, started from each entry (search, queue, artist page), with long and
scrolled data, asserting on what the eye sees:

- the hand-over frame (shadow, badges, text) changes no more than the frames before;
- other targets take a click as soon as the motion looks finished;
- nothing is drawn over the page it came from after the motion ends;
- each element's position, tracked frame by frame, never moves again once it has
  settled and never takes a single-frame step out of line with its neighbours. A
  "last frame equals the still page" check alone misses a settle-then-snap.

Both directions of every transition are filmed; a direction not filmed is reported
as not checked.

A reported motion bug is reproduced in such a film before it is patched, in the
state the maintainer's recording shows
([Motion and look](../amdl-coordinate/SKILL.md#motion-and-look), step 3). Acceptance
of motion checks regressions only: a pop, a flash, a blank or vanishing frame. It
does not judge timing, order or taste, which are the maintainer's
([desktop-motion](../../rules/desktop-motion.md)).

### Web UI (console, site)

[ui-audit](../../../apps/console/tools/ui-audit/README.md) over every page and state,
widths 1440 to 320, light and dark. The verifier opens and looks at the
screenshots; a finding counts only when its screenshot was looked at. One flaw on one
screen means checking the same flaw on every screen. Order: layout that jumps during
an action, misalignment, then polish.

### Real runtime (guest VM, wrapper login, deploy, upgrade)

One real run that exercises the change. A fake QEMU, fake manager or mock backend
does not count. A build meant for the maintainer starts first on a copy of the
maintainer's app data directory, rewritten so nothing points back at the live install,
and reaches wrapper ready there:

- `server/config.yaml`: `database.path`, `downloads_dir` and `temp_dir` are absolute
  paths into the copy (`grep -c` for the home directory finds none).
- The guest port differs from the live one in `wrapper/installed.json`,
  `wrapper/disk.json`, `wrapper/state.json` and `config.yaml`, and `pid` is dropped from
  `state.json`.
- The database is copied with `sqlite3 .backup` into an empty `db/`, never next to
  copied `-wal` or `-shm` files.
- After start, `lsof -Fn -p <pid>` shows the copy's database path.

`make guestboot` and `make upgradecheck`
apply when the change touches their paths.

### API, SSE, config contract

The field or route against `internal/api/openapi.yaml`, then every client that reads
it (console, desktop, CLI, MCP) showing the new value. A route change follows
[amdl-api-change](../amdl-api-change/SKILL.md).

### The first three tasks

For anything a person uses: start a download through the interface, watch its
progress, find the result. Through the UI, not the API.

## Where

Iteration and acceptance run on the development machine named in agent memory,
using its fast build and install loop. GitHub CI still runs on every push to `dev`
and stays the gate for merges; acceptance does not wait for it, and does not replace
it.

## Evidence

Each row is PASS, FAIL or SKIPPED, with an artifact a reader can open: the
entry-point by surface grid filled in, film frame sheets, the ui-audit report path,
the real-run log, command output. A subtle visual change (a detail the maintainer
would have to hunt for) also gets a side-by-side before/after recording of the same
scripted interaction, before on the left; an obvious one needs only screenshots.
Copy the video into the worktree's ignored `dist/review/` before sending it. SKIPPED carries its reason and keeps the
item from counting as done unless the maintainer accepts the gap.

```
Accepted <item> at <short sha>: PASS

| Row | Result | Evidence |
| --- | --- | --- |
| delete from queue row x 8 surfaces | PASS | grid: dist/review/<item>/surfaces.md |
| one track, then album | FAIL | album shows 1/12 complete after the second job; log dist/review/<item>/combo-1.txt |
| film: search -> album, light | PASS | frames dist/review/<item>/film-light.png |

Not covered: <each gap and why>.
```

The verifier writes the verdict into the checklist item. Only a PASS with no
unaccepted gaps marks it done, and only a build with an `accept.sh` record is installed.

## Regressions

When the maintainer reports a regression, first reinstall the last accepted build
(its record and binary are kept with the acceptance records), then root-cause, then
fix. A fix goes through acceptance like any change; it is never stacked on an
unaccepted one.
