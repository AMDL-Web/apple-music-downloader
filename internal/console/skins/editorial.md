# Skin: editorial (Swiss editorial)

Implementation: `internal/console/static/css/skins/editorial.css`. The shared
contract (structure, tokens a skin must declare, DOM hooks, what a skin file
may do) is [DESIGN.md](../DESIGN.md#skins); this page only says what is
particular to this skin. Status: first-pass tokens from the design mockup, to
be refined by the skin designer.

## Intent

A record label's catalogue page: paper white and ink black themes, a strict
12-column grid, heavy grotesk display type (Helvetica Neue / SF Pro Display
Bold) at large sizes, huge KPI numerals, numbered sections with a left label
column ("01 Jobs", CSS counters), hairline rules instead of boxes and cards, no
shadows, no radius, one accent used sparingly for what needs attention
(simulate band, failures, unreachable services, error logs), generous
whitespace. The sidebar becomes a horizontal masthead at 640 px and up.

## Tokens

| Token | Light | Dark | Use | Contrast |
| --- | --- | --- | --- | --- |
| `--bg-canvas` | `#fbfbf8` | `#0c0c0b` | paper | |
| `--text-primary` | `#0b0b0b` | `#f2f1ec` | ink | |
| `--text-secondary` | `#464644` | `#bdbcb6` | ink-2 | 9.2 / 10.3 on paper |
| `--text-tertiary` | `#686865` | `#94938e` | ink-3, section numbers (skin-local) | 5.6 / 6.6 |
| `--border-subtle` | `#d6d6d0` | `#2c2c2a` | hairlines (decorative) | |
| `--border-control` | `#8e8e89` | `#6f6e6a` | outlines that carry meaning | 3.3 / 3.8 |
| `--border-strong` | ink | ink | section rules, masthead (skin-local) | |
| `--accent` | `#d6001c` | `#ff3b30` | alerts only | 5.2 / 5.5 |

Running, completed, healthy and skipped share ink; they differ by glyph and
word.

To fill in: display sizes (page title up to 148 px, hero title up to 176 px,
KPI numerals up to 124 px), the grid and gutters per width, glyph choices per
status.

## Components

To fill in: masthead, section label column, KPI numerals, live queue rows with
per-track cells and a text legend, health facts, recent strip, throughput as
14 columns with zero ticks, showcase hero as a full-bleed band with facts and
the numbered cover grid, logs, drawer, command palette.

## Open questions

- Helvetica Neue and SF Pro are Apple-only; Arial and Liberation fallbacks
  weaken the display type and the tight tracking.
- The overview is much taller than the other skin's at 1440 px.
