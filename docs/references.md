# References

> Type: reference

Outside practices that shaped how this repository is worked on: what was taken,
where it landed, and what was declined with the reason, so a declined idea is not
proposed again without something new. Add a row in the same commit that adopts
or rejects a practice.

Status: **adopted** (taken as is), **adapted** (taken with changes for this repo),
**not adopting** (declined; the reason says what would change that).

## Agent workflow (from lyjwpage)

The maintainer's site repository lyjwpage runs a layered agent setup: a short
always-loaded AGENTS.md, path-scoped rules, skills for procedures, and hooks and
checks for anything that must always happen. This repository took the parts that
fit a Go open-source backend with English docs.

### Instruction files and docs

| Practice | Landed in | Status | Note |
| --- | --- | --- | --- |
| Byte-capped root AGENTS.md with only hard rules, done-conditions and pointers | [AGENTS.md](../AGENTS.md), `internal/repocheck` | adapted | Caps are 12000 B (root) and 5000 B (nested), checked in bytes. |
| CLAUDE.md is exactly `@AGENTS.md` next to every AGENTS.md | CLAUDE.md files, `internal/repocheck` | adopted | Replaced the root symlink, which breaks without `core.symlinks`. |
| Working agreements: decide reversible things, restate ambiguous goals, lightest fix first, search before "can't" | [AGENTS.md](../AGENTS.md#working-agreements) | adopted | English, one reason per rule. |
| "Rule -> enforced by" table, text-only rules marked | [AGENTS.md](../AGENTS.md#rule---enforced-by) | adopted | |
| One review rubric for all review bots, imported by AGENTS.md | [REVIEW.md](../REVIEW.md), review workflows | adapted | Review output stays in Simplified Chinese by the maintainer's choice. |
| Review convergence: fix real defects in one push, edge cases optional | [REVIEW.md](../REVIEW.md), [HUMAN.md](../HUMAN.md#review-bots) | adopted | |
| Path-scoped rules with `paths:` frontmatter | `.claude/rules/` | adopted | Codex and others read nested AGENTS.md instead; rules point there. |
| Nested AGENTS.md per package: invariants, traps, change-together lists, verify command | `internal/api/AGENTS.md` and five more | adopted | |
| Glossary with one line and a source pointer per term | [GLOSSARY.md](../GLOSSARY.md) | adopted | |
| Comment policy: surprising behavior, intent, contracts; no history | `.claude/rules/go.md` | adapted | Exported Go identifiers keep godoc comments. |
| Doc types with an enforced index | [docs/README.md](README.md), `internal/repocheck` | adopted | English `> Type:` markers. |
| Machine-checked links, anchors, backticked paths and `path#Symbol` pointers | `internal/repocheck` | adapted | A Go test instead of a TypeScript script; also accepts `package-dir#Symbol`, which survives file splits. |
| One fact, one source; docs updated in the same commit, found with `rg` | [AGENTS.md](../AGENTS.md#working-agreements), `.claude/rules/docs.md` | adopted | |
| Contract done-conditions; rename directly, no compatibility shims | [AGENTS.md](../AGENTS.md#contracts-and-breaking-changes), `internal/api/AGENTS.md` | adapted | Breaking changes allowed when they fix a defect or remove duplication, but marked `!` and listed in the upgrade guide, because installs and hand-mirrored clients must act on them. Output paths stay frozen without the maintainer's OK. |
| Maintainer-facing collaboration protocol | [HUMAN.md](../HUMAN.md) | adapted | Short; outside contributors read CONTRIBUTING.md. |
| README reading map from question to entry symbol | [README.md](../README.md#reading-map) | adopted | |
| Skill conventions and the `.agents/skills` link | `.claude/skills/`, `.agents/skills` | adopted | |
| Soft rule: split a touched file over about 20 KB | [AGENTS.md](../AGENTS.md#working-agreements), `tools/splitdecls` | adapted | Splits are move-only and proven with `tools/splitdecls` verify. |
| Bounded waits and outbound calls | [AGENTS.md](../AGENTS.md#working-agreements), `.claude/rules/go.md` | adopted | Context deadlines or client timeouts in Go. |
| Read official API docs first; developer.apple.com pages as Markdown via `.md` | `internal/applemusic/AGENTS.md` | adopted | |
| No timelines in current-state docs | none | not adopting | Existing pages keep their upgrade notes; revisit with a docs check if dates start to rot. |
| Out-of-repo facts with verification stamps | none | not adopting | Few such facts today; candidate when branch protection or app settings need recording. |
| Comment-only proof tool | none | not adopting | No comment sweeps planned; a `go/printer` comparison is the path if one is. |
| Memory mirror of the project memory | none | not adopting | Tied to Claude Projects; settled decisions belong in decision docs. |
| Rule-drift check before merge | none | not adopting | Few long-lived parallel branches. |

### Automation

| Practice | Landed in | Status | Note |
| --- | --- | --- | --- |
| CI runs on the branch agents push to | `.github/workflows/ci.yml` | adopted | Push to `main` and `dev`, per-SHA groups on pushes, cancel-in-progress on PRs. |
| One named gate shared by CI and local runs | `Makefile` | adapted | `make check` instead of a shell runner; CI calls each target as its own step. |
| Docker build before a release can tag | `.github/workflows/ci.yml` | adopted | Single-arch build without push. |
| PreToolUse Bash guard with a heredoc-aware tokenizer and two-sided tests | `scripts/guard-commands.mjs` | adapted | Blocks unsigned commits, pushes to `main`, `--admin` merges and force-adds of secrets. |
| Cloud-only SessionStart hook | `.claude/hooks/session-start.sh` | adopted | Downloads modules and builds. |
| Named dev servers in launch.json | `.claude/launch.json` | adopted | One entry, `amdl-dev`, for `make dev`. |
| Gates fail closed, advisory checks fail open and say so | `.claude/rules/scripts.md`, `.claude/rules/workflows.md` | adopted | |
| Jobs holding secrets run default-branch code only | `.claude/rules/workflows.md` | adopted | |
| Verify and diagnose skills | `.claude/skills/amdl-verify/SKILL.md`, `.claude/skills/amdl-diagnose/SKILL.md` | adapted | Loops use `go test`, httptest, `make dev` and saved Apple fixtures. |
| Contract drift test derived from the real router | internal/api route/OpenAPI parity test | adopted | Lands separately in internal/api. |
| Revert proof script | `.claude/skills/amdl-diagnose/SKILL.md` | adapted | A manual red-then-green step with a temporary commit, not a script. |
| Verify Action with receipts, verifier and judge models | none | not adopting | Heavy for a repo where most work lands on `dev` directly. |
| Merge-message generator and merge-method hook | none | not adopting | The release flow and DCO already define merges. |
| Path-based PR tiers and labels | none | not adopting | Same reason. |
| Shrink-only baselines | none | not adopting | No legacy violations to freeze; `golangci-lint --new-from-rev` if needed. |
| Scheduled routines (skill mining, daily review, update digests) | none | not adopting | No routine to govern yet; the disabled weekly snapshot workflow was removed. |
| Delegation, external-worker and cross-model review skills | none | not adopting | Covered by the maintainer's personal skills. |
| Cloudflare deploy guards, identity-signing enforcement, owner-specific conventions | none | not adopting | Site-specific; this repo ships a Docker image and requires DCO sign-off instead. |

## Console design references

Sources behind the embedded console in `internal/console`; the resulting spec is
[internal/console/DESIGN.md](../internal/console/DESIGN.md).

### Visual system

| Practice | Landed in | Status | Note |
| --- | --- | --- | --- |
| Radix Colors 12-step scale (1-2 backgrounds, 3-5 states, 6-8 borders, 9-10 solids, 11-12 text), exact v3 hex values | `internal/console/static/css/tokens.css` | adopted | Light and dark each use their own steps; contrast computed per pair. Light amber uses step 11 because step 9 is 1.58:1 on white. |
| Geist color roles and the background-100/200 split | `internal/console/static/css/tokens.css` | adapted | Named roles (canvas, surface, raised, hover, selected) instead of numbered steps. |
| Geist typography: 14/20 label default, tabular figures for numbers | [DESIGN.md](../internal/console/DESIGN.md#type-spacing-shape-motion) | adapted | System fonts only, so the console works offline and renders CJK titles. |
| Geist materials radius tiers; elevation only on floating layers | [DESIGN.md](../internal/console/DESIGN.md#type-spacing-shape-motion) | adapted | 4/6/12 px radius scale. |
| Linear's redesign: inverted-L chrome, neutral unsaturated surfaces, depth through surface steps | `internal/console/static/css/app.css` | adopted | |
| Monochrome ink accent instead of the amdl-web brand orange | `internal/console/static/css/tokens.css` | adopted | Brand `#e35e38` is 5.1 ΔE from failure red; it stays on the logo mark only. |
| Carbon status indicator pattern: two of color, shape and symbol; purple for outliers | `internal/console/static/js/lib/status.js` | adopted | "Already on disk" uses the purple skipped role; red/green fail under deuteranopia, so labels are mandatory. |
| Geist Status Dot: only in-progress states animate, no separate spinner | `internal/console/static/css/app.css` | adopted | The running pulse is the only loop. |
| Stripe badge discipline: a small semantic set, badges are read-only | `internal/console/static/js/ui/components.js` | adopted | |
| lyjwpage DESIGN.md structure: token tables, the doc as target and code as state, a guard against off-scale values | [DESIGN.md](../internal/console/DESIGN.md), `internal/console/console_test.go` | adapted | Structure only; the look is not borrowed. The guard is a Go test instead of a pnpm script. |

### Patterns

| Practice | Landed in | Status | Note |
| --- | --- | --- | --- |
| shadcn dashboard order (KPI cards, chart, table) and a sidebar that collapses to icons | `internal/console/static/js/app.js` | adapted | 232 px sidebar, 56 px rail at 640-1023 px, bottom tab bar below. |
| Grafana stat panel: one metric per tile, color only where it means something | `internal/console/static/js/views/overview.js` | adopted | |
| Sonarr `getQueueStatus`: one pure function from state to icon, kind and title | `internal/console/static/js/lib/status.js` | adopted | Unit-tested in `internal/console/jstest/status.test.mjs`. |
| Primer segmented ProgressBar with a visible value and `aria-valuetext` | `internal/console/static/js/ui/components.js` | adopted | Counts, never an aggregate percentage (`ItemProgress` has none). |
| Tremor Tracker: one block per period | job drawer track strip | adapted | One block per track, colored by item status. |
| VueTorrent: mobile card per item, keyboard-first queue | `internal/console/static/js/app.js` | adapted | Two-line queue cards under 640 px; `j`/`k` and `/` shortcuts. |
| Grafana logs panel and Supabase Logs Explorer: level colors, expandable JSON details, live toggle, filters in the URL | `internal/console/static/js/views/logs.js` | adopted | |
| Railway log attribute filters | `internal/console/static/js/views/logs.js` | adapted | `job_id` and `request_id` chips that apply the filter. |
| Tailscale filter chips and query-string state | `internal/console/static/js/lib/route.js` | adapted | Filters live in the hash query. |
| MDN ARIA `log` role for the log viewer | `internal/console/static/js/views/logs.js` | adopted | `aria-live="off"` while streaming; milestones go to one polite status region. |
| cmdk command menu | `internal/console/static/js/views/palette.js` | adapted | cmdk is React-only; a small Preact combobox reproduces groups, empty and loading rows. |
| Vercel dashboard: status in the tab, every view usable on mobile | `internal/console/static/js/app.js` | adapted | The tab title shows the running count. |
| Dataviz basics: one axis per chart, small multiples for two measures, thin bars, a table view | `internal/console/static/js/views/overview.js` | adopted | Tracks and bytes per day are two charts sharing the date axis. |

### Stack

| Practice | Landed in | Status | Note |
| --- | --- | --- | --- |
| Preact no-build workflow with htm instead of JSX | `internal/console/static/vendor/`, `internal/console/AGENTS.md` | adapted | The htm 3.1.1 standalone bundle is vendored and checksum-pinned instead of loaded from a CDN. |
| Go `embed` with `all:` and a content-hash ETag (embedded files have no ModTime) | `internal/console/console.go` | adopted | |
| Server-Sent Events read with `fetch` instead of `EventSource` | `internal/console/static/js/lib/stream.js` | adopted | The client must tell 204 (finished) from 404 and 5xx, and own its backoff and hidden-tab pause. |
| Alpine.js, petite-vue, htmx with Go templates, plain DOM | none | not adopting | Larger or less testable; htmx would add a second rendering path that bypasses the public API. |
| A bundler (Vite) and a committed `dist/` | none | not adopting | The image has no Node stage; amdl-web is already the full React app. |
| hls.js for motion covers outside Safari | none | not adopting | Motion covers play only where the browser plays HLS natively; revisit if the showcase needs them everywhere. |
| Sparklines in KPI tiles | none | not adopting | Throughput history has its own chart from `/stats`; tiles stay single numbers. |
| Favicon status dot | none | not adopting | It would need color literals outside the token file; the tab title carries the running count. |
