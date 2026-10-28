# Skin: music (Apple Music, immersive)

The default skin. Implementation: `internal/console/static/css/skins/music.css`.
The shared contract (structure, tokens a skin must declare, DOM hooks, what a
skin file may do) is [DESIGN.md](../DESIGN.md#skins); this page only says what
is particular to this skin. Status: first-pass tokens from the design mockup,
to be refined by the skin designer.

## Intent

Artwork leads. Running jobs, the showcase hero and showcase tiles take each
album's own Apple colors (`--art-bg`, `--art-t1` to `--art-t3`); large rounded
radii; SF Pro Display-style tight headlines; a red accent close to Apple
Music's; dark first, with a designed light theme. Operations data stays
complete but is typeset like Apple's own grouped lists. Under 640 px the bottom
tab bar is the navigation.

## Tokens

| Token | Light | Dark | Use | Contrast |
| --- | --- | --- | --- | --- |
| `--bg-canvas` | `#ffffff` | `#000000` | page | |
| `--bg-surface` | `#f5f5f7` | `#1c1c1e` | grouped surfaces | |
| `--text-primary` | `#1d1d1f` | `#f5f5f7` | text | |
| `--text-secondary` | `#6e6e73` | `#a1a1a6` | labels, meta | 4.66 / 6.61 on surface |
| `--accent` | `#e0213a` | same | primary button, white text | 4.72 |
| `--accent-text` | `#d60f2c` | `#ff4d61` | links, section actions | 4.87 / 5.26 on surface |
| `--info` | `#fa2d48` | same | running, chart bars (marks only) | |

To fill in: the remaining status roles with measured contrast, display sizes,
radii (28 running cards, 22 surfaces and tiles, 16 notices, 12 artwork, 9
controls), elevation (album-tinted cover shadows), translucent chrome.

## Components

To fill in: Now Downloading cards (palette, per-track cells, legend), the KPI
group, health as an inset grouped list with icon tiles, the recent shelf
(horizontal under 640 px, uses `.strip-tile.is-overflow`), the showcase hero
and tiles, the log list, the drawer, the command palette.

## Open questions

- SF Pro Display only exists on Apple platforms; check Windows and Linux
  fallbacks.
- Many light album colors at once on a black page can look loud.
