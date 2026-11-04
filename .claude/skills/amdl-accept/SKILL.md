---
name: amdl-accept
description: Independent acceptance of an amdl change - a verifier that did not write it checks one checklist item against the acceptance rows for its change type (entry points x surfaces, state combinations, filmed motion, ui-audit, real runs over an existing install) and returns PASS/FAIL with evidence. Only an accepted item counts as done. Use when a checklist item is ready, when dispatching or acting as the verifier, and before telling the maintainer something user-facing is finished.
---

# Accept a change

The author's own checks ([amdl-verify](../amdl-verify/SKILL.md)) prove the code does
what the author meant. Acceptance proves it does what the maintainer asked, on every
surface the maintainer will touch. It is a separate step run by someone else.

## Who

- A verifier that did not write the change: a fresh subagent or linked session with
  no access to the author's reasoning. One verifier, not a panel.
- It gets the checklist item only: the maintainer's words, the restatement, the
  surfaces, the acceptance rows, and the commit or build. Not the author's report.
- Sonnet at high effort by default; Opus for motion and record/queue state changes.
- The verifier may read code to find entry points but changes none. It reports gaps;
  the author fixes them; the verifier runs the failed rows again.

Copy, a single style value or a doc fix needs no acceptance: the author's checks are
enough.

## Rows by change type

Every row that matches the change applies. The checklist item lists them when it is
written, so the verifier does not choose its own scope.

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

A film test like `apps/desktop/internal/ui/flight_film_test.go`: 60 Hz frames, light
and dark themes, motion on, started from each entry (search, queue, artist page),
with long and scrolled data. It asserts on what the eye sees:

- the hand-over frame (shadow, badges, text) changes no more than the frames before;
- other targets take a click as soon as the motion looks finished;
- going in and going out take comparable time;
- nothing is drawn over the page it came from after the motion ends.

A reported motion bug is reproduced in such a film before it is patched.

### Web UI (console, site)

[ui-audit](../../../apps/console/tools/ui-audit/README.md) over every page and state,
widths 1440 to 320, both skins, light and dark. The verifier opens and looks at the
screenshots; a finding counts only when its screenshot was looked at. One flaw on one
screen means checking the same flaw on every screen. Order: layout that jumps during
an action, misalignment, then polish.

### Real runtime (guest VM, wrapper login, deploy, upgrade)

One real run that exercises the change. A fake QEMU, fake manager or mock backend
does not count. A build meant for the maintainer starts first on a copy of the
maintainer's app data directory, with absolute paths in its `config.yaml` rewritten
to the copy, and reaches wrapper ready there. `make guestboot` and `make upgradecheck`
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
the real-run log, command output. A subtle visual change also gets a side-by-side
before/after recording for the maintainer. SKIPPED carries its reason and keeps the
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
unaccepted gaps marks it done.
