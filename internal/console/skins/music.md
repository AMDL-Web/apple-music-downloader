# Skin: music (Apple Music, immersive)

The default skin. Implementation: `internal/console/static/css/skins/music.css`.
The shared contract (structure, tokens a skin must declare, DOM hooks, what a
skin file may do) is [DESIGN.md](../DESIGN.md#skins); this page only says what
is particular to this skin.

## Intent

Artwork leads. Running jobs, the showcase hero, showcase tiles and the job
drawer's header take each album's own Apple colors (`--art-bg`, `--art-t1` to
`--art-t3`). Large rounded radii, SF Pro Display-style tight headlines, a red
accent close to Apple Music's. Dark first, with a designed light theme. The
operations data stays complete but is typeset like Apple's own inset grouped
lists, one level below the artwork. Under 640 px a frosted bottom tab bar is
the navigation.

## Tokens

Contrast is measured on `--bg-surface` (the grouped surface) unless noted.

| Token | Light | Dark | Use | Contrast L / D |
| --- | --- | --- | --- | --- |
| `--bg-canvas` | `#ffffff` | `#000000` | page | |
| `--bg-surface` | `#f5f5f7` | `#1c1c1e` | grouped surfaces, cards | |
| `--bg-raised` | `#ffffff` | `#1c1c1e` | drawer, palette | |
| `--bg-group` | `#f5f5f7` | `#2c2c2e` | grouped cells inside overlays (drawer times, buttons) | |
| `--bg-fill` | `#e8e8ed` | `#2c2c2e` | segmented track, search trigger, chips | |
| `--bg-hover` / `--bg-selected` | `#ececf0` / `#e3e3e8` | `#2c2c2e` | row hover, current nav item | |
| `--seg-thumb` | `#ffffff` | `#636366` | pressed segment | |
| `--chip-on-bg` | `#e8e8ed` | `#636366` | pressed toggle chip (log levels, Pause, Wrap); raised with `--chip-on-shadow` in dark only | |
| `--chip-off-bg` / `--chip-off-ring` | transparent / 1 px `#86868b` | `#2c2c2e` / none | toggle chip that is off | ring 3.33 |
| `--bg-chrome` | `rgba(246,246,248,.86)` | `rgba(28,28,30,.82)` | sidebar, tab bar (blurred) | |
| `--topbar-bg` | `rgba(255,255,255,.78)` | `rgba(0,0,0,.72)` | sticky top bar (blurred, no rule) | |
| `--text-primary` | `#1d1d1f` | `#f5f5f7` | text | 15.6 / 15.4 |
| `--text-secondary` | `#6e6e73` | `#a1a1a6` | labels, meta | 4.66 / 6.61 (5.07 / 8.16 on canvas) |
| `--border-control` | `#86868b` | `#6e6e73` | outlined remaining cells, field outline | 3.33 / 3.36 |
| `--accent` | `#e0213a` | same | primary and destructive fills | white on it 4.72 |
| `--accent-text` | `#d60f2c` | `#ff4d61` | "See all" links, current tab | 4.87 / 5.26 |
| `--mark` / `--brand` | `#fa2d48` / `#e0213a` | same | nav and palette icons, logo | non-text |
| `--focus` | `#0071e3` | `#0a84ff` | 2 px focus ring | 4.70 on white / 5.76 on black |
| `--info` | `#fa2d48` | same | running: equalizer, cells, chart bars | 3.79 on white / 5.53 on black |
| `--info-text` | `#d60f2c` | `#ff4d61` | "Running" label | 4.87 / 5.26 |
| `--success` / `-text` | `#248a3d` / `#1b7a34` | `#30d158` | completed marks / "Healthy" | text 4.97 / 8.42 |
| `--danger` / `-text` | `#d70015` | `#ff6961` | failed | 4.94 / 6.03 |
| `--warning` / `-text` | `#b25000` | `#ffd60a` | warnings, simulate notice | 4.77 / 12.05 |
| `--neutral` | `#77777c` | `#8e8e93` | off, queued (marks) | 3.6 / 5.22 |
| `--skipped` / `-text` | `#8944ab` | `#bf5af2` / `#da8fff` | already in library | 5.54 / 7.58 |
| `--on-role` | `#ffffff` | `#000000` | glyph on a solid health tile | 4.3+ / 6.6+ |
| `--track` | `#e8e8ed` | `#3a3a3c` | empty bars | |
| `--series-1` to `-4` | `#fa2d48`, `#ff9500`, `#8944ab`, `#77777c` | `#fa2d48`, `#ff9f0a`, `#bf5af2`, `#8e8e93` | formats bar and legend, by position | non-text |

Tints (`--*-tint`) are the role color at 10-20 % alpha. Badges carry no tint:
an icon (or the equalizer) plus a word, so status never rests on color.
`--badge-text` per role: running, failed, warning and skipped labels wear their
role text color; completed and neutral labels stay `--text-secondary` so a long
completed list stays quiet. Health badges use the role text for every state.

## Type

System stacks only: `-apple-system`, SF Pro Display / SF Pro Text, Helvetica
Neue, PingFang SC, then `--font-system`; mono is `ui-monospace`, SF Mono, Menlo.

| Role | Size / line | Weight | Tracking |
| --- | --- | --- | --- |
| Body (`--fs-body`) | 15 / 22 | 400 | -0.01em |
| Page title | 34 / 40 (30 / 36 phone) | 700 | -0.03em |
| Section title | 22 / 28 (20 / 26 phone) | 700 | -0.02em |
| Hero title | 64 / 66 (34 / 38 phone) | 800 | -0.035em |
| Live card title (`--fs-live-title`) | 26 / 30 (20 / 24 phone) | 700 | -0.025em |
| Live card artist (`--fs-artist`) | 17 / 22 (15 / 20 phone) | 500 | |
| KPI value | 30 / 34 (26 / 30 phone) | 600 | -0.03em, tabular |
| Eyebrow (status, hero kicker) | 12 / 16 | 600 | +0.04em, uppercase |
| Logs (`--fs-mono-log`) | 12.5 mono | 400 | |

## Shape

Running cards and the hero 28 px (`--r-live`); surfaces, tiles, drawer sheet
22 (`--r-card`); notices, toasts, drawer groups 16 (`--r-notice`); palette 18;
artwork 12, list covers 8; nav items and fields 9; segmented 10 / 8; buttons,
badges, chips and level chips are pills. Progress cells are 8 px capsules with
a 3 px gap; remaining cells are outlined, not filled. In the queue list the
cells join into one 6 px capsule bar (no gaps, remaining = `--track`), because
the cell count changes from row to row.

## Elevation and motion

Surfaces are flat: no card shadows or borders. Only artwork and floating layers
lift. Covers on album-colored cards cast a shadow tinted with the album's own
text color; shelf covers cast one tinted with `--art-bg`. Sidebar, tab bar, top
bar, palette and toasts are translucent with a backdrop blur. Showcase tiles
lift 3 px with an album-colored shadow and live covers scale 1.02 on hover,
both only under `prefers-reduced-motion: no-preference`. Running shows a
three-bar equalizer (two pseudo-elements animating `scaleY`, out of phase); it
freezes into a static glyph under reduced motion. Everything else uses the
base durations (120 ms color, 200 ms overlays).

## Components

- **Shell**: brand mark is a 34 px red gradient tile; "Console" group label via
  `.nav::before`; red nav glyphs; the current item is a filled 9 px pill. The top
  bar is the frosted canvas with a "Live" pill, a filled search trigger and two
  Apple segmented controls. On phones the pill loses its fill but keeps the
  word next to the dot. The icon rail (640-1023) keeps red glyphs and shows
  the active count as a red badge.
- **Page head**: title with its summary line beneath (15 px secondary), as an
  Apple large title.
- **Sections**: every card with a head is a shelf: the head sits on the
  canvas, the body is the grouped surface. Section actions are red text links
  with a chevron. A shelf never clips (only its body does), so a heading's
  glyph overshoot is never cut by the rounded corner.
- **Live cards**: under the cells a legend names each shape with its count
  (filled pill "saved", outlined "remaining"; states at zero are left out).
  Failures of the last 24 h follow the cards as grouped rows: a red circle
  glyph, "Failed" in red, the title and the mono link, the error and its age,
  and a Retry button (under the text on phones).
- **Overview order**: Now downloading (full width), Recently completed, then
  Jobs and Throughput (8 columns) beside System health (4 columns, spanning both
  rows), then the log tail. Below 1024 px they stack in that reading order:
  Jobs, Throughput, System health, log tail.
- **Live queue**: each job is a card filled with its album gradient; 136 px
  cover (96 on phones); eyebrow = equalizer + RUNNING in `--art-t3`; title up
  to two lines; artist in `--art-t2`; genre · year · started below; per-track
  cells in `--art-t1`, remaining cells outlined at 75 %. A job without a palette
  (queued, resolving) falls back to the grouped surface.
- **Recently completed**: a shelf with no surface; every tile is shown and the
  row scrolls (7 per view at 1280+, 5, 4, then 138 px tiles on phones running
  to the screen edge). Title and artist, then tracks · finished · took on up
  to two lines; phones show title and artist only.
- **Jobs**: one grouped surface, five hairline-separated columns (three per row
  on phones); icon and label share a line; a zero value goes secondary. Only
  Total shows its sublabel (cancelled); the others' repeated "of N total" is
  hidden but keeps its line, so the columns share one baseline.
- **System health**: inset grouped list, 30 px solid icon tiles in the role
  color, hairlines inset to the text, status word right-aligned in its role
  color, detail in mono. Runtime facts form a second group below a 6 px canvas
  gap.
- **Grouped lists (keyline)**: every hairline starts at the text and runs to
  the card's right edge: health rows, runtime facts, and label/value lists
  (About, Runtime). The tool rows (ffmpeg) continue the Runtime list with the
  same row padding.
- **Throughput**: totals as 20 px figures over their labels; the formats bar
  and its legend dots take one hue per position (`--series-1` to `-4`: red,
  orange, purple, gray).
- **Queue**: one grouped list, 48 px covers, hairlines inset past the cover;
  phones get two-line rows with progress under the title.
- **Drawer**: a floating 22 px sheet inset 8 px from the window (full screen on
  phones); the head is an album-colored card with a 112 px cover; times and
  action buttons are grouped cells. The tracks head carries the per-track
  cells; the extra track strip is hidden, since each row carries its status.
  Track stage fills are calm secondary ink and turn red only on a track that
  is still running.
- **Showcase**: the hero moves above the type switch: album gradient, 300 px
  cover, 64 px heavy title, artist in 24 px, text bottom-aligned with the
  cover like an album header; centered column on phones. The hero actions
  (Tracks filled in `--art-t1`, Open in Apple Music tinted) are lifted into the
  hero's bottom padding under the text, which reserves their height. Tiles
  fill with `--art-bg`; genre · year left, track count right, finish time below.
- **Logs**: level chips, Pause and Wrap are one kind of toggle pill: pressed is
  `--chip-on-bg` (the gray fill in light, the raised thumb in dark), off is
  outlined in light and filled in dark; an off level chip also hollows its
  dot. Filled attribute chips, a grouped log surface.
- **Palette and toasts**: frosted raised surface, red item glyphs, 20 px input
  with no focus rule (the caret shows focus; the 1 px divider stays). On phones
  the palette is inset 8 px with the 16 px radius and drops the key hints.
- **Notices**: 16 px radius role tint, 18 px icon.
- **Empty / loading / error**: base structure; skeletons follow the radii
  (12 for tiles, 28 for the hero).

## Do

- Put album color only where Apple provides it (`--art-*`); fall back to the
  grouped surface, never to a made-up tint.
- Pair every status color with a glyph and a word.
- Add new display sizes as `--fs-*` tokens in the token block.

## Don't

- Put shadows or borders on surfaces; elevation is for artwork and overlays.
- Use the accent red for success or for decoration on data that is healthy.
- Hide operations data to make room for artwork; move it down instead.

## Open questions

- SF Pro Display only exists on Apple platforms; Windows and Linux fall back to
  Helvetica Neue or Segoe UI and the tight tracking looks heavier there.
- Many light album colors at once on a black page can look loud.
- The live list payload cannot tell a track in flight from a waiting one, so
  the legend says "remaining" for both and there is no "13 % transferred".
- A meaningful sublabel per Jobs column ("of 256 slots", "3 workers") and
  icon tiles for the runtime facts need DOM that the console does not render
  yet.
