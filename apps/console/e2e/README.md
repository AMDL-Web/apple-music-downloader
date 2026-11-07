# Console browser tests

Playwright tests of the web console's main paths, run in Chromium against
the real embedded console. This directory has its own `package.json` and
lockfile; nothing else in the repository depends on Node packages.

## What runs

`playwright.config.mjs` starts `server/` as its web server: a small Go
program that mounts the embedded console (`apps/console`) under `/console/`
exactly as `amdl serve` does, and answers `/api/v1` from the in-memory demo
backend (`internal/demo`), the one the desktop and terminal demos use. The
real server is not used because even its simulate mode reads Apple's
catalog. The config builds the server with `go build -cover` on every run and
starts it on a free port.

- Every test has a backend of its own: the `e2e-world` cookie names it and a
  new name starts a fresh one, so tests share no state and run in parallel.
- The backend's clock starts at 2026-03-14 15:00 UTC and stands still; the
  browser's clock is fixed to the same instant. Jobs move only when a test
  calls `world.step(ms)` (`POST /e2e/step`), so progress is deterministic.
- Requests to any other host are aborted: nothing reaches Apple.

| File | Covers |
|---|---|
| `tests/shell.spec.mjs` | overview, primary navigation, library, command palette, phone tab bar |
| `tests/download.spec.mjs` | search and start a download, a job running to done in Activity, the job drawer |
| `tests/settings.spec.mjs` | settings edit and save, hooks add and edit, account log out and log in |
| `tests/a11y.spec.mjs` | axe-core on the main pages in both themes; serious or critical violations fail |
| `tests/screens.spec.mjs` | screenshot comparisons (see below) |

Locators are roles and text only (`getByRole`, `getByText`), as a person or a
screen reader finds things; a change that breaks one is usually one a user
would notice too.

## Running

Needs Go (as for the rest of the repository) and Node.js.

```sh
cd apps/console/e2e
npm ci
npx playwright install --with-deps chromium   # --with-deps needs root and apt; drop it elsewhere
npm test                                      # everything but the screenshots
npm test -- tests/settings.spec.mjs -g hook   # one file, matching tests
npx playwright show-report out/report         # the HTML report of the last run
```

`GO=/path/to/go npm test` picks another Go toolchain. Results, traces of
failed tests and the report go to `out/` (ignored by git).

## Screenshots

`tests/screens.spec.mjs` compares four pages at 1280 px (Overview light and
dark, Activity dark, Settings > Quality light) and Activity at 390 px with the
baselines in `tests/__screenshots__/`. Text rendering differs between
machines, so the baselines are made, and compared, only in the Playwright
Linux image (`mcr.microsoft.com/playwright:v<version>-noble`), which CI runs
the suite in. `npm test` skips them; to run them, or to update the baselines
after an intended change, use Docker:

```sh
npm run test:docker       # the whole suite in the image, screenshots included
npm run test:update       # the same, rewriting the baselines; review and commit the PNGs
./docker.sh tests/screens.spec.mjs -g overview --update-snapshots   # just some
```

`docker.sh` builds the server on the host (the image has no Go) and runs
Playwright in the image with the repository mounted. A failed comparison
leaves the expected, actual and diff images under `out/test-results/` and
in the HTML report. Thresholds: up to 1% of pixels may differ, each by a
per-pixel colour threshold of 0.2; animations are disabled and the caret
hidden.

## Server coverage

The server is built with `-cover` and runs with `GOCOVERDIR=out/gocover`.
It writes its counters when the run ends (Playwright stops it with SIGTERM),
so after `npm test`:

```sh
go tool covdata percent -i=out/gocover -pkg=amdl/apps/console,amdl/internal/demo
go tool covdata textfmt -i=out/gocover -o out/e2e.cov   # for go tool cover -html=out/e2e.cov
```

Each run starts `out/gocover` afresh.

## Known gaps

- The demo backend serves no album record (`GET /api/v1/library/albums/{id}`),
  so an album row in Activity opens a "Not in the library" drawer; the drawer
  test opens the job from the Overview instead.
- The demo backend does not filter the Apple library, so the Library search
  box is not tested.
- `tests/a11y.spec.mjs` lists the serious violations the console had when the
  suite was written (`KNOWN`); fix them and delete their entries.
