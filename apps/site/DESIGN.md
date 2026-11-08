# Website design

The specification of the AMDL website's redesign. It is written for the person who builds
the page and can read only this file, the style reference that comes with it, and the build
plumbing in this directory (`build.mjs`, `blocks.mjs`, `serve.mjs`, `og.mjs`,
`build.test.mjs`, `static/`). Every fact, name, number, path, color, label and data value the
page needs is written here; nothing has to be looked up in the product code.

The page's words are not written here. Section 8 is a brief for the copywriter (another
model): one JSON object with the tone rules, the facts and, per key, its purpose, the
facts it may use, its length limits and the marks it may use. The two content files
(`content/en.json`, `content/zh-CN.json`) are that schema filled in; they arrive from the
copywriter (9.1).
What this document does give verbatim is text that is not copy: labels of the real apps,
commands, flags, config keys, file paths, format names, numbers, MCP tool names and the
made-up demo catalog. Those are written exactly as the code has them, with their source.

Sources are cited as `path#Symbol` (a Go declaration, a YAML key, a Markdown heading, or a
word in another file). `go test ./tools/repocheck/` checks that each still exists.

Contents

1. Goal and audience
2. What to show and what to leave out
3. Page structure
4. Visual system
5. Product mocks
6. Cover art
7. Fact sheet
8. Copy brief
9. Implementation constraints and test plan
10. Responsive behavior
11. Accessibility

---

## 1. Goal and audience

**Goal.** A visitor understands in one screen what AMDL does (Apple Music links in,
finished, tagged `.m4a` files on their own disk out), sees that it is a real, carefully made
product, and leaves the page with an install command or a link into the docs. The current
page lists everything AMDL does; the new one shows a few things well and links to the docs
for the rest.

**Audience**, in order:

1. People who keep a local music library (lossless collectors, Plex/Jellyfin users, NAS
   owners) and have an Apple Music subscription. They care about formats, tags, covers,
   lyrics and where the files land.
2. Self-hosters and terminal users who will run it on a NAS, in Docker or as a service, and
   script it.
3. People who drive tools through AI agents (MCP).

They arrive from GitHub, a forum post or a search. Many read Chinese: the page exists in
English (`en/`) and Simplified Chinese (`zh-CN/`) with the same structure.

**Character.** Calm, exact, unhurried. The look comes from the style reference
(`https://magpie.apache.org/`, captured in the reference folder as `STYLE-REFERENCE.md`,
`tokens.txt` and `part0.png` to `part5.png`): warm paper, one cobalt accent, tight headings,
centered sections with one visual each, pastel cards with soft shadows, a numbered stepper,
hairline dividers, a pale call-to-action band and a quiet footer, in light and dark. Nothing
of Magpie's content, logo, illustrations or wording is reused; only its visual language.

---

## 2. What to show and what to leave out

### Shown (four sections after the hero)

| # | Section | Visual | Why it earned its place |
| --- | --- | --- | --- |
| 0 | Hero | A shelf of five record sleeves (the demo albums' covers) with the finished file of one of them | The first question is "what is this"; a shelf of covers and one `.m4a` answers "music, on your disk" before any word is read, and it shows the cover art the maintainer asked to be detailed |
| 1 | From a link to a file (`#how`) | Three interlocking pastel cards: a link, what AMDL does to it, the files on disk | It is the whole product in one line, and the only explanation most visitors need. The cards carry the four pipeline stages and the real default folder layout |
| 2 | Formats (`#formats`) | One card: a cover with its file facts beside the codec ladder (the quality order and its fallback) | Formats are the main reason people choose AMDL; the ladder also shows the one mechanism worth explaining (codec fallback) without a paragraph |
| 3 | Ways to use it (`#apps`) | A four-step stepper, each step a large mock of a real screen: desktop app, web console, terminal UI, an agent over MCP, with one caption each. Under it, one line of two text links: HTTP API and hooks | AMDL is one backend with several front ends; showing them as equals, one at a time, is clearer than four sections. One large mock each, not three small ones |
| 4 | Install (`#install`) | A vertical numbered stepper with the real commands, in two tabs (Binary, Docker), and two callouts | The page has to end in something a visitor can do |
| — | Call to action band | Pale blue band, one headline, one button | Closes the page as the reference does |
| — | Footer | Wordmark, one-line description, the notice, two link columns, legal line | The notice (personal use, own subscription, undocumented API) has to be on the page |

### Left out (on purpose)

| Left out | Reason |
| --- | --- |
| The "Everything else" list, the specs grid and its seven small figures | A feature dump; the docs hold it. Each section now has one visual |
| Records (per-track state, retries after 30 min and 2 h, give-up, disk check, second quality) | True and useful, but it is how AMDL behaves after the first week, not why someone tries it. The docs link covers it |
| Library sync and browsing your Apple Music library | Needs a wrapper that hands out each account's media-user-token (AMDL-Web's fork); explaining that condition costs more words than the feature earns on a landing page |
| Multiple accounts and regions | An advanced setup; it would need the wrapper-manager explained first |
| The wrapper's internals (QEMU guest, native manager, KVM, Cloudflare template, gateway tokens) | Install step 3 and the NAS callout say what a visitor must know; the rest is in `docs/install.md#a-wrapper-amdl-runs` |
| Speed figures from benchmarks | Numbers without context read as marketing |
| Upgrading from 1.x | Only existing users need it; it stays as a link in the install section and the footer |
| Simulate mode, logs, statistics, the service manager, the command palette | Real but secondary; they belong to the docs |
| The CLI session mock, the desktop Queue and Library screens, the console New download and Logs screens, the hooks settings card, the hero waveform | One mock per interface is enough; these repeated what another mock already showed |
| Catalog language and naming details (`catalog.language`) | A setting, not a selling point; it is in the docs |
| Interactive buttons inside mocks (the old Pause, Download and Run hooks) | The mocks are pictures. The page's only controls are its own: theme, language, tabs, copy buttons |
| Fact rows under each mock, the API and hooks cards, the hook preset chips | They made the apps section the busiest on the page. Each panel keeps one caption; the API and hooks get a text link each, and the docs say the rest |
| Buttons in the hero, the license in the hero | The reference's hero is a headline, a picture, a lead and a small row; the header already has Install and the page ends in a call to action. The license is in the footer |

---

## 3. Page structure

Top to bottom. Content width 1120px, centered. Every section is centered: an `h2` with a
small anchor-link glyph after it, one short lead paragraph (max 640px wide, centered), then
its one visual. Sections are separated by a 1px hairline (`--line`) inside the content
width, as in the reference.

### 3.0 Header (sticky)

- Height 72px (64px on phones). Background `--paper` at 88% opacity with
  `backdrop-filter: blur(12px) saturate(1.2)`; a 1px `--line` bottom border.
- Left: the brand. The AMDL mark (four bars, see 4.1) at 22px, 8px gap, the wordmark
  `AMDL` (verbatim) in `--ink`, 19px, weight 600, letter-spacing -0.02em. The whole is a
  link to `./` (the current language's page top) labelled by `ui.home`.
- Right, in order, 8px apart:
  1. A **Sections** menu: a `<details>` whose `<summary>` is a 36px-tall ghost button with
     a 3-line menu icon and the word `ui.sections`. Its panel (a card: `--paper-raised`,
     radius 12px, `--surface-shadow`, padding 8px, min-width 220px, right-aligned under the
     button) lists the in-page links `nav.how` → `#how`, `nav.formats` → `#formats`,
     `nav.apps` → `#apps`, `nav.install` → `#install`, each a 40px row (44px on phones) with its section
     number in a small circle (1 to 4) before the label. The `<details>` sits inside
     `<nav aria-label="{ui.nav}">`.
  2. `nav.docs`: a ghost text link to `{blob}/docs/README.md`.
  3. The language link: text `ui.otherLang` (the other language's name), `hreflang` and
     `lang` of the other language, `title` = `ui.otherLangLabel`, href `../en/` or
     `../zh-CN/`.
  4. The theme switch: a 3-way radiogroup (Light, Dark, System), see 4.4.
  5. A GitHub icon link (`ICON.github` from `blocks.mjs`), `aria-label` = `ui.github`,
     href = the repository URL.
  6. The primary button `header.install` → `#install`, with no icon.
- One rule for narrower screens (section 10 repeats it):
  - Tablet (701–1023px): Docs and GitHub leave the header and appear at the bottom of the
    Sections panel, under a hairline, as 40px rows.
  - Phones (≤700px): the header holds only the wordmark, the Sections menu (icon only, a
    44px square; the label visually hidden) and the primary button, drawn lighter (36px
    tall, padding 0 12px, 14px) with an invisible `::after` that keeps a 44px hit area. The
    language link and the theme switch also move into the panel's bottom, after Docs and
    GitHub; the language row starts with an 18px globe icon (the same icon size and
    12px gap as the Docs and GitHub rows) so the three labels share one left edge; the theme
    switch there is a full-width row of three 44px radios.
  - Markup: the panel always contains these extra rows and the header always contains its
    own copies; CSS shows exactly one copy of each (`display: none` on the other, so the
    hidden copy is also out of the accessibility tree). The theme switch is thus rendered
    twice (`.theme.theme-head` and `.theme.theme-menu`); `site.js` already syncs every
    `[data-theme-set]` button, and arrow keys move within the group that has focus.
- A skip link (`ui.skip` → `#main`) is the first focusable element; it is visible only on
  focus (ink background, paper text, 12px 20px padding, top-left), as in the reference.

### 3.1 Hero (`<section class="hero" aria-labelledby="hero-title">`)

Padding: 96px top (64px phone), 80px bottom. All centered.

1. **Eyebrow**: a pill: `--surface` background, `--surface-ink` text, 13px, weight 500,
   6px 12px padding, radius 999px. Margin below 24px. It holds two parts:
   - `<span class="prerelease">`: a 6px dot in `--accent`, 6px gap, `hero.prereleaseBadge`
     (the 2.0 status), then a 1px × 12px `--line-strong` divider with 10px on each side.
     This whole span is temporary: on release day it is deleted with its key (section 9).
   - `hero.eyebrow`: the permanent part (plain facts that stay true after release).

   On phones (≤700px) the two parts stack instead of wrapping around the divider: the pill
   becomes a column (2px row gap, padding 6px 14px, radius 14px) and the divider is hidden.
2. **Headline** `<h1 id="hero-title">`: `hero.title` holds two lines separated by `\n`.
   Line one in `--ink`, line two in `--accent`; each line is a block `<span>`. Size
   `--title-hero`, see 4.2. Max width 900px.
3. **Visual: the shelf** (section 6 has the covers; this is the arrangement). Margin 56px
   above, 48px below. A `<figure role="img" aria-label="{hero.shelfLabel}">`; everything
   inside is `aria-hidden`.

   Wide layout (≥1024px): one inline SVG, `viewBox="0 0 1120 440"`, width 100%, height
   auto. Five square sleeves, bottoms aligned at y = 400, each a `<use>` of the album's
   full cover symbol at the given size, centered at x (from the left of the viewBox):

   | Order (left → right) | Album | Size | Center x | z (higher = front) |
   | --- | --- | --- | --- | --- |
   | 1 | Orchard Lights | 190 | 157 | 1 |
   | 2 | Tidal Rooms | 230 | 331 | 2 |
   | 3 | Glass Weather | 300 | 560 | 3 |
   | 4 | Low Tide Signals | 230 | 789 | 2 |
   | 5 | 夏夜信号 | 190 | 963 | 1 |

   (Neighbours overlap by 36px, so the row spans x 62–1058, centered, with air on both
   sides; the sizes step down symmetrically from the center, and at least 81% of each
   sleeve shows. The rule behind the choice: a sleeve left of the center shows its left
   part and one right of it its right part, so each side holds sleeves whose type and main
   shape sit on the visible side; 夏夜信号's vertical title is on its right. Salt & Static,
   Late Bloom and 海边的耳机 appear elsewhere.)

   Each sleeve: corner radius 3px (a `clipPath` rect with `rx="3"` scaled to the size), a
   shadow from one SVG filter `id="shelf-shadow"` holding two `feDropShadow`s with
   presentation attributes for light: `class="d1"` dy 2, stdDeviation 2, `flood-color`
   `#172723`, `flood-opacity` .16; `class="d2"` dy 16, stdDeviation 12, `flood-color`
   `#172723`, `flood-opacity` .16. Dark mode is a stylesheet rule in `site.css` under both
   dark selectors (4.1): `#shelf-shadow feDropShadow { flood-color: #000 }`,
   `#shelf-shadow .d1 { flood-opacity: .35 }`, `#shelf-shadow .d2 { flood-opacity: .45 }`
   (CSS properties override presentation attributes; this is a stylesheet, not a `style`
   attribute, so the CSP allows it). Then a 1px top edge highlight: a line along the top
   inside edge, `#fff` at opacity .22, and a 1px line `#000` at .18 along the bottom inside
   edge (the cardboard edge of a sleeve).

   There is no record or other object between the sleeves: the covers are the picture.

   **The shelf line.** A 1px hairline at y = 401 from x = 0 to 1120 in `--line` (it shows
   past the outer sleeves, x 0–62 and 1058–1120), and a soft floor shadow, the one
   continuous contact shadow under the row: an ellipse at (560, 404), rx 520, ry 10, fill
   `#172723` opacity .10, blurred (stdDeviation 6).

   **The file chip** (HTML, absolutely positioned over the figure; percentage positions so
   it follows the SVG's scale): left edge at 57% of the figure width (over Glass Weather's
   lower-right corner and Low Tide Signals' lower-left), top at 80% of its height; z above everything. A card: `--paper-raised` background, radius 12px,
   `--surface-shadow`, padding 12px 16px 12px 12px, a 1px `--line` border. Inside, a 36px
   rounded square (radius 8px, `--tint-blue` background, `--accent` music-note icon 18px)
   then two lines: `01. Glass Weather.m4a` (mono, 14px, `--ink`, weight 500) and
   `ALAC 24-bit · 96 kHz` (mono, 12px, `--muted`). Verbatim; this is the default path's
   file name (section 7, F20) and the app's long quality label (F16). The chip sits
   inside the figure and is `aria-hidden` with the rest; `hero.shelfLabel` names the file.

   Tablet (701–1023px): the same SVG and chip scale down with the width (the chip's text
   stays 13px/11px).

   Phones (≤700px): a second SVG (hidden above 700px; the wide one is hidden below),
   `viewBox="0 44 360 236"` (the drawing's empty top is cropped: the tallest sleeve starts
   at y = 60): three sleeves, Tidal Rooms 150 at center x 100, Low Tide Signals 150 at
   center x 260, Glass Weather 200 at center x 180 in front; bottoms at y = 260; shelf line
   at y = 261. The file chip leaves absolute positioning and is centered under the SVG,
   pulled up over the shelf (`margin-top: -28px; position: relative`, z above the SVG).

   Motion: on load, under `prefers-reduced-motion: no-preference` only, each sleeve fades
   in and rises 12px, 520ms `--motion-ease`, staggered by distance from the center
   (center 0ms, neighbours 60ms, the outer pair 120ms); the chip follows at 300ms. No motion
   otherwise.
4. **Lead**: `hero.lead`, `--text-lead` size, `--muted`, max width 720px (two to three
   lines at 1440). It contains
   exactly one `**…**` span. Rendering: a hero-only helper `markLead(s)` in `build.mjs`
   calls `inline(s)` and then replaces its single `<strong>…</strong>` with
   `<mark class="hl">…</mark>`; no other key gets this (elsewhere `**…**` stays `<strong>`,
   as `inline()` does today). `.hl`: `--citrus` as a highlighter
   (`background: linear-gradient(transparent 55%, var(--citrus) 55%)`, padding 0 2px),
   text `--ink`, weight 500. A test covers `markLead` (9.3).
5. **Runs on** (24px below the lead): one centered row, 13px, `--muted`, wrapping: the
   label `hero.runsOn`, then four items, each a 16px line icon and a name, 16px apart:
   `macOS` (apple-free icon: a laptop outline), `Windows` (a 2×2 square grid outline),
   `Linux` (a terminal-prompt `>_` box), `Docker` (a container: a box with three
   horizontal slats). No brand logos. The names are verbatim. After them, separated by a
   middle dot, a ghost text link `hero.source` with the GitHub icon (14px) → repository
   URL (`--ink`, weight 500, underline on hover). The hero has no buttons: Install is in
   the header and in the closing band.

### 3.2 From a link to a file (`<section id="how">`)

1. `h2` `how.title`, anchor glyph, lead `how.lead`.
2. **Visual: the interlocking row.** A `<div class="how-row">`, 48px below the lead,
   holding three `<article>` cards. The cards are real content, read in order by a screen
   reader: each title is an `h3`, card B's steps are an `<ol>`, card C's tree is a sub-figure
   (below). Only decoration is `aria-hidden`: the icons, the knobs, the AMDL mark. Three
   cards in a row, flush (no gap), joined by round knobs as in the reference's puzzle
   cards (`part2.png`):

   | Card | Tint token | Title ink token | Icon |
   | --- | --- | --- | --- |
   | A: the link | `--tint-peach` | `--ink-peach` | link (chain) |
   | B: AMDL | `--tint-blue` | `--ink-blue` | the AMDL mark in its four colors |
   | C: on your disk | `--tint-green` | `--ink-green` | folder |

   (Values in 4.1.) Each card at ≥1024px: width 1/3 of 1040px (the row is 1040px, at most
   100% of the content width, centered), min height 380px,
   padding 32px 28px 28px, a top-left light gradient (`--surface-light`), text centered
   for the icon and title, left-aligned for the body. The row as a whole carries the
   shadow with `filter: drop-shadow(0 5px 8px var(--shadow-contact))
   drop-shadow(0 20px 36px var(--shadow-ambient))` so the knobs share it. Outer corners
   radius 14px (left corners of A, right corners of C); inner edges square.

   Knobs: a 32px circle in card A's tint centered on the A|B edge at 50% height (half over
   B), and one in card B's tint on the B|C edge. Drawn as `::after` of A and B, `z-index`
   above the next card, with a soft edge on the side over the next card so it reads as a
   tab: `box-shadow: 2px 0 3px -1px var(--knob-shadow)` (stacked: `0 2px 3px -1px`).

   Card contents, top to bottom:
   - A 28px line icon (stroke 1.5, the card's title ink), centered; 14px gap.
   - Title (`how.link.title`, `how.amdl.title`, `how.file.title`): `--title-card`, the
     card's title ink, centered.
   - Body:
     - **A**: one sentence `how.link.text` (15px, `--muted` on the tint), then a mono pill
       holding the demo link verbatim:
       `https://music.apple.com/us/album/glass-weather/920001` (13px, `--ink`,
       background `#fff` at 60% (dark: `#000` at 25%), radius 8px, padding 8px 10px). The
       link may break only after a slash: the markup puts a `<wbr>` after each single `/`
       (`https://<wbr>music.apple.com/<wbr>us/<wbr>…`) and the pill keeps
       `overflow-wrap: normal; word-break: normal`. 10px under it, a second pill on the
       same plate shows the other half of the card's title, a search: a 14px magnifier
       icon (`--muted`) and `glass weather` (verbatim, mono 13px, `--ink`), 8px apart.
     - **B**: an `<ol>` of four checklist rows (check icon in `--accent`, 16px,
       `aria-hidden`; 10px gap): each a bold name (`how.amdl.steps.N.name`, `--ink`, 15px,
       weight 600) and a short clause (`how.amdl.steps.N.text`, 14px, `--muted`). Order:
       download, decrypt, remux, tag.
     - **C**: one sentence `how.file.text`, then the file tree: a
       `<figure role="img" aria-label="{how.label}">` holding a `<pre>` (its glyphs would
       read badly aloud) in mono 12.5px, line height 1.65, `--ink`, on the same
       translucent plate as card A's pill, padding 10px 12px, `overflow-x: auto` so a
       narrow card scrolls the tree inside the plate and never widens:

       ```
       library/
       └─ Mira Solenne/
          ├─ artist.jpg
          └─ Glass Weather/
             ├─ 01. Glass Weather.m4a
             ├─ 02. Paper Satellites.m4a
             ├─ 03. Slow Lightning.m4a
             ├─ …
             └─ cover.jpg
       ```

       The tree is verbatim for the default config: the default `library_path_format`
       with the demo album (F20), the artist's picture `artist.jpg` in the artist folder
       and the album cover `cover.jpg` in the album folder (F17). Folder names in `--ink`
       weight 600, files weight 400, the tree glyphs in `--muted`.
   - Bottom line (pinned to the card's bottom, centered, 15px, weight 600, the card's
     title ink): `how.link.note`, `how.amdl.note`, `how.file.note`.
3. Under the row, 32px, centered: a soft button `how.more` with an arrow →
   `{blob}/docs/download-pipeline.md`.

Below 1024px (tablet and phone) the cards stack (full width, min height auto, radius 14px
on the outer corners of the stack: top of A, bottom of C), and the knobs move to the
bottom edge's center (A|B and B|C). Three cards in a row need at least 1024px.

### 3.3 Formats (`<section id="formats">`)

1. `h2` `formats.title`, anchor glyph, lead `formats.lead`.
2. **Visual: the format card.** A `<div class="format-card">` of real content (only the
   cover, the circles and the chevrons are `aria-hidden`), 1040px wide (at most 100%),
   `--surface` background, radius 20px, `--surface-shadow`, padding 40px; at ≥1024px a
   grid of two columns: 320px and 1fr, gap 56px.

   **Left column: one file.**
   - The Glass Weather cover (full variant) at 320×320, radius 6px, shadow
     `0 18px 36px -12px rgba(23,39,35,.35)`.
   - 18px below, the file name `01. Glass Weather.m4a` (mono 14px, weight 600, `--ink`),
     a `<p>`.
   - A definition list (`<dl>`), 8px below, rows separated by 1px `--line`, each row 36px: term
     (13px, `--muted`) left, value (13px, `--ink`, mono where marked) right:

     | Term key | Value |
     | --- | --- |
     | `formats.file.codec` | `ALAC 24-bit · 96 kHz` (mono, verbatim) |
     | `formats.file.cover` | `5000 × 5000` (mono, verbatim) |
     | `formats.file.lyrics` | `formats.file.lyricsValue` (copy; it may contain `LRC`) |
     | `formats.file.tags` | `formats.file.tagsValue` (copy) |

   **Right column: the codec ladder.**
   - A heading line `formats.ladder.head` (13px, weight 600, `--muted`, uppercase in en
     with letter-spacing .06em; zh-CN not uppercased).
   - The config line, 10px below, as a one-line code chip (`--code-bg`, `--code-ink`, mono
     13px, radius 8px, padding 8px 12px, `max-width: 100%`, `overflow-x: auto`):
     `quality_priority: ["alac", "ec3", "aac"]`, with `quality_priority` in the key color
     and the strings in the string color (4.7). Verbatim config syntax (F13).
   - Below, an `<ol>` of four rungs stacked with 10px gaps, joined on their left by a vertical 1px
     `--line-strong` rule through 20px circles (a mini vertical stepper):

     | Rung | Circle | Name (verbatim) | Config value (mono, verbatim) | Note (copy) |
     | --- | --- | --- | --- | --- |
     | 1 | `1`, filled `--accent` | `ALAC` | `alac` | `formats.rows.alac` |
     | 2 | `2`, outlined | `Dolby Atmos` | `ec3` | `formats.rows.ec3` |
     | 3 | `3`, outlined | `AAC` | `aac` | `formats.rows.aac` |
     | 4 | a dashed outline, no number | `AAC-LC` | (none) | `formats.rows.aaclc` |

     Each rung is a row card: `--rung-bg`, radius 10px, padding 14px 16px, a soft
     shadow `--rung-shadow` (light `0 1px 2px rgba(23,39,35,.08)`; dark adds a faint top
     highlight, so the rungs read raised above the card, not sunk into it); the name 17px weight 600 `--ink`, the config
     value as a small mono tag (12px, `--surface-ink` on `--tint-blue`, radius 6px, padding
     2px 6px) right of the name, the note on a second line (14px, `--muted`). Rung 4's card
     has a 1px dashed `--line-strong` border and no shadow (the floor, always tried last).
     Between rungs, on the vertical rule, a 12px down-chevron in `--muted`.
   - Under the ladder, `formats.also` (13px, `--muted`) followed by three mono chips,
     verbatim: `ac3` `aac-binaural` `aac-downmix`.
3. Caption under the card, centered, 14px, `--muted`, max 640px: `formats.caption`.

Below 1024px the card is one column: the cover 240×240 centered above the file name, the
`dl` and the ladder; padding 32px (24px on phones).

### 3.4 Ways to use it (`<section id="apps">`)

1. `h2` `apps.title`, anchor glyph, lead `apps.lead`.
2. **The DOM.** One tab widget built with `blocks.mjs#tabs`, extended with two optional
   arguments (keep today's calls working): `tabs(label, items, { cls, between })`, where
   `cls` is added to the outer `div` and the tab list, `between` is HTML placed after the
   tab list, and an item may give `labelHtml` (used as is) in place of `label` (escaped).
   This section needs no `between`. The tree:

   ```
   <div class="tabs apps">
     <div class="tablist stepper" role="tablist" aria-label="{apps.title}">
       <button role="tab" id="tab-apps-desktop" aria-controls="panel-apps-desktop" …>
         <span class="step-n" aria-hidden="true">1</span><span class="step-l">{apps.desktop.tab}</span>
       </button>  … console (2), terminal (3), agents (4)
     </div>
     <div class="panel" id="panel-apps-desktop" role="tabpanel" aria-labelledby="tab-apps-desktop">
       <div class="stage">
         <button class="stage-arrow prev" aria-label="{ui.prev}">…</button>
         <div class="mock mock-desktop" role="img" aria-label="{apps.desktop.label}">
           <div class="dk dk-wide" aria-hidden="true" lang="en">…</div> …
         </div>
         <button class="stage-arrow next" aria-label="{ui.next}">…</button>
       </div>
       <p class="stage-caption">{apps.desktop.caption}</p>
       <a class="btn soft" href="…">{apps.desktop.doc} →</a>
     </div>
     … three more panels
   </div>
   ```

   The arrows live in each panel's stage, outside the `.mock`, so the "mocks contain no
   controls" test holds; they select the previous or next tab, wrapping (desktop ↔
   agents), and move focus to that tab. They need JavaScript: add `.stage-arrow` to
   `NOSCRIPT_STYLE` in `blocks.mjs` (next to `.tablist,.copy,.theme`). In panel 4 the
   M4 side column also sits inside `.stage`, beside `.mock`, never inside it.
3. **The stepper** (the tab list): four numbered steps on one hairline, max width 760px,
   centered, 40px below the lead, min height 64px: 1 `apps.desktop.tab`,
   2 `apps.console.tab`, 3 `apps.terminal.tab`, 4 `apps.agents.tab`. Visuals in 4.4.
4. **The stage**, 32px below the stepper: a rounded panel (radius 24px) the width of the
   content (at most 1120px), `--stage` background, padding 40px 64px. At ≥1024px it has a
   fixed `aspect-ratio: 1120 / 700`, so switching tabs never moves the page; its inner box
   (992×620 at full width) is the 1.6 ratio of M1 and M2, which fill it, while M3 and M4
   are centered in it. Below 1024px its height is auto. The mock carries its own shadow
   (`0 30px 60px -20px rgba(23,39,35,.35), 0 0 0 1px rgba(23,39,35,.06)`; dark
   `0 30px 60px -20px #000, 0 0 0 1px rgba(255,255,255,.06)`). The round 40px arrows
   (`--action-soft-bg`, chevron icon) sit vertically centered (`top: 50%`) on the stage's
   left and right edges, half outside it.
5. Under the stage, centered:
   - The caption (`apps.X.caption`), 1.0625rem, `--muted`, line height 1.6, max 620px,
     28px below the stage. It is the panel's only sentence; it sits under the section
     lead in weight, not above it.
   - 20px below, a soft button `apps.X.doc` with an arrow, to the panel's guide:

     | Panel | Guide |
     | --- | --- |
     | desktop | `{blob}/docs/install.md#the-desktop-app` |
     | console | `{repo}#web-console` |
     | terminal | `{blob}/docs/cli.md#terminal-interface` |
     | agents | `{blob}/docs/cli.md#mcp-server` |

   The panels' mocks (section 5): desktop → M1, console → M2, terminal → M3, agents → M4
   with its side column.
6. Under the tab widget, 16px, centered, 13px `--muted`: `apps.shown` (the mocks are
   illustrations with made-up music and, like the apps, in English).
7. **Two more ways**, 24px below, one centered line, 15px: `apps.more` (13px `--muted`
   label), then two text links 24px apart, each `--accent`, weight 500, with a 16px line
   icon before it: `apps.api.link` (a plug) → `{blob}/docs/api.md`, and `apps.hooks.link`
   (a lightning bolt) → `{blob}/docs/automation.md#job-hooks`. It wraps on phones (label
   on its own line, then the two links). No cards, no chips.

### 3.5 Install (`<section id="install">`)

1. `h2` `install.title`, anchor glyph, lead `install.lead` (it names the two requirements:
   an Apple Music subscription and a wrapper).
2. A segmented control centered: `tabs(install.tabsLabel, …)` (the tab list's accessible
   name), two tabs `install.tabs.binary`, `install.tabs.docker`. Pill track `--surface`,
   thumb `--paper-raised` with a small shadow, 36px tall (44px on phones), 14px weight
   600. Ids `tab-binary` / `panel-binary`, `tab-docker` / `panel-docker`.
3. Each panel: a vertical stepper, max width 760px, centered, left-aligned text. Each step
   is a grid row: a 32px numbered circle (step 1 filled `--accent` with white number;
   others outlined `--line-strong`, number `--ink`) with a 1px `--line` vertical rule
   running from under it to the next circle; then the step: title
   (`install.<tab>.steps.N.title`, an `h3` in `--title-card`), text
   (`install.<tab>.steps.N.text`, 16px, `--muted`, rendered with `inline()`), its code
   blocks (4.4), and, for Docker step 4 only, a second paragraph after the code block
   (`install.docker.steps.3.after`), 14px apart. The
   commands are exactly these (`RAW` is
   `https://raw.githubusercontent.com/AMDL-Web/apple-music-downloader/${REF}` as in
   `build.mjs`):

   Binary:

   | Step | Code block label | Lines |
   | --- | --- | --- |
   | 1 | `macOS · Linux` | `$ curl -fsSL ${RAW}/deploy/install/install.sh \| sh` |
   | 1 | `Windows · PowerShell` | `PS> [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor 3072; irm ${RAW}/deploy/install/install.ps1 \| iex` |
   | 2 | (none) | `$ amdl doctor` |
   | 3 | (none) | `$ amdl wrapper install` |
   | 4 | (none) | `$ amdl service install` then `$ amdl service status` |

   Docker:

   | Step | Lines |
   | --- | --- |
   | 1 | `$ mkdir amdl && cd amdl`, `$ mkdir -p data/downloads && chown 1000:1000 data/downloads`, `$ curl -fsSLO ${RAW}/deploy/docker-compose.yml` |
   | 2 | (no code) |
   | 3 | `$ docker compose up -d` |
   | 4 | `http://wrapper-manager:8080` (an output line, no prompt; the value to paste), then the paragraph `install.docker.steps.3.after` |

   (These are `CODE.install` in today's `build.mjs`; keep that structure.)
4. Two callouts under the tabs, max 760px, 16px apart:
   - **2.0 note** (shown until release, see section 9): `--citrus` background, `--ink`
     text, radius 12px, padding 16px 20px, a label
     pill `install.prereleaseLabel` (11px uppercase weight 700, `--ink` on `#fff` at 60%),
     then `install.prerelease` (15px).
   - **NAS**: `--surface` background, `--surface-ink` text, same shape, label
     `install.nas.label`, text `install.nas.text`, then the link `install.nas.link` →
     `{blob}/docs/deployment.md#the-wrapper`.
5. `install.more` (15px, `--muted`, centered, max 640px): links to the install guide,
   the deployment guide and the 2.0 upgrade guide through inline `[text](href)` marks.

### 3.6 Call to action band

Full-bleed band (edge to edge), `--cta-bg`, padding 112px 0
(72px phones). Centered: `cta.title` as an `h2` in `--title-page` size (larger than a
section title), `--ink`; 28px below, one primary button `cta.button` with an arrow →
`{blob}/docs/install.md`. No lead, no second button.

### 3.7 Footer

`--paper` background, padding 56px 0 32px. A grid: brand column (2fr) and two link
columns (1fr each).

- Brand column: the mark and wordmark (22px), then `footer.tagline` (14px, `--muted`,
  max 320px), then `footer.notice` (13px, `--muted`, max 360px, 12px above).
- Column "Docs" (`footer.docs.title`, 14px weight 600 `--ink`), links 14px `--muted`, 10px
  apart, each with a 14px line icon (book, terminal, plug, bolt, arrow-up-right):
  `footer.docs.install` → `{blob}/docs/install.md`, `footer.docs.deployment` →
  `{blob}/docs/deployment.md`, `footer.docs.cli` → `{blob}/docs/cli.md`,
  `footer.docs.api` → `{blob}/docs/api.md`, `footer.docs.automation` →
  `{blob}/docs/automation.md`, `footer.docs.upgrade` → `{blob}/docs/upgrading-to-2.0.md`.
- Column "Project" (`footer.project.title`): `footer.project.github` → repository URL,
  `footer.project.releases` → `{repo}/releases`, `footer.project.license` →
  `{blob}/LICENSE`, `footer.project.docs` → `{blob}/docs/README.md`.
- Under the grid, 40px: a 1px `--line`, then one line, 12px, `--muted`: `footer.legal`
  on the left; on the right the language link again (same as the header's).

Phones: one column; the link columns side by side in two columns under the brand.

### 3.8 Head and shared labels

- `<title>` and `og:title`: `meta.title`. `<meta name="description">` and
  `og:description`: `meta.description`. `og:image:alt`: `meta.ogAlt`. With `--base`,
  `og:image` is `og.png`, plus canonical, `hreflang` alternates and `og:url`, as
  `build.mjs` does today. `theme-color` as in 4.1.
- Every code block's copy button takes `ui.copy` (its name) and `ui.copied` (after
  copying), through `blocks.mjs#copyButton` as today.

---

## 4. Visual system

### 4.1 Color tokens

Declared on `:root` in `static/site.css`, redefined for dark under
`@media (prefers-color-scheme: dark) { :root:not([data-theme="light"]) { … } }` and again
under `:root[data-theme="dark"] { … }` (the theme switch sets `data-theme`). `body` sets
`background: var(--paper); color: var(--ink)`.

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| `--paper` | `#fffdf7` | `#0e1412` | page background |
| `--paper-raised` | `#ffffff` | `#151d1b` | menus, chips, file chip |
| `--rung-bg` | `var(--paper-raised)` | `#202c3a` | ladder rungs (dark: `--surface` lifted toward white, so they read raised) |
| `--rung-shadow` | `0 1px 2px rgb(23 39 35 / .08)` | `inset 0 1px 0 rgb(255 255 255 / .04), 0 1px 2px rgb(0 0 0 / .4)` | ladder rungs |
| `--knob-shadow` | `rgb(23 39 35 / .1)` | `rgb(0 0 0 / .35)` | the how-row knobs' soft edge |
| `--ink` | `#172723` | `#e6ece8` | text, headings |
| `--muted` | `#52635a` | `#9aaba2` | body text, captions (6.27:1 / 7.73:1 on paper) |
| `--line` | `#dce4db` | `#24302c` | hairlines, borders |
| `--line-strong` | `#b9c6bb` | `#3a4843` | stepper rails, outlined circles, dashed borders |
| `--accent` | `#004aad` | `#8ab4ff` | the one accent: hero line two, links, active step, checks |
| `--action-bg` | `#004aad` | `#2f6fdf` | primary button (white text: 8.13:1 / 4.71:1) |
| `--action-hover` | `#003b8b` | `#3d7cf0` | primary hover |
| `--action-ink` | `#ffffff` | `#ffffff` | primary button text |
| `--action-soft-bg` | `#d3e3f9` | `#1c2a3d` | soft buttons, round arrows |
| `--action-soft-hover` | `#c2d8f4` | `#24364f` | soft hover |
| `--action-soft-ink` | `#004aad` | `#bcd3ff` | soft button text |
| `--surface` | `#e8effb` | `#18222e` | eyebrow pill, format card, NAS callout, segmented track |
| `--surface-ink` | `#233f60` | `#c4d8ff` | text on `--surface` |
| `--stage` | `#eef2f8` | `#141c26` | the apps stage |
| `--citrus` | `#edf4bd` | `#2e3318` | hero highlight, 2.0 callout |
| `--tint-peach` | `#ffe1cc` | `#352720` | card A |
| `--tint-blue` | `#dceaff` | `#1c2a3d` | card B, file chip icon tile, ladder value tags |
| `--tint-green` | `#d4efd5` | `#1b3024` | card C |
| `--ink-peach` | `#7a3f1f` | `#f2c9ad` | card A title, icon and bottom line; numbers in M4's JSON |
| `--ink-blue` | `#1a3f7a` | `#bcd3ff` | card B title, icon and bottom line |
| `--ink-green` | `#1d4f2a` | `#b9e3c1` | card C title, icon and bottom line; strings in M4's JSON |
| `--cta-bg` | `#dceaff` | `#152238` | the call-to-action band |
| `--code-bg` | `#131f2c` | `#0a1119` | code blocks (dark in both themes) |
| `--code-ink` | `#e5eaf4` | `#e5eaf4` | code text |
| `--code-line` | `#405269` | `#2a3a4f` | code block border and label bar rule |
| `--code-muted` | `#8a9bb3` | `#8a9bb3` | prompts, comments, output |
| `--code-key` | `#9cc2ff` | `#9cc2ff` | JSON/YAML keys |
| `--code-string` | `#c5e1a5` | `#c5e1a5` | strings |
| `--code-number` | `#ffcc80` | `#ffcc80` | numbers |
| `--shadow-contact` | `#17272324` | `#00000059` | shadow layer 1 |
| `--shadow-ambient` | `#17272326` | `#00000066` | shadow layer 2 |
| `--surface-shadow` | `0 5px 8px var(--shadow-contact), 0 20px 36px var(--shadow-ambient)` | same | cards |
| `--surface-light` | `linear-gradient(145deg, #ffffff50, transparent 65%)` | `linear-gradient(145deg, #ffffff0f, transparent 65%)` | top-left light on cards |
| `--focus` | `#004aad` | `#8ab4ff` | focus ring |

`<meta name="theme-color">`: light `#fffdf7`, dark `#0e1412`.

**The AMDL mark** (the brand's only other colors, never used elsewhere on the page): four
rounded bars on a dark tile, from `static/favicon.svg`, which stays as it is: a 32×32 tile
`rx="8"` fill `#0a0a0a`; bars (x, y, width 4, height, `rx="2"`): (6.5, 13, 12) `#fb7185`,
(11.8, 7, 18) `#fbbf24`, (17.1, 16, 9) `#34d399`, (22.4, 10, 15) `#818cf8`. The same four
colors are the terminal UI's logo (M3). In the header the mark is drawn inline at 22px; in
dark mode the tile gets a 1px `#ffffff33` inner stroke (`.mark-ring`, stroke-opacity .2)
so it does not vanish into the dark paper.

### 4.2 Type

**Font decision: system fonts, tuned; Geist is named first but not shipped.**

- Vendoring Geist as woff2 would break two rules this site keeps for good reasons: the
  Content-Security-Policy is `default-src 'none'` with no `font-src`, and the test "pages
  load nothing from another host" forbids any `url(` that loads a resource, so a font file
  needs a policy change, a test change and a second request per page; `static/` would grow
  by about 70–140 KB of binary. It would also not help the Chinese page, which Geist has
  no glyphs for, so the two languages would look like two sites.
- The look the reference gets from Geist comes mostly from three things that system fonts
  can do: a semibold heading weight (600), tight negative tracking on large sizes, and
  generous line height on body text.
- So the stacks name `Geist` first: a visitor who has it installed gets it, nobody
  downloads it. README's "Fonts are the system's" stays true.

```css
--font-sans: "Geist", ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
  "Segoe UI Variable Text", "Segoe UI", Roboto, "Helvetica Neue", Arial,
  "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif;
--font-display: "Geist", ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
  "Segoe UI Variable Display", "Segoe UI", Roboto, "Helvetica Neue", Arial,
  "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif;
--font-mono: "Geist Mono", ui-monospace, "SF Mono", SFMono-Regular, "JetBrains Mono",
  Menlo, Consolas, "Liberation Mono", monospace;
```

`-webkit-font-smoothing: antialiased; text-rendering: optimizeLegibility;`. No
`font-feature-settings`: stylistic sets differ from one system font to the next. Numbers
that line up (mocks, code, the format card's values) use
`font-variant-numeric: tabular-nums`.

Weights are only 400, 500, 600 and 700, which every system face has as a real or a
reliably snapped weight: body 400; buttons, labels and the eyebrow 500; headings, the
wordmark, card bottom lines and rung names 600; nothing on the page itself is 700 except
the 2.0 label pill.

| Role | Token | Size / line height | Weight | Tracking (en) | Tracking (zh-CN) |
| --- | --- | --- | --- | --- | --- |
| Hero title | `--title-hero` | `clamp(2.5rem, 5.6vw, 4.25rem)` / 1.06 | 600 | -0.045em | -0.01em |
| Page-size title (CTA) | `--title-page` | `clamp(2rem, 4vw, 3.25rem)` / 1.12 | 600 | -0.04em | 0 |
| Section title (`h2`) | `--title-section` | `clamp(1.75rem, 3.2vw, 2.5rem)` / 1.2 | 600 | -0.035em | 0 |
| Card title | `--title-card` | 1.25rem / 1.3 | 600 | -0.02em | 0 |
| Lead | `--text-lead` | `clamp(1.0625rem, 1.25vw, 1.1875rem)` / 1.65 | 400 | -0.005em | 0 |
| Body | `--text-body` | 1.125rem / 1.7 | 400 | 0 | 0.01em |
| Reading (cards, steps) | `--text-reading` | 1rem / 1.65 | 400 | 0 | 0.01em |
| Small | `--text-small` | .875rem / 1.55 | 400 | 0 | 0 |
| Eyebrow / label | — | .8125rem / 1.4 | 500 | .01em | 0 |
| Code | — | .875rem / 1.65 mono | 400 | 0 | 0 |

Headings use `--font-display`, `text-wrap: balance`; paragraphs `text-wrap: pretty`. On
`:lang(zh-CN)` headings take weight 600 and no negative tracking (negative tracking crowds
CJK).

### 4.3 Spacing and layout

`--space-1` 4px, `-2` 8px, `-3` 12px, `-4` 16px, `-5` 20px, `-6` 24px, `-8` 32px, `-10` 40px,
`-12` 48px, `-16` 64px, `-20` 80px. `--content-width: 1120px`. `--page-gutter`: 40px
(≥1024), 32px (701–1023), 20px (≤700). `--section-padding: clamp(64px, 8vw, 112px)` top
and bottom. Heading → lead gap 16px; lead → visual 48px. Radii: `--radius-control` 8px,
`--radius-card` 14px, `--radius-panel` 20px, `--radius-stage` 24px,
`--radius-round` 999px.

### 4.4 Components

**Buttons.** Height 40px (44px on phones), padding 0 16px, radius 8px, 15px weight 500,
icon 16px with 8px gap, `transition: background-color var(--motion-fast) var(--motion-ease)`.

- Primary: `--action-bg` / `--action-ink`; hover `--action-hover`; active translateY(1px).
- Soft: `--action-soft-bg` / `--action-soft-ink`; hover `--action-soft-hover`.
- Ghost (header): transparent, `--ink`, hover `--surface`.
- Round (stage arrows): 40px circle, soft colors, 18px chevron.

**Links** in text: `--accent`, underline 1px with `text-underline-offset: 3px`, the
underline at 40% opacity until hover.

**Section heading with anchor.** `<div class="sec-title"><h2 id="{id}-title">{title}</h2><a class="anchor" …></a></div>`:
the link `<a class="anchor" href="#id" aria-label="{ui.anchor}">` is the `h2`'s sibling,
not its child, so the heading's accessible name, and the section's through
`aria-labelledby`, is the title alone. `.sec-title` carries the heading type (the `h2`
inside is `display: inline` and inherits it), so title and glyph still run as one
centered, balanced line. The glyph: a 16px link icon in `--muted`, 8px left margin,
visible at 55% opacity, 100% on hover or focus.

**Inline code in copy** (leads, captions, card text, steps, callouts): a chip, 0.88em
mono, padding 0.1em 0.35em, radius 5px, `--ink` at 6% behind it. It never breaks inside a
token (`white-space: nowrap`), and `box-decoration-break: clone` keeps the padding on
both ends if a chip ever has to wrap. Below 360px of page width, where the longest chip
(`%LOCALAPPDATA%\Programs\amdl`) is wider than the column, chips may break anywhere.

**Cards.** Radius 14px, `--surface-shadow`, background a tint plus `--surface-light` as a
second background layer, padding 32px (the how cards use 32px 28px 28px). Icons are 1.5px-stroke line icons (`blocks.mjs`
icons take a `stroke-width` of 1.75 today; cards use 1.5).

**Stepper (apps).** A horizontal list: each step is a `button[role=tab]` laid out as a
column: a 28px circle (1px `--line-strong` border, `--paper` fill, 13px weight 600 number
in `--ink`) and a label under it (14px, `--muted`). A 1px `--line` rule runs behind the
circles from the first to the last center. The selected step: circle filled `--accent`,
number white, a 4px ring of `--action-soft-bg` around it (`box-shadow: 0 0 0 4px …`),
label `--accent` weight 600. Hover: circle border `--accent`. Focus: the focus ring around
the circle. Steps spread evenly (`justify-content: space-between`).

**Vertical stepper (install, ladder).** The same circles, the rule vertical (1px,
`--line`), each step's content to the right with 20px gap.

**Segmented control (install tabs).** Described in 3.5. Selected: thumb with
`0 1px 2px rgba(23,39,35,.12), 0 0 0 1px var(--line)`.

**Code block.** From `blocks.mjs#codeBlock` (keep its markup: `.code`, `.code-bar`, `pre`,
`.copy` button), restyled: `--code-bg`, 1px `--code-line` border, radius 10px. Label bar
36px, 12px mono `--code-muted`, the copy button at its right (28px, icon 16px,
`--code-muted`, hover `--code-ink`, a check icon after copying). On phones the copy
button is a 44px target, the label bar 44px tall, and an unlabelled block's `pre` keeps
60px on its right for the button. `pre` padding 14px 16px,
13.5px mono, `--code-ink`, horizontal scroll inside the block only (`overflow-x: auto`;
the page never scrolls sideways). Prompt (`.c-p`) `--code-muted` and not selectable
(`user-select: none`), comment (`.c-c`) `--code-muted` italic, output (`.c-o`)
`--code-muted`, JSON key `.c-k` `--code-key`, string `.c-s` `--code-string`, number `.c-n`
`--code-number`. A block without a label has its copy button in the top-right corner.

**Callouts.** Described in 3.5.

**Theme switch.** A pill (`--surface` background, padding 3px, radius 999px) holding three
28px round radio buttons (in the phone menu: a full-width pill of three equal 44px-tall
segments; see 3.0 for the two copies) with 16px icons (sun, moon, monitor from `blocks.mjs#ICON`);
checked: `--paper-raised` background with a small shadow, icon `--ink`; unchecked icon
`--muted`. `role="radiogroup"` `aria-label="{ui.theme}"`, each `role="radio"`
`aria-checked`, `title` and `aria-label` from `ui.themes.*`. Arrow keys move along it
(today's `site.js` does this).

**Sections menu.** Described in 3.0. The panel's rows: 40px, radius 8px, hover `--surface`,
the number circle 22px with 12px numbers.

**Footer.** Described in 3.7.

### 4.5 Motion

- `--motion-fast: .18s` (color, opacity), `--motion-content: .28s` (panel switch),
  `--motion-slide: .52s` (hero entrance), `--motion-ease: cubic-bezier(.22, .7, .3, 1)`.
- Tab panels (apps, install): the newly shown panel fades from 0 to 1 and rises 6px over
  `--motion-content`. Done in CSS with a keyframe on `.panel:not([hidden])`.
- The hero shelf entrance (3.1).
- Copy button: the icon swaps to a check for 1.6s (today's `site.js`).
- Nothing loops, nothing parallaxes, no scroll-triggered reveals.
- Under `prefers-reduced-motion: reduce`, every animation and transition is removed
  (`animation: none; transition: none`) and `scroll-behavior` is `auto` (otherwise
  `smooth` on `html`).

### 4.6 Icons

Line icons on a 24 grid, round caps and joins, `stroke="currentColor"`, `fill="none"`,
1.5px stroke in cards and the footer, 1.75px in buttons. Reuse `blocks.mjs#ICON` and add:
menu (three lines), chevron-left, chevron-right, chevron-down, check (exists), folder
(exists), laptop, grid-2x2, terminal-box, container (box with slats), plug (exists), bolt
(exists as `f_hooks`), book, music-note (exists as `note`), github (exists). All inline SVG with
presentation attributes only.

### 4.7 Code colors

See the token table (`--code-*`). JSON in code lines is colored by today's
`blocks.mjs` `json()` helper; YAML-like chips in the format card are colored by hand
(key, punctuation `--code-muted`, strings).

---

## 5. Product mocks

Four mocks, each a picture of one real screen, built in HTML and CSS (and inline SVG for
icons and covers) in the app's own tokens and type, on the demo catalog. They are not the
apps and run no app code. Each is `role="img"` with an `aria-label` from the content
files and its inside `aria-hidden="true"`; none contains a button, link or form control.
The words inside a mock are the app's English UI labels, on both language pages (the apps
are English-only), so the mock's inner `aria-hidden` wrapper (`.dk-wide`, `.dk-phone`,
`.cn-wide`, `.cn-phone`, `.tw`) carries `lang="en"`. The frame itself carries no `lang`:
its `aria-label` comes from the content files and is in the page's language. In M4 the
chat rows (you, the ask, the agent, the reply) are the page's copy, so only each tool
call (`li.ag-call`) carries `lang="en"`.

Every mock follows the page theme: the app's light tokens on a light page, its dark
tokens on a dark page (all three apps ship both themes).

**Scaling.** Each mock is drawn in the app's own pixels and scales as one picture: the
frame is a container (`container-type: inline-size` on its wrapper) and one design pixel
is `--u: calc(100cqw / <design width>)`, clamped to `[0.4px, 1px]`; every length inside is
`calc(n * var(--u))`. One exception: M3's window is narrower than the stage at 1px (815
design px at 100 columns), so its `--u` is `calc(100cqw / 830)` clamped to
`[0.4px, 1.2px]` and the terminal fills the stage's width (mono about 15.6px). The floor fits the narrowest tablet stage (701px page: about 557px
of stage inside its padding; M2's 1280 × 0.4 = 512). At ≤700px of page width each mock
switches to its phone layout (given per mock), because a desktop screen scaled to 300px
is unreadable. **Every phone layout** is `width: 100%; max-width: 343px`, centered; its
fixed sizes (icons, covers, type) are in px at `--u: 1px`, and every length that spans
the frame (rows, bars, cells, columns) is in % or `fr`, so the frame fits 280px (a 320px
screen) without scrolling the page.

Covers inside mocks are `<svg><use href="#cv-<key>"/></svg>` in the variant for their
size (section 6.2): full at 160 design px and larger, mid from 96 to 159, small below.

Icons inside mocks are drawn after Lucide (24 grid, 2px stroke), the set both the console
and the desktop's look approximate; they are listed by name.

### M1. Desktop app: the album page (panel 1)

**Recreates** `apps/desktop/internal/golden/testdata/album-light.png` and
`album-dark.png` (1024×640): the app's album page while the album downloads, in
standalone or client mode connected to a server named `nas`. Tokens from
`apps/desktop/internal/ui/theme.go#LightTokens` and `#DarkTokens`; radii from the same
file (card 24, control 8, inner 4, cover 16).

**Type.** `theme.go#MonoFont` says the interface and titles use the system font and only
audio facts (codecs, resolutions, counts, sizes, durations, percentages, paths, times) use
the embedded JetBrains Mono. The golden PNGs are drawn in the mono face throughout; the
mock follows `theme.go`: `--dk-sans: -apple-system, BlinkMacSystemFont, "Segoe UI
Variable Text", "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif` for labels and
titles, `--dk-mono: "JetBrains Mono", ui-monospace, "SF Mono", Menlo, Consolas, monospace`
(weight 500, semibold 600) for facts. Base size 13 design px.

**Tokens** (design-px values; hex as in `theme.go`; sampled values from the golden PNGs
where `theme.go` composes them):

| Token | Light | Dark | Use |
| --- | --- | --- | --- |
| ground | `#f6f7fa` | `#111219` | header area, top bar |
| side | `#fbfbfd` | `#15161f` | sidebar |
| body | `#f0f2f6` | `#0d0e14` | track list and activity area |
| raise | `#ffffff` | `#181a23` | buttons |
| sel | `#e3e3e6` | `#2b2c35` | selected nav row |
| card | `#efeff2` | `#22232c` | sidebar now-card and connection card |
| line | `rgba(20,22,34,.09)` | `rgba(236,238,248,.09)` | hairlines |
| line2 | `rgba(20,22,34,.16)` | `rgba(236,238,248,.16)` | button outline |
| fg | `#161822` | `#eef0f7` | text |
| muted | `#555b6e` | `#a3a8bb` | secondary text |
| faint | `#5f6476` | `#878ca1` | eyebrows |
| accent | `#3d58e0` | `#93a8ff` | the running track |
| ok | `#17834b` | `#62d69a` | Saved, finished cells |
| okSoft | `rgba(23,131,75,.10)` | `rgba(98,214,154,.14)` | connection icon tile |
| track | `rgba(20,22,34,.10)` | `rgba(236,238,248,.10)` | empty bars |
| gilt | `#d9bd78` (ink `#161822`) | same | the hi-res quality tag |
| run cell | `#8a8c92` | `#7d6633` | album step cells in progress |
| rest cell | `#d2d3d7` | `#352e22` | album step cells to come |
| hero ink | `fg` | `#f2c14e` (the album's fg) | title, artist, figure values |

In dark mode the album's own text color (Glass Weather's `#f2c14e`) colors the title, the
artist and the SIZE and TRACKS values, as in `album-dark.png`.

**Frame.** 1024×640 design px, radius 12, `overflow: hidden`, a 1px line ring. Grid: sidebar
232 | main 792.

**Sidebar** (`side`, right border 1px `line`; padding 18 10 12):

- Top-left, at (18, 18): a 16px panel-toggle icon (`panel-left`), `muted`.
- Nav at y = 60, rows 30 tall, 2 apart, x 10–221, radius 8, padding 0 14, icon 14 then
  label 13px (14px gap):
  - `Home`, icon `house`, selected (`sel` background).
  - `Queue`, icon `list`, and `3` right-aligned in mono 11px `muted`.
  - `Library`, icon `library`.
- Bottom, now-card at y 512–568 (x 10–221, radius 8, `card`): a 40px cover of Low Tide
  Signals (small variant, radius 4) at x 18; beside it `Low Tide Signals` (12px weight 600),
  `1 of 11` (mono 11px `muted`), and a mini progress bar 2px tall across 126px: a done
  piece (18px, `fg`), a striped running piece (26px, diagonal stripes of `fg` at 45%), the
  rest `track`, then five 3px dots (`fg`).
- Connection card at y 576–628 (radius 8, `card`): a 30px square tile (radius 6,
  `okSoft`, a `server` icon 16px in `ok`), then `nas:18570` (mono 13px weight 600) over
  `Live` (11px `muted`).

**Main.**

- Top bar (y 0–50, `ground`): at x 264 a `chevron-left` 14px and `Home` (13px `muted`); at
  the right (x 940–1006, y 11–41) a `Pause` button (13px, `raise`, 1px `line2`, radius 8,
  padding 0 12).
- Header (y 50–302, `ground`):
  - Cover: Glass Weather, full variant, 216×216 at (264, 64), radius 16, shadow
    `0 24px 48px -12px rgba(22,24,34,.35)` (dark `rgba(0,0,0,.6)`).
  - Text column at x = 504:
    - `ALBUM` at y 72: 11px weight 600, letter-spacing .08em, `faint` (dark `fg`).
    - `Glass Weather` at y 96–136: 36px, weight 700, letter-spacing -0.02em, hero ink.
    - At y 152–172: `Mira Solenne` (17px weight 500, hero ink), 10 gap, the gilt tag
      `ALAC 24-bit · 96 kHz` (mono 11px weight 600, `gilt` background, `#161822` text,
      radius 4, padding 2 6).
    - Figures at y 198: four columns at x 504, 629, 754, 879. Labels (11px weight 600,
      .08em, `muted`): `SIZE`, `SPEED`, `LEFT`, `TRACKS`. Values at y 218 (mono 18px
      weight 500): `536.3` followed by ` MB` in 13px `muted`; `6.4` + ` MB/s`; `~3 min`;
      `5 / 12`. SIZE and TRACKS values in hero ink, SPEED and LEFT in `fg`.
    - Step cells at y 250–258: twelve cells (one per track) across x 504–992, 2 apart,
      radius 2: cells 1–5 `ok`, 6–7 run cell, 8–12 rest cell.
    - At y 266: `5 / 12 finished` (mono 12px) left, `58%` (mono 12px) right-aligned at
      992.
  - A 1px `line` across the main column at y 302.
- Body (y 303–640, `body`):
  - Track list x 264–748, rows 59 tall, separated by 1px `line`, first row from y 316:

    | # | Title | Sub (mono 11px `muted`) | Meter | Length (mono 13) | State (12px, right-aligned at 744) |
    | --- | --- | --- | --- | --- | --- |
    | 1 | Glass Weather | `alac 24/96 · 98.6 MB` | full | `3:52` | check icon + `Saved`, `ok` |
    | 2 | Paper Satellites | `alac 24/96 · 107.9 MB` | full | `4:14` | `Saved` |
    | 3 | Slow Lightning | `alac 24/96 · 117.3 MB` | full | `4:36` | `Saved` |
    | 4 | Northbound | `alac 24/96 · 127.1 MB` | full | `4:59` | `Saved` |
    | 5 | Halo Static | `alac 24/96 · 85.4 MB` | full | `3:21` | `Saved` |
    | 6 | Small Hours | `alac 24/96 · 95.2 MB` | 62% | `3:44` | arrow-down icon + `62%`, `accent` |

    (Row 6 is cut by the frame's bottom edge at about two thirds, as in the golden.) The
    number is 13px `muted` right-aligned at x 296; the title 14px weight 600 `fg` at x 312
    (its column runs to x 484, so the mono sub line never reaches the meter in any
    fallback mono face); the meter at x 484–583: a 3px-tall stage bar: a 10px piece, a 72px piece, then three
    3px dots, all `fg` when full (dark: `fg` at 85%); for row 6 the 72px piece is filled
    62% in `accent` and the rest `track`, the dots `track`.
  - Activity aside x 785–1024 (1px `line` on its left, padding 20): `ACTIVITY` (11px
    weight 600, .08em, `faint`) at y 327; one row at y 352: `18:27:00` (mono 12px
    `muted`) and `Queued` (12px `fg`) 16 apart. At y 562 a 1px `line`, then `SAVED TO`
    (eyebrow style) and `/volume1/music/library/Mira Solenne/Glass Weather/` (mono 12px
    `fg`, line height 1.45, wrapping).

The sizes are made up (about 3.4 Mbit/s for 24/96 ALAC); 536.3 MB is the sum of the five
saved files. The lengths are `internal/demo/catalog.go#durationMS` for these songs.

**Phone layout** (≤700px): no sidebar, no aside. A card (width 100%, max 343px, radius
16, ground): top bar with `Home` and `Pause`; the cover at 36% of the card's inner width
(max 120px) left with the text column right (title 24px, wrapping, artist line, the gilt
tag on its own line); the 12 step cells full width; `5 / 12
finished` and `58%`; then rows 2–6 (body background; contiguous numbers, the running
row last), each 52 tall without the meter column. The cover here is the mid variant
(at most 120px; 6.2).

### M2. Web console: Overview (panel 2)

**Recreates** the console's Overview page with jobs running (`apps/console`, as served by
`amdl serve`, on the demo catalog), styled by its one skin `music`
(`apps/console/static/css/skins/music.css`, specified in
`apps/console/skins/music.md#tokens`). Page structure from the console's
`overview.js`: the health line, Needs attention, Live queue.

**Type.** `-apple-system, BlinkMacSystemFont, "SF Pro Text", "Helvetica Neue",
"PingFang SC", "Segoe UI", system-ui, Roboto, Arial, sans-serif` (display:
`"SF Pro Display"` in place of `"SF Pro Text"`); mono `ui-monospace, "SF Mono", Menlo,
Consolas, monospace`. Body 15/22 weight 400, tracking -0.01em.

**Tokens** (from `music.md#tokens`):

| Token | Light | Dark |
| --- | --- | --- |
| canvas | `#ffffff` | `#000000` |
| surface (grouped) | `#f5f5f7` | `#1c1c1e` |
| fill (search pill, chips) | `#e8e8ed` | `#2c2c2e` |
| selected (current nav row) | `#e3e3e8` | `#2c2c2e` |
| chrome (sidebar) | `rgba(246,246,248,.86)` | `rgba(28,28,30,.82)` |
| text | `#1d1d1f` | `#f5f5f7` |
| secondary | `#636368` | `#a1a1a6` |
| accent (New download pill, links) | `#e0213a` fill; text links `#d60f2c` | `#e0213a` fill; text links `#ff4d61` |
| brand tile | gradient `#fa2d48` → `#e0213a` | same |
| success | `#248a3d` | `#30d158` |
| danger / danger text | `#d70015` / `#c4001a` | `#ff6961` / `#ff6961` |
| danger tint | danger at 12% | danger at 18% |
| neutral | `#77777c` | `#8e8e93` |
| hairline | `rgba(0,0,0,.08)` | `rgba(255,255,255,.1)` |

Radii: cards 28, panels and covers 8, tags 4, pills full.

**Frame.** 1280×800 design px, radius 14, canvas background, `overflow: hidden`, a 1px
hairline ring. Grid: sidebar 248 | main 1032.

**Sidebar** (chrome, right hairline; padding 16 12):

- Brand row (height 44): a 34px tile (radius 8, the brand gradient at 135°, a white
  music-note glyph 18px) and `AMDL` (17px weight 700) 10 apart.
- 12 below: the `New download` pill: full width, 36 tall, accent fill, white `plus` icon
  16 and `New download` (15px weight 600).
- 16 below, nav rows (36 tall, radius 8, padding 0 10, icon 18 secondary, label 15px):
  `Overview` (icon `layout-grid`, current: selected fill, label weight 600, icon text
  color), `Activity` (icon `activity`, with a count badge `4`, the queued plus running
  jobs: fill background, 12px, radius 999, padding 0 7, right), `Settings` (icon
  `settings`). No `Library` row: the console shows it only while an account's Apple
  library is on, which this page leaves out (section 2).
- At the sidebar's foot: `Logs` (icon `file-text`, label secondary, weight 400).

**Top bar** (52 tall, canvas at 78% with blur, no rule): right-aligned, 12 apart: an 8px
live dot (success; no `Simulate` tag, which the console adds only in simulate mode), the
search pill (fill background, 300 wide, 34 tall, radius 999, `search` icon 16 secondary, `Search or paste a link…` 14px secondary, a `Ctrl K` key at
the right: 11px, 1px hairline border, radius 4, padding 1 5), and a 34px round theme
button (`monitor` icon 18).

**Page** (padding 8 32 32):

1. Title `Overview` (34/40, weight 700, tracking -0.03em, display face) with the date line
   `Nov 8` beneath (15px secondary; the console's short date, `format.js#shortDate`).
2. The health line (16 below): a success `circle-check` 16 and `All systems healthy`
   (15px weight 500, success text), then `Status` (15px, accent text link, 12 gap).
3. **Needs attention** (24 below). Head on the canvas: `Needs attention` (22/28 weight 700,
   tracking -0.02em), `Last 24 hours` (13px secondary, 10 gap), and at the right
   `All failed` + `chevron-right` (15px, accent text). Body: a grouped surface (surface,
   radius 28, padding 16 20) with one row: a danger `circle-x` 20; a chip `Failed`
   (danger tint background, danger text, 12px weight 600, radius 999, padding 2 8); the
   link `us/album/920099` (mono 13px, text); the error
   `Apple Music has nothing at this link in the US storefront` (13px secondary) on a second
   line under the link; at the right `10s ago` (13px secondary). No `Retry` pill: the
   console offers none for a not-found (404) failure (`overview.js`, `jobview.js#isNotFound`).
4. **Live queue** (32 below). Head: `Live queue`, and at the right `Open queue` +
   chevron. Body: a two-column grid (gap 16) of live cards, each 476×232:

   A live card: radius 28, padding 20, background
   `linear-gradient(140deg, <bg> 30%, color-mix(in srgb, <bg> 88%, <t1>) 100%)`, text
   color `<t1>`. Layout: cover 136×136 (radius 8, mid-variant cover: at about 105 rendered
   px the sleeve type would be noise; shadow `0 0 0 1px rgba(255,255,255,.14),
   0 8px 18px -6px rgba(0,0,0,.45)`, so a cover on a card of its own color keeps an edge)
   at left; to its right
   (gap 20): the eyebrow (a three-bar equalizer glyph 12px, then `RUNNING`, 12/16 weight
   600 tracking .04em, `<t3>`), the title (26/30 weight 700 tracking -0.025em, one line),
   the artist (17/22 weight 500, `<t2>`), the facts line (13px `<t3>`:
   `<genre> · <year> · Started <hh:mm>`). Across the bottom (full width, 20 below the
   cover): one cell per track (8 tall capsules, gap 3; saved cells filled `<t1>`, the rest
   outlined `1px` `<t1>` at 75%), then `<n>%` (13px weight 600, `<t2>`), and the legend
   under the cells (13px `<t2>`): a 14×6 filled capsule + `<saved> saved`, an outlined
   capsule + `<left> remaining`, and `~<m> min left`.

   Palette per album: `<bg>` = the album's bg, `<t1>` = its fg, `<t2>` = `#ffffff`,
   `<t3>` = `#ffffff`, both solid (the demo's second text color is `ffffff`; it has no
   third, so the console falls back to `--on-art-light`, `#ffffff`;
   `internal/demo/catalog.go#albums`, `format.js#artPalette`).

   | Card | Album | Artist | Facts | Cells | % | Legend |
   | --- | --- | --- | --- | --- | --- | --- |
   | 1 | Tidal Rooms | Mira Solenne | `Pop · 2022 · Started 21:16` | 10: 7 saved | `74%` | `7 saved`, `3 remaining`, `~2 min left` |
   | 2 | Low Tide Signals | Nightjar Coast | `Alternative · 2026 · Started 21:18` | 11: 4 saved | `41%` | `4 saved`, `7 remaining`, `~6 min left` |
   | 3 | 夏夜信号 | 青柠电台 | `Mandopop · 2025 · Started 21:19` | 10: 1 saved | `16%` | `1 saved`, `9 remaining`, `~11 min left` |
   | 4 | queued | — | — | — | — | — |

   Card 4 is a queued job not yet read: the grouped surface instead of a gradient, text
   in text/secondary colors, a 136px placeholder (fill background, radius 8, a
   `music` glyph 32 in secondary), eyebrow `clock` icon + `QUEUED` (secondary), the title
   the link in mono 17px `us/album/920004`, the artist line `Not resolved yet`
   (secondary), no cells.

   (The frame's height cuts the second row of cards at about 60%; the cut is the frame's
   bottom edge, with no fade.)

The Chinese title in card 3 is data (the demo artist 青柠电台), shown as received.

**Phone layout** (≤700px; frame width 100%, max 343px): the console's own phone layout:
no sidebar; a top bar with the brand tile and the live dot; the title `Overview` (30/36)
and the date `Nov 8`; the health line; Needs attention as one stacked row (link, error,
then `10s ago`); the live cards in one column (cover 96, title 20/24, artist 15/20), cards 1 and 2 only; a frosted bottom tab
bar (chrome, 56 tall) with four equal tabs: `Overview` (current, accent), `Activity`,
`New` (a 48×30 accent capsule holding a white `plus`, raised 6 px), `Settings`; labels
11px.

### M3. Terminal UI (panel 3)

**Recreates** `apps/amdl/internal/tui/testdata/frame-100.ansi` (100 columns) and, for
phones, `frame-46.ansi` (46 columns): `amdl` with no arguments, connected to a server
named `nas`, one album running as a job card, a search open under the prompt. The frames
in the repository use real artists; the mock puts the demo catalog in their place, keeping
every column position. Colors from `apps/amdl/internal/tui/theme.go#newTheme`.

**Theme** (the frame follows the page theme):

| Class | Dark | Light | Use |
| --- | --- | --- | --- |
| bg | `#1c1b19` | `#faf9f5` | terminal background |
| fg | `#e8e6dc` | `#141413` | text |
| dim | `#8a8880` | `#6f6d66` | secondary text |
| faint | `#45443f` | `#cfccc2` | rules, empty bars, the prompt box |
| sel | `#2c2b28` | `#ebe9e1` | background of the selected search row |
| acc | `#576c85` | `#1f3b5c` | the card's album color: Glass Weather's bg made to reach 3:1 on the terminal background (`albumAccent`) |
| soft | `#3a444f` | `#8c9aa9` | `acc` faded halfway into bg: the part of the album bar in progress (`soft`) |
| skip | `#6b6a64` | `#a3a19a` | a track already on disk |
| ok | `#9cc27a` | `#4f7a34` | `✓ in library`, the running dot |
| bad | `#f2836b` | `#b8442a` | failed |
| b1 | `#fb7185` | `#e11d48` | logo bar 1, prompt `>`, `▸`, `●` |
| b2 | `#fbbf24` | `#d97706` | logo bar 2 |
| b3 | `#34d399` | `#059669` | logo bar 3 |
| b4 | `#818cf8` | `#4f46e5` | logo bar 4 |
| bold | `fg`, weight 700 | same | titles |
| u | underline | | the selected search tab |

**Window.** A terminal window around the screen: radius 12, the frame background `bg`,
a 28px title bar (dark `#262522`, light `#efede6`) with three 10px circles in `faint` at
the left and the title `~ — amdl` centered (12px, `dim`, the page's sans face). Body
padding 14 18. The screen is a `<pre class="tui tui-wide">` of 23 lines at 13px mono,
line height 1.38 (`--font-mono`), `white-space: pre`; it scales with the container like
the other mocks (100 columns ≈ 780 design px). The logo's `█` and `▄` are drawn as inline
boxes (`<i class="blk">`, `<i class="blk half">`) one character wide, so the bars stay solid
in any font.

**The 100-column screen**, line by line, as runs of `[class, text]` (`""` = no color):

```text
 1  ["",   "   "] ["b2","██"] ["","    "] ["b4","▄▄"] ["","   "] ["bold","amdl"] ["dim"," 2.0.0"]
 2  ["b1","██"] [""," "] ["b2","██"] [""," "] ["b3","▄▄"] [""," "] ["b4","██"] ["","   "] ["dim","nas · nas.local:18570 · wrapper ready (us)"]
 3  ["b1","██"] [""," "] ["b2","██"] [""," "] ["b3","██"] [""," "] ["b4","██"] ["","   "] ["dim","/volume1/music · 1.2 TB free"]
 4  (empty)
 5  ["dim","  128 saved · "] ["bad","2 failed"]
 6  (empty)
 7  ["b1","> "] ["fg","mira solenne"]
 8  ["acc","◆ "] ["bold","Glass Weather"] ["dim"," · Mira Solenne #1"] ["", 57 spaces] ["dim","ALAC 24/96"]
 9  ["fg","  3/12  "] ["acc","━━━━"] [""," "] ["skip","━━━━━"] [""," "] ["acc","━━━━"] [""," "] ["bad","━━━━"] [""," "]
    ["soft","╍╍╍╍╍"] [""," "] ["soft","╍╍"] ["faint","──"] [""," "] ["faint","──── ───── ──── ──── ───── ────"] ["dim","  46%  ~1 min"]
10  ["dim","  03 "] ["fg","Slow Lightning          "] ["","  "] ["acc","━━━━━━ ━━━━━━━━━━━━━━━━━━━━"] ["faint","──────────── "] ["faint","─ ─ ─ ─ ─"] ["","  "] ["dim","Download 62%"]
11  ["dim","  05 "] ["fg","Halo Static             "] ["","  "] ["acc","━━━━━━ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ ╸ "] ["faint","─ ─ ─ ─"] ["","  "] ["dim","Decrypt 35%"]
12  ["dim","  06 "] ["fg","Small Hours             "] ["","  "] ["acc","━━━━━━ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ "] ["bad","━ "] ["faint","─ ─ ─ ─"] ["","  "] ["bad","✗ decrypt failed"]
13  ["b1","◆ "] ["bold","Slow Lightning Radio"] ["dim"," #2"] ["", 69 spaces] ["dim","queued"]
14  ["faint","╭" + 98 × "─" + "╮"]
15  ["faint","│ "] ["b1","> "] ["dim","link, search or /" padded to 95] ["faint","│"]
16  ["faint","╰" + 98 × "─" + "╯"]
17  ["fg","  search · mira solenne   "] ["bold u","all"] ["dim","  album  song  playlist  artist"]
18  (selected row, every run on the sel background)
    ["","  "] ["b1","▸ "] ["dim","○ "] ["dim","album    "] ["bold","Glass Weather" padded to 30] ["dim","  Mira Solenne · 2025 · 12 tracks" padded to 41] ["ok","  ✓ in library"]
19  ["","    "] ["b1","● "] ["dim","album    "] ["fg","Tidal Rooms" padded to 30] ["dim","  Mira Solenne · 2022 · 10 tracks"]
20  ["","    "] ["dim","○ "] ["dim","song     "] ["fg","Paper Satellites" padded to 30] ["dim","  Mira Solenne · 4:14"]
21  ["","    "] ["dim","○ "] ["dim","playlist "] ["fg","Slow Lightning Radio" padded to 30] ["dim","  Mira Solenne"]
22  ["","    "] ["dim","○ "] ["dim","artist   "] ["fg","Mira Solenne" padded to 30]
23  ["ok","  ●"] ["dim"," 1 running · 1 queued"] ["", 30 spaces] ["dim","enter downloads · space selects · tab filters"]
```

What it shows (all verbatim behavior of the TUI, `docs/cli.md#terminal-interface`): the
banner (logo, `amdl` and version, the server and its wrapper's state, the download folder
and space left, saved and failed counts), the prompt's last request, a running job card
(title, session number `#1`, quality, the album bar with one cell per track: done `━`,
already on disk in `skip`, failed in `bad`, in progress `╍`, not started `─`, then count,
percent and time left), three track rows with their seven-stage bars and states, a queued
card (`#2`, the demo playlist `Slow Lightning Radio`, curated by Mira Solenne), the
prompt box, the search tabs and hits (`○` unmarked, `●` marked with Space, `▸` the
cursor), and the status line with the keys of the moment.

**The 46-column phone screen** (`<pre class="tui tui-narrow">`, shown ≤700px instead of the
wide one; 20 lines, 12.5px):

```text
 1  as wide line 1
 2  ["b1","██"] [""," "] ["b2","██"] [""," "] ["b3","▄▄"] [""," "] ["b4","██"] ["","   "] ["dim","nas · nas.local:18570 · wrapper…"]
 3  as wide line 3
 4  (empty)
 5  as wide line 5
 6  (empty)
 7  as wide line 7
 8  ["acc","◆ "] ["bold","Glass Weather"] ["dim"," #1"]
 9  ["fg","  3/12  "] ["acc","━━━"] ["skip","━━"] ["acc","━━━"] ["bad","━━━"] ["soft","╍╍╍╍"] ["faint", 18 × "─"] ["dim","  46%"]
10  ["b1","◆ "] ["bold","Slow Lightning Radio"] ["dim"," #2"] ["", 15 spaces] ["dim","queued"]
11  ["faint","╭" + 44 × "─" + "╮"]
12  ["faint","│ "] ["b1","> "] ["dim","link, search or /" padded to 41] ["faint","│"]
13  ["faint","╰" + 44 × "─" + "╯"]
14  ["fg","  search · mira solenne"]
15  (sel) ["","  "] ["b1","▸ "] ["dim","○ "] ["dim","album    "] ["bold","Glass Weather" padded to 31]
16  ["","    "] ["b1","● "] ["dim","album    "] ["fg","Tidal Rooms"]
17  ["","    "] ["dim","○ "] ["dim","song     "] ["fg","Paper Satellites"]
18  ["","    "] ["dim","○ "] ["dim","playlist "] ["fg","Slow Lightning Radio"]
19  ["","    "] ["dim","○ "] ["dim","artist   "] ["fg","Mira Solenne"]
20  ["ok","  ●"] ["dim"," 1 running · 1 queued"] ["", 6 spaces] ["dim","enter downloads"]
```

The phone window is width 100%, max 343px. The 46-column screen at 12.5px mono is about
350px wide, so the `<pre>` scrolls sideways inside the window (`overflow-x: auto`); the
page does not.

### M4. An agent over MCP (panel 4)

**Shows** an agent client using `amdl mcp`. It is not a picture of a particular product
(no client's name, logo or colors); it is a neutral chat in the site's own tokens. The
tool names, argument names and result shapes are verbatim from the MCP server
(`docs/cli.md#mcp-server`, `internal/capability/capability.go#All`).

**Layout** inside the stage: at ≥1024px two columns, the chat 560 and the side column
taking the rest (`flex: 1`), gap 32 (scaled with the stage like the other mocks); both
stretch to the stage's full height (`align-items: stretch`), the chat's messages sit at
the bottom of its card as in a chat window, and the side column is centered vertically.
Below 1024px one column, the chat (width 100%, max 600px) with the side column under it.

**The chat** (560 wide at ≥1024px, `--paper-raised`, radius 16, 1px `--line`, padding 24,
the mock shadow). An ordered list, 16 apart:

1. A row labelled `apps.agents.chat.you` (11px weight 600 uppercase .06em `--muted`), the
   message `apps.agents.chat.ask` (16px `--ink`) in a bubble: `--surface`, radius 14,
   padding 12 16, right-aligned, max 80%.
2. Three tool calls, each a block (radius 10, 1px `--line`, background `--paper`, padding
   12 14), mono 12.5px:
   - Head: a 7px dot in `--accent`, the tool name in weight 600 `--ink`.
   - The arguments on one line, JSON-colored (keys `--accent`, strings `--ink-green`,
     numbers `--ink-peach`, punctuation `--muted`).
   - The result on one line, prefixed `→ ` in `--muted`, same coloring, clipped with
     `text-overflow: ellipsis` at the block's width.

   | Tool | Arguments | Result |
   | --- | --- | --- |
   | `search_catalog` | `{"term":"Nightjar Coast","types":["albums"],"limit":3}` | `{"albums":{"items":[{"name":"Low Tide Signals","release_date":"2026-05-14","url":"…/920003"},{"name":"Salt & Static","release_date":"2023-03-14","url":"…/920004"}]}}` |
   | `start_download` | `{"urls":["https://music.apple.com/us/album/low-tide-signals/920003"],"codec_priority":["ec3"]}` | `{"accepted":[{"url":"…/920003","status":"accepted","job_id":"job_9b2c…","resource":{"kind":"album","id":"920003"}}],"rejected":[]}` |
   | `wait_for_job` | `{"job_id":"job_9b2c…","timeout_s":300}` | `{"done":true,"job":{"status":"completed","album_progress":{"total":11,"done":11,"failed":0,"percent":100},…}}` |

3. A row labelled `apps.agents.chat.agent`, the reply `apps.agents.chat.reply` (16px
   `--ink`, left-aligned, no bubble), whose inline `code` mark holds the folder
   `/volume1/music/library/Nightjar Coast/Low Tide Signals/` (it follows the default
   template, F20).

**The side column** (the stage's remaining width, about 400):

- `apps.agents.config` (13px weight 600 `--muted`), then a code block (4.4, no label,
  with a copy button) holding the `mcpServers` entry `amdl cli setup` prints
  (`apps/amdl/internal/cli/meta.go#cliSetupCmd`), verbatim:

  ```json
  { "mcpServers": { "amdl": { "command": "/Users/me/.local/bin/amdl", "args": ["mcp", "--server", "nas"] } } }
  ```

  pretty-printed over 8 lines in the block.
- 20 below, a `<details>` (closed at every width) whose summary is
  `apps.agents.tools` (15px weight 600, `--ink`, a chevron that turns), listing every tool
  the MCP server declares, grouped, each group a label (`apps.agents.groups.<id>`, 12px
  `--muted`) and its tools as mono chips (12px, `--surface-ink` on `--surface`, radius 6,
  padding 2 6, gap 4). The groups and tools, verbatim and complete (37 tools; a test keeps
  this equal to `capability.go`):

  | Group id | Tools |
  | --- | --- |
  | `catalog` | `search_catalog` `list_artist_albums` `get_album` `get_playlist` `lookup_link` `probe_quality` |
  | `jobs` | `start_download` `list_jobs` `get_job` `wait_for_job` `cancel_job` `retry_job` `move_job` `delete_job` |
  | `library` | `list_library` `list_library_playlists` `get_resource` `get_library_playlist` `get_library_summary` `download_resource` `reset_resource` `pause_resource` `resume_resource` `delete_resource` `check_library` `get_library_check` |
  | `apple` | `list_apple_library` `list_apple_library_playlist_tracks` `sync_apple_library` `get_apple_library_status` |
  | `system` | `get_status` `get_logs` `get_config` `set_config` `list_accounts` `update_account` `delete_account` |

The chat is panel 4's `.mock` (`role="img"`, `aria-label="{apps.agents.label}"`); the
side column sits beside it inside `.stage` and is real text (the code block and the tool
list are readable and copyable).

---

## 6. Cover art

The page shows all eight albums of the demo catalog (`internal/demo/catalog.go#albums`);
today each cover is two colors, a diagonal gradient and a disc (`catalog.go#cover`). The
new covers keep each album's two colors (bg and fg) as the anchor and nothing else of the
old drawing: the page never shows the apps' two-color thumbnails, and keeping the disc's
position would make four covers the same image (a big warm disc low and left of center).
Each cover adds a concept, its own composition, texture, light and typography, so that it
reads as a designed record sleeve, and the eight differ in silhouette, not only in color.

### 6.1 The catalog

| Key | Album | Artist | Genre | Year | Tracks | bg | fg | Atmos | Main shape (where, at 40px) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `glass` | Glass Weather | Mira Solenne | Pop | 2025 | 12 | `#1f3b5c` | `#f2c14e` | yes | a big sun, low left, behind three panes |
| `tidal` | Tidal Rooms | Mira Solenne | Pop | 2022 | 10 | `#0f5257` | `#a8e6cf` | no | nested door frames, a small glow right of center |
| `tide` | Low Tide Signals | Nightjar Coast | Alternative | 2026 | 11 | `#2b2d42` | `#ef8354` | yes | a tall mast left of center with rings; a small sun top right |
| `salt` | Salt & Static | Nightjar Coast | Alternative | 2023 | 9 | `#5c2a3e` | `#f4a259` | no | a dark screen cut by a light wedge |
| `orchard` | Orchard Lights | The Velvet Orchard | Jazz | 2024 | 8 | `#3d2c1e` | `#e9c46a` | no | a gold band on top, two swags of lights over dark treetops |
| `bloom` | Late Bloom | The Velvet Orchard | Jazz | 2026 | 5 | `#264653` | `#f4d35e` | yes | a gold band on top, one flower low left |
| `summer` | 夏夜信号 | 青柠电台 | Mandopop | 2025 | 10 | `#14213d` | `#fca311` | no | a tall lit skyline filling the lower half, a thin crescent above it |
| `shore` | 海边的耳机 | 青柠电台 | Mandopop | 2023 | 9 | `#1b4332` | `#95d5b2` | no | a horizontal beach split, a dark cable curve, a half sun on the sea line |

Only Glass Weather has a big round disc: a set where most sleeves are "circle over a
horizon" reads as generated placeholders, so the other moons and suns are small (Low Tide
Signals), thin (Late Bloom, 夏夜信号), half sunk (海边的耳机) or absent (Orchard Lights).

Album ids are `920001` to `920008` in this order; artist ids `910001` (Mira Solenne),
`910002` (Nightjar Coast), `910003` (The Velvet Orchard), `910004` (青柠电台). Release dates (`releaseDate`): Glass Weather
2025-06-14, Tidal Rooms 2022-04-14, Low Tide Signals 2026-05-14, Salt & Static 2023-03-14,
Orchard Lights 2024-11-14, Late Bloom 2026-08-14, 夏夜信号 2025-04-14, 海边的耳机
2023-03-14. Album links are `https://music.apple.com/us/album/<slug>/<id>` with `slug`
lowercased, spaces to `-`, other characters dropped (`glass-weather`, `low-tide-signals`).

Track lists (verbatim; lengths from `durationMS`, shown where the page uses them):

- **Glass Weather**: Glass Weather 3:52, Paper Satellites 4:14, Slow Lightning 4:36,
  Northbound 4:59, Halo Static 3:21, Small Hours 3:44, Weathervane 4:06, Blue Hour Again
  4:28, Signal Fires 4:51, Undertow 3:48, Night Market 3:26, Afterglow (Reprise) 4:16.
- **Tidal Rooms**: Tidal Rooms 4:31, Seaglass 4:08, Low Ceilings 3:46, Hollow Bells 4:23,
  Lighthouse Keeper 4:00, Drift 3:38, Open Water 3:15, Moon Tide 3:54, Coastline 3:32,
  Harbor Song 3:06.
- **Low Tide Signals**: Low Tide Signals 3:59, Radio Silence 4:52, Static Bloom 3:15, Dune
  Lines 3:07, Faultline 3:29, Kestrel 4:22, Cold Open 4:44, Saltwater Wires 4:36, Night
  Ferry 4:58, Long Exposure 3:24, Breakwater 3:02.
- **Salt & Static**: Salt & Static, Harbor Lights, Sodium Glow, Wreckage, Steady Hands,
  Gull Cry, Tin Roof, Ember Coast, Last Ferry.
- **Orchard Lights**: Orchard Lights, Velvet Hour, Brass Moon, Late Bloom, Copper Rain,
  Quiet Waltz, Amber Room, Last Call at Nine.
- **Late Bloom**: Late Bloom, Garden Steps, Under Lanterns, Fig Tree, Evening Rounds.
- **夏夜信号**: 夏夜信号, 便利店灯光, 天台, 末班车, 汽水, 晚风收音机, 霓虹, 海报, 慢慢走, 再见夏天.
- **海边的耳机**: 海边的耳机, 潮汐表, 白色衬衫, 灯塔, 冰柠檬, 防波堤, 落日, 空房间, 回程.

Playlists (`catalog.go#playlists`; cover = an album's): Glass and Neon (AMDL Editors,
920001), Coastal Nights (Nightjar Coast, 920003), Orchard Sessions (The Velvet Orchard,
920005), 夏天的电台 (青柠电台, 920007), Signals After Dark (AMDL Editors, 920002), Slow
Lightning Radio (Mira Solenne, 920004).

### 6.2 How covers are built

- One module (call it `covers.mjs`) exports, per album key, three SVG `<symbol>`s with
  `viewBox="0 0 300 300"`: `cv-<key>` (full), `cv-<key>-m` (mid) and `cv-<key>-s`
  (small). All are generated from one list of layers: a layer marked `detail` is left out
  of the small symbol, and a layer marked `type` (the sleeve's words, catalog mark and
  dial numbers; type layers always come last) is left out of the mid and small symbols.
  The full and mid symbols share the picture, a group `cv-<key>-art` in the sprite's
  defs, so the mid symbol adds no markup but one `<use>`.
- The page carries one hidden sprite right after `<body>`: an `<svg aria-hidden="true"
  focusable="false" class="sprite">` (`.sprite { position: absolute; width: 0; height: 0;
  overflow: hidden }`, never `display: none`, which breaks gradients and filters in some
  browsers) holding `<defs>` (shared filters, patterns, gradients, the eight picture
  groups) and the 24 symbols.
- Each use is
  `<svg class="cv" viewBox="0 0 300 300" aria-hidden="true" focusable="false"><use href="#cv-glass"/></svg>`;
  size and radius come from CSS on the
  wrapper (`border-radius` + `overflow: hidden` on a wrapping `span`, since an `svg`'s
  border radius does not clip its content everywhere). Pick the variant by design size:
  full at 160 design px and above (the hero, the format card, M1's album cover), mid from
  96 to 159 (M2's live-card covers, M1's phone cover: the texture stays, the sleeve type,
  which would be 3–4px of grey noise there, goes), small below 96 (M1's sidebar card).
- Only presentation attributes (`fill`, `stroke`, `stroke-width`, `opacity`,
  `fill-opacity`, `stroke-dasharray`, `stroke-linecap`, `transform`, `font-family`,
  `font-size`, `font-weight`, `letter-spacing`, `text-anchor`, `textLength`,
  `lengthAdjust`,
  `filter="url(#…)"`, `mask="url(#…)"`, `clip-path="url(#…)"`). Never a `style`
  attribute, never `<style>` inside the SVG, never `<image>`, never `xlink:href` to
  anything but a `#fragment`. Every id is prefixed `cv-`.
- Randomness (rain, dust, stars, windows, crystals) comes from a seeded PRNG so every build
  draws the same covers: `mulberry32(seed)` with the seed = the album id as a number
  (920001…). Round coordinates to one decimal to keep the markup small.
- Text in covers is decoration of a sleeve, not page copy: only the demo names (artist,
  album), years and made-up catalog numbers. It is `aria-hidden` with the rest of the
  symbol. Latin sleeve type uses `font-family="Geist, 'Helvetica Neue', Helvetica, Arial,
  sans-serif"` unless stated; mono type `font-family="'Geist Mono', 'SF Mono', Menlo,
  Consolas, monospace"`; Chinese poster type `font-family="'Songti SC', 'STSong',
  'Noto Serif CJK SC', 'Source Han Serif SC', serif"`.
- **Shared defs** (all `cv-` prefixed):
  - `cv-grain`: `<filter x="0" y="0" width="100%" height="100%" color-interpolation-filters="sRGB">`
    `<feTurbulence type="fractalNoise" baseFrequency="0.85" numOctaves="2" seed="3"
    stitchTiles="stitch"/>` `<feColorMatrix type="matrix" values="0 0 0 0 0  0 0 0 0 0
    0 0 0 0 0  0 0 0 -1.6 1.05"/>` (dark specks). Applied to a full 300×300 rect,
    `opacity` per cover.
  - `cv-grain-light`: the same with `seed="11"` and matrix `"0 0 0 0 1  0 0 0 0 1
    0 0 0 0 1  0 0 0 1.6 -0.62"` (light specks).
  - `cv-static`: `<feTurbulence type="turbulence" baseFrequency="0.95 0.06" numOctaves="1"
    seed="5"/>` `<feColorMatrix type="saturate" values="0"/>` (horizontal streak noise).
  - `cv-ripple`: `<feTurbulence type="fractalNoise" baseFrequency="0.02 0.18"
    numOctaves="2" seed="9"/>` `<feDisplacementMap in="SourceGraphic" scale="7"
    xChannelSelector="R" yChannelSelector="G"/>` (water reflection wobble).
  - `cv-soft4`: `<feGaussianBlur stdDeviation="4"/>` (glows).
  - `cv-fade-up`: a vertical `linearGradient` (x1 0 y1 1 x2 0 y2 0) white → black, for
    masks.
- **Reading at 40px and at 300px.** At 40px (the desktop sidebar card, list rows) a cover
  must read by color and one big shape: the bg field and the main shape of the 6.1 table,
  one silhouette. Small symbols drop text, grain, hairlines, specks and
  anything under 4 units of the 300 grid. At 300px (hero center, format card) the detail
  layers (type, texture, thin lines, highlights) carry the "designed" feel. Check both by
  rendering the page at 2× and looking at the sidebar card and the hero center side by
  side, and by laying the eight small symbols in a row at 40px (9.3, review): no two may
  share a silhouette.

### 6.3 Artist house styles

Two albums per artist share a system, as real discographies do:

- **Mira Solenne** (art pop): photographic-feeling atmospheres made of soft gradients and
  light; small wide-tracked caps in the top-left (artist, then title) with a short rule
  under them; a tiny catalog mark bottom-right `MS·25` / `MS·22`.
- **Nightjar Coast** (alternative): risograph zine print: halftone dots, two inks with
  1–2px misregistration, flat shapes (no shading), a rubber-stamp box for the band name,
  rotated a few degrees; catalog `NJC-011` / `NJC-009`. The stamp (a 128×30 box, stroke
  1.5) holds `NIGHTJAR COAST` in a typewriter face, `font-family="'Courier New', 'Courier
  Prime', 'Liberation Mono', monospace"`, 10.5 units, weight 700, letter-spacing 2,
  `textLength="112"` (`lengthAdjust="spacing"`), at (x + 8, y + 20) in the ink with a pale
  copy .35 opacity offset (1.2, .8) under it.
- **The Velvet Orchard** (jazz): mid-century label: a solid fg band across the top with the
  name and title in dark condensed caps, flat geometric shapes below (circles, arcs), a
  thin offset-print outline; catalog `VO 4024` / `VO 4026`. The title is condensed on
  every system, not only where a narrow face is installed: `font-family="'Arial Narrow',
  'Roboto Condensed', 'Liberation Sans Narrow', 'Helvetica Neue', Arial, sans-serif"` at
  28 units, weight 700, squeezed with `textLength` and `lengthAdjust="spacingAndGlyphs"`
  to about 80% of a regular face's width (`ORCHARD LIGHTS` 218, `LATE BLOOM` 156), at
  (20, 54).
- **青柠电台** ("Lime Radio", Mandopop): city-pop summer poster: flat illustration, vertical
  Chinese title in a serif, a small FM dial motif, a lime dot as the artist's mark (the one
  color not from the catalog, shared by both covers: `#b8d86b`).

### 6.4 The eight covers

Coordinates are in the 300×300 viewBox. "Derived" colors: deep = bg × 0.55 per channel
(as `catalog.go#cover` darkens), lift = bg mixed 12% toward white, mid = bg and fg mixed
50/50, pale = fg mixed 55% toward white.

#### `glass`: Glass Weather — Mira Solenne

**Concept.** A late sun seen through a rain-streaked window: the title's "glass" and
"weather" in one image. Palette: bg `#1f3b5c`, fg `#f2c14e`, deep `#112032`, lift
`#3a5370`, pale `#f9e3af`.

Layers, bottom to top:

1. Sky: rect 300×300, vertical linear gradient: 0 `#112032`, .55 `#1f3b5c`, 1 `#2a4a6e`.
2. Sun glow: circle (129, 207) r 150, radial gradient `#f2c14e` opacity .38 at 0 →
   opacity 0 at 1.
3. The scene: a `<g id="cv-glass-scene">` placed in the sprite's `<defs>` (so it never
   draws by itself); it is drawn only through the `<use>`s of layer 4:
   - the sun: circle (129, 207) r 66, radial gradient (focus at (110, 188)): 0
     `#f9e3af`, .55 `#f2c14e`, 1 `#e0a63a`;
   - a far hill: path `M0 236 C60 226 110 241 170 232 S260 222 300 233 V300 H0 Z`, fill
     `#112032` opacity .92;
   - a nearer hill: path `M0 262 C70 250 130 268 200 258 S270 250 300 256 V300 H0 Z`, fill
     `#0c1828`.
4. Refraction: the cover is three vertical panes (x 0–100, 100–200, 200–300), each a
   `<use href="#cv-glass-scene">` through a `clipPath` rect of its pane: pane 1 with no
   transform, pane 2 translated (2, -1.5), pane 3 translated (-1, 1); the shifts stay
   small, so the step in the hills reads as a window mullion, not a glitch. Each `<use>`
   is blurred (`cv-glass-blur`: `feGaussianBlur` stdDeviation 1.2), so the scene sits
   behind the glass and the drops in a sharper plane. (For pane 2:
   `<use href="#cv-glass-scene" transform="translate(2 -1.5)" filter="url(#cv-glass-blur)"/>`
   inside a `<g clip-path="url(#cv-glass-pane2)">`.)
5. Pane joints: lines at x 100 and x 200, stroke `#ffffff` opacity .14, width 1. (detail)
6. Condensation (detail): 70 dots, r .5–1.1, fill `#ffffff` opacity .12–.25, uniform over
   the cover, denser in the top third (draw y as `300 * u²`).
7. Rain on the glass (detail): 12 drops from the PRNG, x in 14–286, y in 70–280, `rx`
   2.5–6. A drop is an ellipse, `ry` = 1.15 × `rx`, fill lift `#3a5370` at .55, with a 1px
   deep `#112032` rim along its upper half (opacity .6), a 1px pale `#f9e3af` crescent
   along its lower edge (an arc from 35° to 145° at .86 of the radii, opacity .55) and a
   glint (circle r = .18 × `rx`, at least .6, `#ffffff` .7) up and left of center. Two of
   every three drops have a trail above them: a filled path 2–4 wide (at most 1.2 × `rx`)
   that starts at the drop's top, runs 18–64 up and tapers to a point, wobbling up to ±2.5
   in x along two cubic curves, fill `#ffffff` at .12–.2. A trail ends at its drop, never
   in a dot.
8. Three large drops (always, also in small): drops as in layer 7 with `rx` 7 at (62, 92),
   5 at (226, 140) and 6 at (170, 58); the first and last have trails (46 and 40 long).
   The small symbol keeps the drops without trails.
9. Grain: rect with `cv-grain`, opacity .18; then `cv-grain-light` opacity .08. (detail)
10. Type (detail): `MIRA SOLENNE` at (20, 30), 11 units, weight 600, letter-spacing 2.6,
    fill `#f9e3af` opacity .92; `GLASS WEATHER` at (20, 45), 11, weight 400,
    letter-spacing 2.6, opacity .62; a rule (20, 53)–(62, 53), stroke `#f2c14e` width 1
    opacity .7; bottom-right `MS·25` at (282, 286), `text-anchor="end"`, mono 8,
    letter-spacing 1, fill `#f9e3af` opacity .55.

Small: layers 1, 2, one unclipped `<use href="#cv-glass-scene">` with no transform (in
place of layer 4), 8. At 40px: a navy square with a setting yellow sun and two pale drops.

#### `tidal`: Tidal Rooms — Mira Solenne

**Concept.** A row of doorways into receding rooms, the lower part flooded and still; a
round window glows at the end. Palette: bg `#0f5257`, fg `#a8e6cf`, deep `#082d2f`, lift
`#2c676b`, mid `#5c9c93`, pale `#d8f4e9`.

1. Ground: rect, vertical gradient 0 `#0f5257`, 1 `#082d2f`.
2. The rooms group `cv-tidal-rooms`: five nested doorframes, each a rect (fill `#082d2f`
   opacity .16, stroke `#a8e6cf`):

   | Frame | x | y | w | h | stroke width | stroke opacity |
   | --- | --- | --- | --- | --- | --- | --- |
   | 0 | 34 | 24 | 232 | 290 | 3 | 1 |
   | 1 | 74 | 60 | 172 | 230 | 2.4 | .8 |
   | 2 | 106 | 90 | 126 | 180 | 1.8 | .62 |
   | 3 | 131 | 113 | 92 | 140 | 1.4 | .48 |
   | 4 | 150 | 131 | 66 | 108 | 1.1 | .36 |

   Floor lines (detail): lines from each frame's bottom corners toward the next frame's,
   stroke `#a8e6cf` opacity .2, width .8.
3. The round window: circle (183, 163) r 19, radial gradient 0 `#ffffff`, .5 `#d8f4e9`,
   1 `#a8e6cf`; glow (drawn before the window): circle r 60 at the same center, filled with
   a radial gradient `#a8e6cf` opacity .28 at 0 → opacity 0 at 1; no blur filter.
4. The water: rect y 196–300, fill vertical gradient 0 `#0f5257` opacity .55, 1 `#082d2f`
   opacity .95, over the rooms.
5. Reflection (detail): `<use href="#cv-tidal-rooms">` mirrored about y = 196
   (`transform="translate(0 392) scale(1 -1)"`), opacity .3, with `filter="url(#cv-ripple)"`,
   clipped to y 196–300; the window's reflection: ellipse (183, 229) rx 17 ry 4.5, fill
   `#d8f4e9` opacity .5, broken into three dashes by two 2-unit gaps.
6. The waterline: line (0, 196)–(300, 196), stroke `#d8f4e9` opacity .6, width 1.2.
7. Ripples: seven horizontal dashed lines at y 206, 214, 223, 233, 246, 262, 280, stroke
   `#a8e6cf`, opacity .38 → .18 (nearer = fainter), widths .8 → 1.4, dash patterns
   `"14 9"`, `"18 10"`, `"22 12"`, `"26 14"`, `"30 16"`, `"36 18"`, `"44 22"`, each with a
   different `stroke-dashoffset` (PRNG) so they do not align. (y 206 and 223 only in
   small.)
8. Grain .16 + light grain .06. (detail)
9. Type (detail): Glass Weather's type, `MIRA SOLENNE` / `TIDAL ROOMS`, fill `#d8f4e9`, and
   `MS·22` bottom-right, but placed at (46, 39) and without the rule: at (20, 30) the
   words would cross doorframe 0's top-left corner (x 34, y 24), and inside the frame the
   rule would read as another frame line.

Small: layers 1; 2 with frames 0, 2 and 4 only (no floor lines); 3 without the glow; 4;
6; 7 with the ripples at y 206 and 223 only. At 40px: teal, nested frames, a mint dot
right of center over a pale horizontal line.

#### `tide`: Low Tide Signals — Nightjar Coast

**Concept.** A radio mast on a flat shore at low tide, its signal rings spreading over a
halftone sky; a small late sun high on the right throws a thin column of light down the
wet sand. The mast, not the sun, is the image. Printed as a two-ink risograph. Palette:
bg `#2b2d42`, fg `#ef8354`, deep `#171824`, lift `#444659`, pale `#f8c7b2`.

1. Paper: rect fill `#2b2d42`.
2. Halftone sky (detail): a pattern `cv-tide-dots` (`patternUnits="userSpaceOnUse"`, 5×5,
   `patternTransform="rotate(22)"`, a circle (2.5, 2.5) r 1.15 fill `#ef8354`), on a rect
   y 0–214, masked by `cv-fade-up` (dense at the horizon, gone by y 40), opacity .55.
3. Horizon: line (0, 214)–(300, 214), stroke `#f8c7b2` opacity .7, width 1.
4. Sun: circle (232, 78) r 22, fill `#ef8354`; glow: circle r 46 at the same center,
   radial gradient `#ef8354` opacity .35 → 0. Misregistration (detail): the same circle
   outline, stroke `#f8c7b2` width 1.2 opacity .45, translated (2, -1.4).
5. The sun's column on the wet sand: seven bars centered on x 232, height 2.4, `rx` 1.2,
   fill `#ef8354`: (y, width, opacity) = (220, 30, .85), (228, 26, .72), (237, 22, .6),
   (247, 17, .5), (258, 12, .42), (270, 8, .35), (283, 5, .3).
6. Tide ridges (detail): four paths across the sand,
   `M0 230 C80 224 150 238 300 228`, `M0 248 C90 242 180 258 300 246`,
   `M0 268 C70 263 200 277 300 265`, `M0 289 C100 283 190 297 300 287`, stroke `#f8c7b2`
   opacity .25, width 1.
7. The mast (the main shape), centered on x 104 from the shore (y 214) to its top (y 58):
   - legs (88, 214)–(104, 58) and (120, 214)–(104, 58), stroke `#f8c7b2` width 2;
   - bracing (detail): an X between the legs every 16 units of height from y 206 up to
     y 74, stroke `#f8c7b2` width .9 opacity .8;
   - a platform line (94, 120)–(114, 120), width 1.6; two dish antennas (detail): half
     ellipses rx 5 ry 7, open side outward, at (95, 100) facing left and (113, 138) facing
     right, stroke `#f8c7b2` width 1;
   - guy wires (detail): from (104, 80) to (30, 214) and to (178, 214), stroke `#f8c7b2`
     opacity .35 width .8;
   - the hut at its foot: rect (80, 202) 48×12, fill `#171824`, stroke `#f8c7b2` width 1;
   - the beacon: circle (104, 56) r 3.4 fill `#ef8354`, glow circle r 10 opacity .35;
   - its reflection (detail): a dashed vertical line x 104 from y 218 to 290, stroke
     `#f8c7b2` opacity .3 width 1.2, dash `"6 5"`, `filter="url(#cv-ripple)"`.
8. Signal arcs from the beacon (104, 56): radii 20, 38, 58, 80, 104; each radius two arcs
   (left and right, from -50° to +50° around the horizontal), stroke `#ef8354`, widths
   2.2, 2, 1.8, 1.6, 1.4, opacity 1, .8, .6, .42, .28; the two outermost dashed `"3 4"`.
   (The right arc of radius 104 peaks at x 208, clear of the sun's left edge at x 210.)
9. Ink speckle (detail): 40 small irregular dots (r .6–1.4) of `#2b2d42` scattered over
   the sun, the bars and the arcs between x 60 and 230, y 20 and 120 (paper showing
   through ink).
10. Grain .22 + light grain .1. (detail)
11. Type (detail), on the sand at the lower left: a stamp group rotated -4° about
    (82, 251): rect (18, 236) 128×30, no fill, stroke `#ef8354` width 1.5;
    `NIGHTJAR COAST` at (26, 256) in the stamp face (6.3), fill `#ef8354`; a
    misregistered copy of the text in `#f8c7b2` opacity .35 offset (1.2, .8) under it.
    Title `LOW TIDE SIGNALS` at (18, 288), 9, weight 700, letter-spacing 2, fill
    `#f8c7b2` opacity .82. `NJC-011` at (282, 288), end-anchored, mono 8, fill `#f8c7b2`
    opacity .6.

Small: 1; a flat glow (an ellipse at (104, 214) rx 120 ry 70, radial `#ef8354` .3 → 0,
clipped to y < 214); 3; 4 without the outline; bars 1–4 of layer 5; the mast's legs and
hut with the legs at width 3; arcs of radius 20, 38 and 58 at width 3. At 40px: slate,
a pale mast left of center inside orange rings, a small orange sun top right.

#### `salt`: Salt & Static — Nightjar Coast

**Concept.** A harbor light breaking through TV static, salt crystals on the ledge in front.
Palette: bg `#5c2a3e`, fg `#f4a259`, deep `#321722`, lift `#704455`, pale `#fad5b4`.

1. Paper: rect fill `#5c2a3e`.
2. Screen: rounded rect (34, 40) 232×184, `rx` 26, fill `#321722`; a bezel stroke
   `#fad5b4` opacity .22 width 1; an inner rect inset 6 (`rx` 21), stroke `#000` opacity
   .25 width 2 (detail).
3. Static (detail; in small, six horizontal lines `#fad5b4` opacity .25 at y 60–200): a
   rect over the screen with `filter="url(#cv-static)"`, opacity .5, clipped to the
   screen, masked out across the clear band (step 4).
4. The clear band, y 150–206 inside the screen: a gradient from `#321722` (top) to
   `#5c2a3e` (bottom) (the sea at night); a horizon line at y 190, `#fad5b4` opacity .35;
   the lighthouse at x 204: tower path `M197 206 L200 166 L208 166 L211 206 Z` fill
   `#1f0e15`; gallery rect (196, 160) 16×4; lamp room rect (199, 150) 10×10 fill
   `#1f0e15`; the lamp circle (204, 155) r 3.4 fill `#f4a259`, glow r 10 opacity .5.
5. The beam: polygon (204, 155), (34, 128), (34, 186), fill linear gradient from
   `#f4a259` opacity .85 at the lamp to 0 at x 34, clipped to the screen (it crosses the
   static: the beam is drawn after the static, so it cuts through it).
6. Scanlines (detail): pattern 300×3 with a 1-unit line `#000` opacity .2, over the screen.
7. Glare (detail): ellipse (110, 78) rx 70 ry 26 rotated -12°, fill `#ffffff` opacity .06.
8. The ledge: rect y 236–300 fill `#4a2132`; its edge line y 236 stroke `#fad5b4` opacity
   .2.
9. Salt: 26 crystals from the PRNG on y 244–296: squares 2.5–6.5 wide, rotated 0–45°,
   fill `#fad5b4` opacity .5–.9 (detail), masked out of the stamp's box (`cv-salt-nostamp`:
   a black rect (148, 247) 140×40 rotated 3° about (218, 267)) so the stamp's letters print
   clean; three cubes always, printed flat in the two inks like the rest of the zine: the
   cube's outline (top and sides as one hexagon) in pale `#fad5b4`, set off by (1.5, 1.5)
   for misregistration, then the top face in `#f4a259`; no shaded third face. At
   (62, 262) size 14, (118, 276) size 10 and (150, 288) size 7 (the middle cube sits left
   of the stamp, which covers x 154–282 on the ledge).
10. Grain .22 + light grain .1. (detail)
11. Type (detail): the stamp group rotated +3° (box (154, 252) 128×30, so it sits on the
    ledge at the right), `NIGHTJAR COAST`; `SALT & STATIC` at (18, 26), 9, weight 700,
    letter-spacing 2, fill `#fad5b4` opacity .82; `NJC-009` at (282, 26) end-anchored.

Small: 1, 2, the six lines, 4 (without glow), 5, the three cubes. At 40px: plum with a dark
screen cut by an amber wedge.

#### `orchard`: Orchard Lights — The Velvet Orchard

**Concept.** String lights swagged over a night orchard. The two garlands are the image and
carry all the light; there is no moon. Palette: bg
`#3d2c1e`, fg `#e9c46a`, deep `#211810`, lift `#544539`, mid `#937844`, pale `#f5e4bc`.

1. Ground: rect fill `#3d2c1e`.
2. The top band: rect (0, 0) 300×64 fill `#e9c46a`. (In small too, without text.)
3. (No moon: it would make an eighth disc in the set; 6.1.)
4. Back row of trees: circles r 22 at y 182, x 24, 76, 128, 180, 232, 284, fill `#544539`.
5. Middle row: circles r 28 at y 214, x 0, 62, 124, 186, 248, 310, fill `#4a3828`.
6. Front row: circles r 36 at y 252, x 30, 112, 194, 276, fill `#211810`; trunks: rects 6
   wide from each canopy's bottom (y 288) to 300, fill `#211810`; ground rect y 262–300
   fill `#211810`.
7. Garlands (the main shape): two quadratic paths,
   `M0 120 Q75 160 150 128 Q225 98 300 136` and `M0 150 Q100 196 200 160 Q255 140 300 162`,
   stroke `#f5e4bc` opacity .6 width 1.2. Bulbs every 20 units of arc length along each
   (compute by sampling the curve): circle r 3 fill `#e9c46a`, a glow circle r 9 fill
   radial `#e9c46a` .55 → 0, and a 1-unit cap line above each bulb in `#211810`.
8. Grain .2 + light grain .08. (detail)
9. Type in the band (detail): `THE VELVET ORCHARD` at (20, 25), 10, weight 700,
   letter-spacing 2.2, fill `#211810`; `ORCHARD LIGHTS` at (20, 54) in the house title
   face (6.3: 28 units, weight 700, `textLength="218"`), fill `#211810` (it ends at
   x 238, so it stays whole where the hero's Tidal Rooms covers the sleeve's right edge);
   `VO 4024` at (280, 25), end-anchored, mono 9, fill `#211810`; a thin rule under the
   band at y 64, `#211810` width 1.5.

Small: 1, 2, 6, then both garlands at wire width 2 with bulbs every 40 units, r 6, no
glows. At 40px: a gold band on top, two swags of gold dots over dark trees.

#### `bloom`: Late Bloom — The Velvet Orchard

**Concept.** One flower opening at night, five petals for the five tracks, the last petal
still half-closed. Palette: bg `#264653`, fg `#f4d35e`, deep `#14262d`, lift `#405c68`,
mid `#8d8d59`, pale `#faebb7`.

1. Ground: rect fill `#264653`.
2. Rings (detail): circles centered (90, 198) r 40, 70, 100, 130, 160, no fill, stroke
   `#faebb7` opacity .07 width 1.
3. The top band, as Orchard Lights, fill `#f4d35e`.
4. Stem: path `M90 210 C96 240 104 268 122 300`, stroke `#8d8d59` width 3; a leaf: ellipse
   (106, 254) rx 18 ry 7 rotated 35°, fill `#8d8d59`; a second leaf (detail): ellipse
   (100, 278) rx 13 ry 5 rotated -25°.
5. Petals around (90, 198): ellipses rx 26 ry 13, centered 24 units out from the center
   at angles -90°, -18°, 54°, 126° (rotated to point outward), fill `#f4d35e`, the second
   and fourth `#faebb7`; the fifth at 198°: rx 18 ry 9, 20 units out, fill `#f4d35e`
   opacity .78 (the late one). Offset outlines (detail): each petal's outline, stroke
   `#14262d` opacity .3 width 1, translated (1.5, 1).
6. Center: circle r 9 fill `#14262d`; eight stamen dots r 1.6 fill `#faebb7` at radius 13
   (detail).
7. Crescent moon: circle (238, 104) r 20 fill `#faebb7`, masked by a circle (246, 98) r 18
   (a crescent).
8. Stars (detail): nine 4-point sparkles (two crossing 1-unit lines 5–9 long) in the upper
   right (x 160–290, y 76–170), stroke `#faebb7` opacity .5–.85.
9. Grain .18 + light grain .07. (detail)
10. Type in the band (detail): `THE VELVET ORCHARD`, `LATE BLOOM` (`textLength="156"`),
    `VO 4026`, as Orchard Lights, in `#14262d`.

Small: 1, 3, 4 (stem and first leaf), 5 (no outlines), 6 (center only), 7. At 40px: a
yellow band, a yellow five-petal flower low left on teal.

#### `summer`: 夏夜信号 — 青柠电台

**Concept.** A rooftop on a summer night: a tall, lit city fills the lower half, a thin
orange crescent above it, a TV antenna sending signal arcs, the FM dial along the bottom. The
skyline is the image. Palette: bg `#14213d`, fg `#fca311`,
deep `#0b1221`, lift `#303c54`, mid `#886227`, pale `#fed694`, lime `#b8d86b`.

1. Sky: rect, vertical gradient 0 `#0b1221`, .6 `#14213d`, 1 `#1d2f55`.
2. Stars (detail): 30 dots from the PRNG in y < 130, outside the moon's halo, r .6–1.3,
   fill `#fed694` opacity .5–.9; one 4-point star at (40, 52).
3. Moon, a thin crescent so the skyline dominates: circle (178, 90) r 34 fill `#fca311`,
   masked by a black circle (184, 84) r 33 (`cv-summer-crescent`; about 8 units at its
   widest, lit on the lower left); halo: circle r 52 radial `#fca311` .16 → 0.
4. Skyline (the main shape): one path of rectangular roofs standing on y 250 across the
   whole width (PRNG widths 14–34; heights 40–110, so roofs reach from y 210 up to y 140,
   just under the moon; right of x 240 at most 70, so the vertical title stands on sky),
   with a water tower and two spires among them, fill `#0b1221`; a second, farther row
   behind it (heights 20–60 above the first row's line, fill `#1d2f55`, detail).
5. Windows: 3×4 rects on a 7×9 grid inside the near buildings, 35% lit (PRNG), fill
   `#fed694` (a few `#fca311`) opacity .85 (detail). In small: 14 of the lit windows (the
   first 14 of the PRNG list) drawn 6×7, so the city still shows light at 40px.
6. Rooftop: rect y 248–300 fill `#0b1221`; its edge line y 248 stroke `#303c54` width 1.
   A water tank (detail): rect (214, 222) 36×26 `rx` 3 fill `#14213d` stroke `#303c54`, two
   legs to 248.
7. Antenna at x 72: mast (72, 300)–(72, 170), stroke `#fed694` width 1.4; crossbars at
   y 176 (34 long), 186 (28), 196 (22), centered on the mast, width 1.4.
8. Signal arcs from (72, 170), right side only, radii 14, 26, 40, stroke `#fca311` width
   1.4, opacity .9, .6, .35.
9. FM dial (detail; drawn after the grain and counted as type, so the mid symbol leaves it
   out): baseline (12, 290)–(288, 290) stroke `#fed694` opacity .5; ticks every
   6.9 units (41 ticks) 3 tall, every fifth 7 tall; labels `88` `92` `96` `100` `104`
   `108` above the tall ticks at y 283, mono 6, fill `#fed694` opacity .7; the needle at
   98.6: x 158.3, line (158.3, 277)–(158.3, 298), stroke `#fca311` width 1.6; `FM` at
   (12, 283), mono 6, weight 700.
10. Grain .2 + light grain .08. (detail; drawn before the dial)
11. Type (detail): the title vertical at the right, one `<text>` per character at x 272,
    y 46, 74, 102, 130: `夏` `夜` `信` `号`, Chinese serif, 24, weight 700, fill `#fed694`,
    `text-anchor="middle"`; the artist vertical at x 250, y 44, 56, 68, 80: `青` `柠` `电`
    `台`, 10, weight 600, fill `#fca311`; the lime dot: circle (250, 32) r 3 fill
    `#b8d86b`.

Small: 1, 3 (a thicker crescent: the mask circle at (188, 81) r 30, no halo), 4 (near row
only), the 14 large windows of 5, 6 (no tank), 7, arcs 14 and 26. At 40px: navy, a dark
skyline with lit windows over the lower half, an orange crescent above it.

#### `shore`: 海边的耳机 — 青柠电台

**Concept.** Wired earphones left on the sand by the sea; the cable runs off and becomes the
edge of a wave. Palette: bg `#1b4332`, fg `#95d5b2`, deep `#0e241b`, lift `#365a4b`, mid
`#588c72`, pale `#cfecdc`, lime `#b8d86b`.

1. Sky: rect y 0–132, vertical gradient 0 `#1b4332`, 1 `#365a4b`.
2. Sun, low and half under the sea line, so the cable stays the main shape: circle
   (225, 132) r 40 fill `#cfecdc` and a halo r 64 radial `#cfecdc` .3 → 0, both clipped
   to the sky (`cv-shore-sky`, y < 132).
3. Sea: rect y 132–176 fill `#588c72`; horizon line y 132 stroke `#cfecdc` opacity .6;
   wave lines (detail): five short horizontal dash runs at y 140, 147, 155, 163, 170,
   stroke `#cfecdc` opacity .35 width 1, dash `"10 7"`, `"14 8"`, `"18 9"`, `"22 10"`,
   `"26 12"`; the sun's glitter (detail): six short pale bars under the sun, centered x
   225, widths 30 → 8.
4. Sand: rect y 176–300 fill `#95d5b2`; texture (detail): a dot pattern (5×5, circle r .7
   fill `#588c72`) opacity .35.
5. Foam: a scalloped path along y 176 (`M0 178 Q12 171 24 178 T48 178 …` to 300), stroke
   `#cfecdc` width 3, round joins; under it a thin wet band (detail): the same path
   translated (0, 5), stroke `#588c72` opacity .4 width 2.
6. Earbuds (both `#0e241b`, highlights `#cfecdc` opacity .6): bud 1 at (96, 236), a circle
   r 11 and a stem (rounded rect 6×22 `rx` 3) rotated 25° from the circle's lower right;
   bud 2 at (140, 252), the same rotated -40°; each with a highlight arc (r 7, 100°) on the
   upper left and a mesh dot grid (detail, 3×3 dots r .7 pale .5) on its face.
7. Cable: from each stem's end, 2.2 wide `#0e241b` lines to a Y-splitter (a 4×8 rounded
   rect) at (122, 270); one cable on: `M122 274 C170 292 210 252 236 222 S268 170 300 178`,
   width 2.2, stroked with a linear gradient along x: `#0e241b` up to x 230, then to
   `#cfecdc` at x 280 (the cable turns into foam where it meets the water).
8. Shell (detail): a fan at (238, 262): five ridges from a hinge, fill `#cfecdc` stroke
   `#588c72` width .8.
9. Grain .16 + light grain .06. (detail)
10. Type (detail): the title vertical at the left, x 26, y 40, 66, 92, 118, 144:
    `海` `边` `的` `耳` `机`, Chinese serif, 20, weight 700, fill `#cfecdc`, middle-anchored;
    the artist vertical at x 48, y 40, 52, 64, 76: `青` `柠` `电` `台`, 9, fill `#95d5b2`;
    the lime dot (48, 29) r 3 `#b8d86b`; `FM 98.6` at (282, 22), end-anchored, mono 7,
    fill `#cfecdc` opacity .7.

Small: 1, 2, 3 (no lines), 4 (flat), 5, 6 (no mesh), 7. At 40px: green over mint, a pale
half sun on the horizon right of center, a dark cable line.

### 6.5 Where each cover appears

| Place | Albums | Variant | Size |
| --- | --- | --- | --- |
| Hero shelf | orchard, tidal, glass, tide, summer | full | 190–300 |
| Hero shelf, phone | tidal, glass, tide | full | 150–200 |
| Formats card | glass | full | 320 (240 phone) |
| M1 desktop | glass (header), tide (sidebar card) | full / small | 216 / 40 |
| M1 desktop, phone | glass | mid | at most 120 |
| M2 console | tidal, tide, summer | mid | 136 (96 phone) |
| OG card | glass, tidal, tide | full | 320, 240, 240 |

---

## 7. Fact sheet

Every claim the page may make. The copy brief (section 8) refers to these ids. A claim not
in this table does not go on the page.

**Status first.** AMDL 2.0 is not released yet: `v2.0.0-rc.13` is its last release
candidate (`.github/release-notes/v2.0.0-rc.13.md`) and the site's repository links follow
`dev` (`apps/site/build.mjs#REF`). Every claim below describes 2.0; the page says 2.0 is in
development until release day (section 9). Facts marked **[unreleased]** are announced
for a later version and must be phrased as such.

| Id | Fact | Source |
| --- | --- | --- |
| F01 | AMDL (`amdl`) downloads music from Apple Music to your own disk: it resolves a link, downloads the encrypted media, decrypts it, remuxes and tags it, and saves finished `.m4a` files | `README.md` (opening), `AGENTS.md` |
| F02 | It takes song, album, playlist, artist and radio station links, or a search of the catalog. Up to 100 links at once | `README.md` (opening), `internal/capability/capability.go#All` (`start_download`), `docs/cli.md#commands` |
| F03 | Self-hosted, for one person: each install serves one user; there are no user accounts and no auth (access control belongs to whatever runs in front) | `README.md#limits`, `AGENTS.md` |
| F04 | It needs your own Apple Music subscription | `README.md#products` ("What the server needs") |
| F05 | It needs a wrapper: the service that holds your Apple Music login. amdl holds no Apple account and no keys | `README.md#how-it-works`, `docs/install.md#the-wrapper` |
| F06 | Decryption runs inside amdl, in-process (a pure-Go translation of Temari, MIT) | `README.md#how-it-works`, `README.md#license` |
| F07 | No decrypted copy of a whole track is written to disk: decrypted samples are piped straight into ffmpeg | `README.md#how-it-works` |
| F08 | Catalog reads use Apple's undocumented web-player API, which can change without notice; no Apple developer key is needed | `README.md#how-it-works`, `README.md#limits` |
| F09 | Audio only: music videos are not supported; live radio such as Apple Music 1 is not supported; a station is a rolling list of what Apple offers now | `README.md#limits` |
| F10 | License: AGPL-3.0 | `LICENSE`, `README.md#license` |
| F11 | Repository: `https://github.com/AMDL-Web/apple-music-downloader` | `apps/site/build.mjs#REPO` |
| F12 | 2.0 is in development (release candidates; rc.13 is the last) **[unreleased]** | `.github/release-notes/v2.0.0-rc.13.md`, `apps/site/build.mjs#REF` |
| F13 | Codecs, in a priority list you order: `alac` (Apple Lossless), `ec3` (Dolby Digital Plus / Dolby Atmos), `ac3` (Dolby Digital), `aac`, `aac-binaural`, `aac-downmix`. AAC-LC is always appended as the last fallback and is not listed | `configs/config.example.yaml#download.quality_priority` |
| F14 | Default priority `["alac"]`. With `codec_alternative` on (default), a codec that runs out of attempts hands over to the next; each codec has its own attempts | `configs/config.example.yaml#download.quality_priority`, `configs/config.example.yaml#download.codec_alternative`, `README.md#how-it-works` |
| F15 | ALAC up to 24-bit / 192 kHz by default; a lower ceiling can be set. Apple serves ALAC at 44.1 kHz and above, 16- and 24-bit | `configs/config.example.yaml#download.alac_max_sample_rate`, `configs/config.example.yaml#download.alac_max_bit_depth` |
| F16 | The apps label qualities `ALAC 24/96`, `Atmos`, `Dolby Digital`, `AAC 256`, `AAC Binaural`, `AAC Downmix`; the long form is `ALAC 24-bit · 96 kHz`. AAC is 256 kbps | `internal/format/format.go#Quality`, `internal/format/format.go#LongQuality` |
| F17 | Cover art is embedded, 5000×5000 JPEG by default; album and playlist covers are also saved as `cover.jpg` next to the music, and the artist's picture as `artist.jpg` in the artist folder (when Apple has one) | `configs/config.example.yaml#download.cover_size`, `configs/config.example.yaml#download.embed_cover`, `configs/config.example.yaml#download.save_album_cover`, `configs/config.example.yaml#download.save_artist_cover`, `docs/download-pipeline.md#standalone-covers`, `GLOSSARY.md` (Standalone cover) |
| F18 | Lyrics are embedded as LRC, timed by line or by word; translation and pronunciation (romanization) can be added; `.lrc` or `.ttml` files can be saved beside the track | `configs/config.example.yaml#download.embed_lyrics`, `configs/config.example.yaml#download.lyrics_timing`, `configs/config.example.yaml#download.lyrics_extras`, `configs/config.example.yaml#download.lyrics_files` |
| F19 | Each track is checked for integrity after download (on by default) | `configs/config.example.yaml#download.check_integrity` |
| F20 | Songs, albums and artists share one folder tree: `library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}` by default; playlists go to `playlists/{PlaylistName}/…` and stations to `stations/{StationName}/…`. The templates are configurable | `configs/config.example.yaml#download.library_path_format`, `configs/config.example.yaml#download.playlist_path_format`, `configs/config.example.yaml#download.station_path_format` |
| F21 | A file already at its path is adopted, not downloaded again (unless `download.overwrite_existing` is on); downloading again replaces a file only once the new one is complete | `README.md#where-files-land`, `README.md#how-it-works`, `configs/config.example.yaml#download.overwrite_existing` |
| F22 | One binary, `amdl`: the command line, the MCP server, the terminal interface and the backend with its web console | `docs/install.md` (opening), `README.md#command-line` |
| F23 | Desktop app: native, for macOS (Apple silicon and Intel), Windows and Linux (amd64; needs GTK 3); attached to each release as `.dmg`, setup `.exe`, `.deb` and `.tar.gz`. Packages are not signed yet: macOS and Windows each stop the first launch once | `docs/install.md#the-desktop-app`, `README.md#desktop-app` |
| F24 | The desktop app is a client of an AMDL server, or standalone with the backend inside the app; standalone it shares one backend and history with `amdl service` on the same machine. On first launch it asks where downloads run, and its setup screen installs ffmpeg, QEMU where needed and the wrapper | `README.md#desktop-app`, `docs/install.md#the-desktop-app` |
| F25 | Web console: the browser client the server serves at `/console/` on the API's port, with nothing to install on the device that opens it; it uses only the public API. Light and dark. The Docker image publishes it on port 18080 for the network. A binary install stays on this machine (loopback): `amdl serve` on port 18080, `amdl service install` on a port it picks at install (`amdl service status` prints the address). Never write the loopback IP address on the page (the build rejects it); say "this machine" | `README.md#web-console`, `configs/config.example.yaml#server.listen`, `docs/cli.md#background-service`, `apps/console/skins/music.md#tokens` |
| F26 | The console's pages: New download, Overview, Activity, Library (your Apple Music library, when switched on), Settings, Logs; a command palette and keyboard shortcuts; a phone layout with bottom tabs | `apps/console/static/js/lib/palette.js#STATIC_ITEMS`, `apps/console/skins/music.md#components` |
| F27 | Terminal UI: `amdl` with no arguments (or `amdl tui`); prints into the terminal's scrollback; paste a link or type to search; up to three jobs as cards with an album bar and each track's stage; `/` commands such as `/quality`, `/queue`, `/library`, `/jobs`, `/retry`, `/logs`, `/login`, `/help`; follows a light or dark terminal; with `NO_COLOR` the bars still read by shape | `docs/cli.md#terminal-interface` |
| F28 | Command line: every result is JSON on stdout, errors are JSON on stderr with an exit status per kind of failure; `amdl cli search "<task>"` finds the command for a task; `--dry-run` on changes | `README.md#command-line`, `docs/cli.md#commands` |
| F29 | MCP: `amdl mcp` serves 37 tools over stdio; `amdl cli setup` prints the config for Claude Code, Codex or any `mcpServers` client and an agent skill; no tool signs the wrapper in, so an Apple ID password never passes through a model | `docs/cli.md#mcp-server`, `docs/cli.md#agent-setup`, `internal/capability/capability.go#All` |
| F30 | An agent goes from a search to a download in two calls (`search_catalog`, then `start_download`) and follows the job with `wait_for_job` | `docs/cli.md#mcp-server` |
| F31 | HTTP API: REST and Server-Sent Events under `/api/v1` on the console's port; the OpenAPI spec is `internal/api/openapi.yaml`; no CORS headers (the console is its only browser client) | `README.md#api` |
| F32 | Hooks: a webhook or a command when a job is queued, finishes, fails or is cancelled. Presets: ntfy, Bark and Telegram notifications; Plex, Jellyfin and Emby library refreshes; Custom. Commands are edited only in the file. Off by default | `README.md#automation`, `docs/automation.md#job-hooks`, `internal/hookpreset/hookpreset.go#All`, `apps/console/static/js/lib/hook-presets.js#HOOK_EVENTS` |
| F33 | Docker: the image `ghcr.io/amdl-web/apple-music-downloader` for linux/amd64 and linux/arm64, with ffmpeg inside, on port 18080; the Compose file is `deploy/docker-compose.yml`, which carries the wrapper-manager service commented out; `PUID`/`PGID` default 1000:1000 | `deploy/docker-compose.yml`, `README.md#quick-start` |
| F34 | Binary install: `install.sh` (macOS, Linux) checks the release against `SHA256SUMS` and installs `amdl` into `~/.local/bin`; `install.ps1` (Windows) into `%LOCALAPPDATA%\Programs\amdl`. Releases build macOS and Linux for amd64 and arm64 and Windows for amd64; Windows on ARM runs the amd64 build under emulation | `deploy/install/install.sh`, `deploy/install/install.ps1`, `docs/install.md#install-the-binary`, `Makefile#DIST_TARGETS` |
| F35 | Homebrew and Scoop packages arrive with 2.0.1 **[unreleased]** | `docs/install.md#homebrew-and-scoop` |
| F36 | ffmpeg 8 or later; `amdl doctor` checks the server, ffmpeg, the wrapper and the service and prints the command that installs what is missing | `internal/media/ffmpeg.go#RecommendedFFmpegMajor`, `docs/install.md#ffmpeg`, `docs/install.md#run-the-backend` |
| F37 | `amdl wrapper install` gets a wrapper (wrapper-manager, checked against a pinned hash) and runs it natively on Linux x86_64 with user namespaces, in a QEMU guest elsewhere; not on Windows on ARM | `docs/install.md#a-wrapper-amdl-runs`, `README.md#command-line` |
| F38 | `amdl service install` runs the backend as a service that starts at login (LaunchAgent, systemd user unit or Task Scheduler task); `amdl service status` prints the console's address. With a wrapper on another machine, pass its address: `amdl service install --wrapper <url>`. The wrapper is signed in on the console's Account page, with the terminal UI's `/login`, or with `amdl wrapper login` | `docs/install.md#run-the-backend`, `docs/install.md#a-wrapper-amdl-runs`, `docs/cli.md#background-service` |
| F39 | The wrapper's native and Docker launchers (the Compose file's wrapper-manager service included) run on Linux x86_64 (amd64) and need user namespaces (`unshare -Ur true` must succeed); on arm64, or on a host without user namespaces such as Synology DSM, use wrapper-manager v2's QEMU variant, which needs `/dev/kvm` | `README.md#products` ("What the server needs"), `docs/deployment.md#the-wrapper`, `deploy/docker-compose.yml` (wrapper-manager comment) |
| F40 | wrapper-manager v2 holds up to 10 Apple Music accounts and has the login the console's Account page uses (two-step codes are asked for there) | `docs/deployment.md#the-wrapper`, `GLOSSARY.md` (wrapper-manager) |
| F41 | Docker sign-in: open `http://localhost:18080/`, Settings › Account, set Session service to `http://wrapper-manager:8080`, save and restart from the notice, sign in with the Apple ID, paste a link into New download; files land in `./data/downloads` | `README.md#quick-start` |
| F42 | Binary installs save music to `~/Music/AMDL` (`%UserProfile%\Music\AMDL` on Windows) | `docs/install.md#where-files-go` |
| F43 | Until 2.0.0 is released, the install script and the `latest` image mean an older release: pass a 2.0 release candidate's tag with `--version` (`-Version` on Windows), for example `v2.0.0-rc.13`, and in the Compose file replace `latest` with the same version without the `v` (`2.0.0-rc.13`; images are tagged without it). A database written by rc.1 to rc.7 cannot be opened **[unreleased: removed on release day]** | `deploy/install/install.sh` (`--version`), `.github/workflows/docker-publish.yml` (`type=semver,pattern={{version}}`), `docs/deployment.md`, `apps/site/README.md#release-day` |
| F44 | Coming from 1.x: the database migrates on first start and 1.x cannot read it afterwards; back it up and read the upgrade guide first | `README.md#upgrading-from-1x`, `docs/upgrading-to-2.0.md` |
| F45 | The apps are in English; this site and the README are in English and Simplified Chinese | `apps/console/DESIGN.md#principles`, `README.zh-CN.md` |

Link targets the page uses (all built from `REPO` and `BLOB` = `{repo}/blob/${REF}` in
`build.mjs`; content strings may write `{repo}` and `{blob}`):

| Target | Where |
| --- | --- |
| `{repo}` | hero (Runs-on row), header, footer |
| `{repo}/releases` | footer |
| `{repo}#web-console` | apps panel 2 |
| `{blob}/LICENSE` | footer |
| `{blob}/docs/README.md` | header Docs, footer |
| `{blob}/docs/install.md` | CTA, footer, install.more |
| `{blob}/docs/install.md#the-desktop-app` | apps panel 1 |
| `{blob}/docs/cli.md` | footer |
| `{blob}/docs/cli.md#terminal-interface` | apps panel 3 |
| `{blob}/docs/cli.md#mcp-server` | apps panel 4 |
| `{blob}/docs/api.md` | apps links line, footer |
| `{blob}/docs/automation.md` / `#job-hooks` | footer / apps links line |
| `{blob}/docs/deployment.md` / `#the-wrapper` | footer, install.more / NAS callout |
| `{blob}/docs/download-pipeline.md` | how.more |
| `{blob}/docs/upgrading-to-2.0.md` | footer, install.more |
| `${RAW}/deploy/install/install.sh`, `install.ps1`, `deploy/docker-compose.yml` | install code |

---

## 8. Copy brief

The copywriter gets one file: the JSON object of 8.2, which holds the tone rules (`tone`),
the text of every fact (`facts`, the "Fact" column of section 7) and the keys (`keys`).
Nothing else is needed. The answer is two JSON files, `en.json` and `zh-CN.json`, with
exactly these keys (dots are nesting; a numeric segment is an array index, so
`how.amdl.steps.0.name` is `how.amdl.steps[0].name`), every leaf a non-empty string.

Per key: `purpose`; `facts` (ids into `facts`); `maxChars` (en) and `maxCharsZh` (zh-CN),
counting visible text only (below); `marks`, the inline marks the key may use (`code`,
`strong`, `link`; an empty list means plain text); `links`, the hrefs a `link` mark may
use; `verbatim`, strings that must appear exactly so in both languages; `lang: "other"`
on the two keys written in the other language. `maxCharsZh` is `ceil(maxChars × 0.6)`
plus the length of the key's `verbatim` strings (Latin text costs as much in Chinese as in
English), except for the `lang: "other"` keys, where it equals `maxChars`.

**Counting.** Before counting, replace every `[text](href)` with its text and delete
every backtick and every `**`; then count Unicode code points (`[...s].length`). A
full-width character counts as one.

### 8.1 Tone guide

The same rules are the `tone` array at the top of the JSON in 8.2, which is what the
copywriter gets; they are repeated here to be read.

- You are writing the words for the website of AMDL, an open-source, self-hosted tool that
  downloads music from Apple Music to the user's own disk as finished, tagged files.
- Restrained and plain. Say what the thing does, in the fewest words that are true. No hype,
  no superlatives ("best", "ultimate", "blazing", "seamless", "powerful", "effortless"), no
  exclamation marks (neither ! nor ！), no rhetorical questions in body text, no emoji.
- Short. Every key has two hard limits in characters: `maxChars` for `en.json` and
  `maxCharsZh` for `zh-CN.json`. Only visible text counts: `[text](href)` counts as its
  text, and the `` ` `` and `**` marks themselves do not count.
- Concrete. Prefer a fact (a format, a number, a command) to an adjective. Use only the
  facts listed for each key (the facts object holds their text); do not add claims. Numbers
  and names exactly as given.
- Strings a key lists in verbatim must appear exactly so, in both languages.
- Calm second person ("you") where a subject is needed; no "we".
- Unreleased things are said to be coming or in development, never as available.
- Never name or imply endorsement by Apple. "Apple Music" is the service the user already
  pays for.
- Headings: sentence case, no final period. Body: full sentences with periods. Button and
  link labels: two or three words, a verb first where natural, no period.
- Marks: a key may use only the marks its marks list names: code as `` `text` `` for
  commands, paths, addresses and config keys; strong as `**text**` (the one highlighted
  phrase in hero.lead, UI labels in the Docker steps); link as `[text](href)`, where href is
  one of the key's links.
- Simplified Chinese (zh-CN) is written for Chinese readers, not translated word for word:
  natural, plain Mandarin as in good Chinese technical docs. Full-width punctuation
  (，。：；（）), a half-width space between Chinese and Latin letters or digits (下载 ALAC 24-bit),
  product and format names left in Latin (AMDL, Apple Music, ALAC, AAC, Docker, MCP; Dolby
  Atmos may be 杜比全景声), commands and UI labels of the apps unchanged (the apps are in
  English).
- Keys with `"lang": "other"` are written in the other language in each file: ui.otherLang
  is 中文 in en.json and English in zh-CN.json; ui.otherLangLabel is Chinese in en.json and
  English in zh-CN.json.
- Accessibility labels (keys ending in label, Label or Alt, or under ui.) describe what a
  screen reader user would otherwise miss, in one plain sentence, no "image of".

### 8.2 Schema (hand this over verbatim)

```json
{
  "tone": [
    "You are writing the words for the website of AMDL, an open-source, self-hosted tool that downloads music from Apple Music to the user's own disk as finished, tagged files.",
    "Restrained and plain. Say what the thing does, in the fewest words that are true. No hype, no superlatives (\"best\", \"ultimate\", \"blazing\", \"seamless\", \"powerful\", \"effortless\"), no exclamation marks (neither ! nor ！), no rhetorical questions in body text, no emoji.",
    "Short. Every key has two hard limits in characters: maxChars for en.json and maxCharsZh for zh-CN.json. Only visible text counts: [text](href) counts as its text, and the ` and ** marks themselves do not count.",
    "Concrete. Prefer a fact (a format, a number, a command) to an adjective. Use only the facts listed for each key (the facts object holds their text); do not add claims. Numbers and names exactly as given.",
    "Strings a key lists in verbatim must appear exactly so, in both languages.",
    "Calm second person (\"you\") where a subject is needed; no \"we\".",
    "Unreleased things are said to be coming or in development, never as available.",
    "Never name or imply endorsement by Apple. \"Apple Music\" is the service the user already pays for.",
    "Headings: sentence case, no final period. Body: full sentences with periods. Button and link labels: two or three words, a verb first where natural, no period.",
    "Marks: a key may use only the marks its marks list names: code as `text` for commands, paths, addresses and config keys; strong as **text** (the one highlighted phrase in hero.lead, UI labels in the Docker steps); link as [text](href), where href is one of the key's links.",
    "Simplified Chinese (zh-CN) is written for Chinese readers, not translated word for word: natural, plain Mandarin as in good Chinese technical docs. Full-width punctuation (，。：；（）), a half-width space between Chinese and Latin letters or digits (下载 ALAC 24-bit), product and format names left in Latin (AMDL, Apple Music, ALAC, AAC, Docker, MCP; Dolby Atmos may be 杜比全景声), commands and UI labels of the apps unchanged (the apps are in English).",
    "Keys with \"lang\": \"other\" are written in the other language in each file: ui.otherLang is 中文 in en.json and English in zh-CN.json; ui.otherLangLabel is Chinese in en.json and English in zh-CN.json.",
    "Accessibility labels (keys ending in label, Label or Alt, or under ui.) describe what a screen reader user would otherwise miss, in one plain sentence, no \"image of\"."
  ],
  "facts": {
    "F01": "AMDL (`amdl`) downloads music from Apple Music to your own disk: it resolves a link, downloads the encrypted media, decrypts it, remuxes and tags it, and saves finished `.m4a` files",
    "F02": "It takes song, album, playlist, artist and radio station links, or a search of the catalog. Up to 100 links at once",
    "F03": "Self-hosted, for one person: each install serves one user; there are no user accounts and no auth (access control belongs to whatever runs in front)",
    "F04": "It needs your own Apple Music subscription",
    "F05": "It needs a wrapper: the service that holds your Apple Music login. amdl holds no Apple account and no keys",
    "F06": "Decryption runs inside amdl, in-process (a pure-Go translation of Temari, MIT)",
    "F07": "No decrypted copy of a whole track is written to disk: decrypted samples are piped straight into ffmpeg",
    "F08": "Catalog reads use Apple's undocumented web-player API, which can change without notice; no Apple developer key is needed",
    "F09": "Audio only: music videos are not supported; live radio such as Apple Music 1 is not supported; a station is a rolling list of what Apple offers now",
    "F10": "License: AGPL-3.0",
    "F11": "Repository: `https://github.com/AMDL-Web/apple-music-downloader`",
    "F12": "2.0 is in development (release candidates; rc.13 is the last) [unreleased]",
    "F13": "Codecs, in a priority list you order: `alac` (Apple Lossless), `ec3` (Dolby Digital Plus / Dolby Atmos), `ac3` (Dolby Digital), `aac`, `aac-binaural`, `aac-downmix`. AAC-LC is always appended as the last fallback and is not listed",
    "F14": "Default priority `[\"alac\"]`. With `codec_alternative` on (default), a codec that runs out of attempts hands over to the next; each codec has its own attempts",
    "F15": "ALAC up to 24-bit / 192 kHz by default; a lower ceiling can be set. Apple serves ALAC at 44.1 kHz and above, 16- and 24-bit",
    "F16": "The apps label qualities `ALAC 24/96`, `Atmos`, `Dolby Digital`, `AAC 256`, `AAC Binaural`, `AAC Downmix`; the long form is `ALAC 24-bit · 96 kHz`. AAC is 256 kbps",
    "F17": "Cover art is embedded, 5000×5000 JPEG by default; album and playlist covers are also saved as `cover.jpg` next to the music, and the artist's picture as `artist.jpg` in the artist folder (when Apple has one)",
    "F18": "Lyrics are embedded as LRC, timed by line or by word; translation and pronunciation (romanization) can be added; `.lrc` or `.ttml` files can be saved beside the track",
    "F19": "Each track is checked for integrity after download (on by default)",
    "F20": "Songs, albums and artists share one folder tree: `library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}` by default; playlists go to `playlists/{PlaylistName}/…` and stations to `stations/{StationName}/…`. The templates are configurable",
    "F21": "A file already at its path is adopted, not downloaded again (unless `download.overwrite_existing` is on); downloading again replaces a file only once the new one is complete",
    "F22": "One binary, `amdl`: the command line, the MCP server, the terminal interface and the backend with its web console",
    "F23": "Desktop app: native, for macOS (Apple silicon and Intel), Windows and Linux (amd64; needs GTK 3); attached to each release as `.dmg`, setup `.exe`, `.deb` and `.tar.gz`. Packages are not signed yet: macOS and Windows each stop the first launch once",
    "F24": "The desktop app is a client of an AMDL server, or standalone with the backend inside the app; standalone it shares one backend and history with `amdl service` on the same machine. On first launch it asks where downloads run, and its setup screen installs ffmpeg, QEMU where needed and the wrapper",
    "F25": "Web console: the browser client the server serves at `/console/` on the API's port, with nothing to install on the device that opens it; it uses only the public API. Light and dark. The Docker image publishes it on port 18080 for the network. A binary install stays on this machine (loopback): `amdl serve` on port 18080, `amdl service install` on a port it picks at install (`amdl service status` prints the address). Never write the loopback IP address on the page (the build rejects it); say \"this machine\"",
    "F26": "The console's pages: New download, Overview, Activity, Library (your Apple Music library, when switched on), Settings, Logs; a command palette and keyboard shortcuts; a phone layout with bottom tabs",
    "F27": "Terminal UI: `amdl` with no arguments (or `amdl tui`); prints into the terminal's scrollback; paste a link or type to search; up to three jobs as cards with an album bar and each track's stage; `/` commands such as `/quality`, `/queue`, `/library`, `/jobs`, `/retry`, `/logs`, `/login`, `/help`; follows a light or dark terminal; with `NO_COLOR` the bars still read by shape",
    "F28": "Command line: every result is JSON on stdout, errors are JSON on stderr with an exit status per kind of failure; `amdl cli search \"<task>\"` finds the command for a task; `--dry-run` on changes",
    "F29": "MCP: `amdl mcp` serves 37 tools over stdio; `amdl cli setup` prints the config for Claude Code, Codex or any `mcpServers` client and an agent skill; no tool signs the wrapper in, so an Apple ID password never passes through a model",
    "F30": "An agent goes from a search to a download in two calls (`search_catalog`, then `start_download`) and follows the job with `wait_for_job`",
    "F31": "HTTP API: REST and Server-Sent Events under `/api/v1` on the console's port; the OpenAPI spec is `internal/api/openapi.yaml`; no CORS headers (the console is its only browser client)",
    "F32": "Hooks: a webhook or a command when a job is queued, finishes, fails or is cancelled. Presets: ntfy, Bark and Telegram notifications; Plex, Jellyfin and Emby library refreshes; Custom. Commands are edited only in the file. Off by default",
    "F33": "Docker: the image `ghcr.io/amdl-web/apple-music-downloader` for linux/amd64 and linux/arm64, with ffmpeg inside, on port 18080; the Compose file is `deploy/docker-compose.yml`, which carries the wrapper-manager service commented out; `PUID`/`PGID` default 1000:1000",
    "F34": "Binary install: `install.sh` (macOS, Linux) checks the release against `SHA256SUMS` and installs `amdl` into `~/.local/bin`; `install.ps1` (Windows) into `%LOCALAPPDATA%\\Programs\\amdl`. Releases build macOS and Linux for amd64 and arm64 and Windows for amd64; Windows on ARM runs the amd64 build under emulation",
    "F35": "Homebrew and Scoop packages arrive with 2.0.1 [unreleased]",
    "F36": "ffmpeg 8 or later; `amdl doctor` checks the server, ffmpeg, the wrapper and the service and prints the command that installs what is missing",
    "F37": "`amdl wrapper install` gets a wrapper (wrapper-manager, checked against a pinned hash) and runs it natively on Linux x86_64 with user namespaces, in a QEMU guest elsewhere; not on Windows on ARM",
    "F38": "`amdl service install` runs the backend as a service that starts at login (LaunchAgent, systemd user unit or Task Scheduler task); `amdl service status` prints the console's address. With a wrapper on another machine, pass its address: `amdl service install --wrapper <url>`. The wrapper is signed in on the console's Account page, with the terminal UI's `/login`, or with `amdl wrapper login`",
    "F39": "The wrapper's native and Docker launchers (the Compose file's wrapper-manager service included) run on Linux x86_64 (amd64) and need user namespaces (`unshare -Ur true` must succeed); on arm64, or on a host without user namespaces such as Synology DSM, use wrapper-manager v2's QEMU variant, which needs `/dev/kvm`",
    "F40": "wrapper-manager v2 holds up to 10 Apple Music accounts and has the login the console's Account page uses (two-step codes are asked for there)",
    "F41": "Docker sign-in: open `http://localhost:18080/`, Settings › Account, set Session service to `http://wrapper-manager:8080`, save and restart from the notice, sign in with the Apple ID, paste a link into New download; files land in `./data/downloads`",
    "F42": "Binary installs save music to `~/Music/AMDL` (`%UserProfile%\\Music\\AMDL` on Windows)",
    "F43": "Until 2.0.0 is released, the install script and the `latest` image mean an older release: pass a 2.0 release candidate's tag with `--version` (`-Version` on Windows), for example `v2.0.0-rc.13`, and in the Compose file replace `latest` with the same version without the `v` (`2.0.0-rc.13`; images are tagged without it). A database written by rc.1 to rc.7 cannot be opened [unreleased: removed on release day]",
    "F44": "Coming from 1.x: the database migrates on first start and 1.x cannot read it afterwards; back it up and read the upgrade guide first",
    "F45": "The apps are in English; this site and the README are in English and Simplified Chinese"
  },
  "keys": {
    "meta.title": {"purpose": "Browser tab and search result title. Product name, then what it does.", "facts": ["F01", "F03"], "maxChars": 60, "maxCharsZh": 40, "marks": [], "verbatim": ["AMDL"]},
    "meta.description": {"purpose": "Search snippet and social card text: what it is, what it saves, the ways to use it.", "facts": ["F01", "F02", "F03", "F13", "F15", "F22"], "maxChars": 155, "maxCharsZh": 93, "marks": []},
    "meta.ogAlt": {"purpose": "Alt text of the social card image (the hero headline beside three album covers).", "facts": ["F01"], "maxChars": 90, "maxCharsZh": 54, "marks": []},

    "ui.skip": {"purpose": "Skip link to the main content.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "ui.nav": {"purpose": "Accessible name of the header navigation.", "facts": [], "maxChars": 20, "maxCharsZh": 12, "marks": []},
    "ui.sections": {"purpose": "Label of the header menu that lists the page's sections.", "facts": [], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "ui.anchor": {"purpose": "Accessible name of the small link glyph after each section title (a link to this section).", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "ui.theme": {"purpose": "Accessible name of the light/dark/system switch.", "facts": [], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "ui.themes.light": {"purpose": "Theme option: light.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "ui.themes.dark": {"purpose": "Theme option: dark.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "ui.themes.system": {"purpose": "Theme option: follow the system.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "ui.github": {"purpose": "Accessible name of the GitHub icon link.", "facts": ["F11"], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "ui.otherLang": {"purpose": "The other language's own name, shown as the language link. In en.json write 中文; in zh-CN.json write English.", "facts": [], "maxChars": 8, "lang": "other", "maxCharsZh": 8, "marks": []},
    "ui.otherLangLabel": {"purpose": "Tooltip of the language link, written in the OTHER language (en.json: Chinese; zh-CN.json: English).", "facts": [], "maxChars": 28, "lang": "other", "maxCharsZh": 28, "marks": []},
    "ui.copy": {"purpose": "Accessible name of a code block's copy button.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "ui.copied": {"purpose": "Same button after copying.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "ui.home": {"purpose": "Accessible name of the logo link to the page top.", "facts": [], "maxChars": 20, "maxCharsZh": 12, "marks": []},
    "ui.prev": {"purpose": "Accessible name of the stepper's previous arrow.", "facts": [], "maxChars": 20, "maxCharsZh": 12, "marks": []},
    "ui.next": {"purpose": "Accessible name of the stepper's next arrow.", "facts": [], "maxChars": 20, "maxCharsZh": 12, "marks": []},

    "nav.how": {"purpose": "Menu entry for section 1 (from a link to a file).", "facts": ["F01"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "nav.formats": {"purpose": "Menu entry for section 2 (formats).", "facts": ["F13"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "nav.apps": {"purpose": "Menu entry for section 3 (desktop app, web console, terminal, agents).", "facts": ["F22"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "nav.install": {"purpose": "Menu entry for section 4 (install).", "facts": [], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "nav.docs": {"purpose": "Header link to the documentation.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},

    "header.install": {"purpose": "The header's primary button, jumps to the install section.", "facts": [], "maxChars": 14, "maxCharsZh": 9, "marks": []},

    "hero.prereleaseBadge": {"purpose": "Temporary first part of the eyebrow pill: AMDL 2.0 is in development. Deleted with its markup on release day.", "facts": ["F12"], "maxChars": 24, "maxCharsZh": 18, "marks": [], "verbatim": ["2.0"]},
    "hero.eyebrow": {"purpose": "Permanent part of the small pill above the headline: two plain facts that stay true after release (for example self-hosted and open source). No version state; that is hero.prereleaseBadge.", "facts": ["F03", "F10"], "maxChars": 40, "maxCharsZh": 24, "marks": []},
    "hero.title": {"purpose": "The headline, two lines separated by \\n. Line 1 names the service, line 2 (shown in the accent color) says where the music ends up. Each line at most 28 characters.", "facts": ["F01"], "maxChars": 57, "maxCharsZh": 35, "marks": []},
    "hero.lead": {"purpose": "One or two sentences under the shelf of album covers: what goes in, what comes out, how it is used. Contains exactly one **highlighted** phrase (the outcome).", "facts": ["F01", "F02", "F07", "F22"], "maxChars": 230, "maxCharsZh": 138, "marks": ["strong"]},
    "hero.source": {"purpose": "Small text link at the end of the Runs-on row, after the platform names: opens the GitHub repository (for example 'View source'). The hero has no buttons.", "facts": ["F11"], "maxChars": 18, "maxCharsZh": 11, "marks": []},
    "hero.runsOn": {"purpose": "Label before the platform names macOS, Windows, Linux, Docker.", "facts": ["F23", "F33", "F34"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "hero.shelfLabel": {"purpose": "Accessible description of the hero picture: made-up album covers on a shelf and the finished file 01. Glass Weather.m4a in ALAC 24-bit · 96 kHz. Name no number of covers: the wide shelf shows five, a phone three.", "facts": ["F16", "F20"], "maxChars": 200, "maxCharsZh": 161, "marks": [], "verbatim": ["01. Glass Weather.m4a", "ALAC 24-bit · 96 kHz"]},

    "how.title": {"purpose": "Section 1 heading: a link becomes a file.", "facts": ["F01"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "how.lead": {"purpose": "One sentence under the heading: what AMDL does with a link, and that the decryption happens on your machine.", "facts": ["F01", "F05", "F06", "F07"], "maxChars": 170, "maxCharsZh": 102, "marks": []},
    "how.link.title": {"purpose": "Card A title: the input (a link).", "facts": ["F02"], "maxChars": 22, "maxCharsZh": 14, "marks": []},
    "how.link.text": {"purpose": "Card A sentence: which links are accepted, or search instead. A demo album link is shown under it.", "facts": ["F02"], "maxChars": 90, "maxCharsZh": 54, "marks": []},
    "how.link.note": {"purpose": "Card A bottom line, bold: a short plain statement about the input (for example that many links can go at once).", "facts": ["F02"], "maxChars": 48, "maxCharsZh": 29, "marks": []},
    "how.amdl.title": {"purpose": "Card B title: what AMDL does (may simply be AMDL or a short verb phrase).", "facts": ["F01"], "maxChars": 22, "maxCharsZh": 14, "marks": []},
    "how.amdl.steps.0.name": {"purpose": "Step 1 name: download.", "facts": ["F01"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "how.amdl.steps.0.text": {"purpose": "Step 1 clause: fetches the encrypted media from Apple.", "facts": ["F01"], "maxChars": 56, "maxCharsZh": 34, "marks": ["code"]},
    "how.amdl.steps.1.name": {"purpose": "Step 2 name: decrypt.", "facts": ["F06"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "how.amdl.steps.1.text": {"purpose": "Step 2 clause: inside amdl, with keys from your own wrapper.", "facts": ["F05", "F06"], "maxChars": 56, "maxCharsZh": 34, "marks": ["code"]},
    "how.amdl.steps.2.name": {"purpose": "Step 3 name: remux.", "facts": ["F07"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "how.amdl.steps.2.text": {"purpose": "Step 3 clause: ffmpeg writes the .m4a; no decrypted whole track on disk.", "facts": ["F07", "F36"], "maxChars": 56, "maxCharsZh": 34, "marks": ["code"]},
    "how.amdl.steps.3.name": {"purpose": "Step 4 name: tag.", "facts": ["F17", "F18"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "how.amdl.steps.3.text": {"purpose": "Step 4 clause: tags, cover and lyrics go into the file, then an integrity check.", "facts": ["F17", "F18", "F19"], "maxChars": 56, "maxCharsZh": 34, "marks": ["code"]},
    "how.amdl.note": {"purpose": "Card B bottom line, bold: one plain statement (for example that a failed codec falls back to the next).", "facts": ["F14"], "maxChars": 48, "maxCharsZh": 29, "marks": []},
    "how.file.title": {"purpose": "Card C title: the output on your disk.", "facts": ["F20"], "maxChars": 22, "maxCharsZh": 14, "marks": []},
    "how.file.text": {"purpose": "Card C sentence: songs, albums and artists share one folder tree; a folder tree is shown under it.", "facts": ["F20"], "maxChars": 90, "maxCharsZh": 54, "marks": ["code"]},
    "how.file.note": {"purpose": "Card C bottom line, bold: files already there are kept, not downloaded again.", "facts": ["F21"], "maxChars": 48, "maxCharsZh": 29, "marks": []},
    "how.label": {"purpose": "Accessible description of the folder tree in card C: under library/, the artist folder Mira Solenne holds artist.jpg and the album folder Glass Weather, which holds the numbered .m4a tracks and cover.jpg.", "facts": ["F17", "F20"], "maxChars": 200, "maxCharsZh": 145, "marks": [], "verbatim": ["Mira Solenne", "Glass Weather"]},
    "how.more": {"purpose": "Soft button to the download pipeline documentation.", "facts": [], "maxChars": 30, "maxCharsZh": 18, "marks": []},

    "formats.title": {"purpose": "Section 2 heading about formats.", "facts": ["F13", "F15"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "formats.lead": {"purpose": "One sentence: lossless up to 24-bit/192 kHz, Dolby Atmos or AAC, in the order you choose.", "facts": ["F13", "F14", "F15"], "maxChars": 170, "maxCharsZh": 102, "marks": []},
    "formats.file.codec": {"purpose": "Row label in the file facts: codec / quality.", "facts": ["F16"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "formats.file.cover": {"purpose": "Row label: cover art (value shown is 5000 × 5000).", "facts": ["F17"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "formats.file.lyrics": {"purpose": "Row label: lyrics.", "facts": ["F18"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "formats.file.lyricsValue": {"purpose": "Row value: LRC, timed by line or by word. May use `LRC`.", "facts": ["F18"], "maxChars": 40, "maxCharsZh": 24, "marks": ["code"]},
    "formats.file.tags": {"purpose": "Row label: tags / metadata.", "facts": ["F17"], "maxChars": 14, "maxCharsZh": 9, "marks": []},
    "formats.file.tagsValue": {"purpose": "Row value: what the tags hold (title, artist, album, track number and so on), very short.", "facts": ["F01"], "maxChars": 40, "maxCharsZh": 24, "marks": []},
    "formats.ladder.head": {"purpose": "Small heading over the codec ladder: tried in this order.", "facts": ["F13", "F14"], "maxChars": 40, "maxCharsZh": 24, "marks": []},
    "formats.rows.alac": {"purpose": "Note under ALAC: lossless, up to 24-bit / 192 kHz.", "facts": ["F13", "F15"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "formats.rows.ec3": {"purpose": "Note under Dolby Atmos: spatial audio, where the album has it.", "facts": ["F13"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "formats.rows.aac": {"purpose": "Note under AAC: 256 kbps.", "facts": ["F16"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "formats.rows.aaclc": {"purpose": "Note under AAC-LC: always tried last, not listed.", "facts": ["F13", "F14"], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "formats.also": {"purpose": "Label before three extra codec names (ac3, aac-binaural, aac-downmix): also available.", "facts": ["F13"], "maxChars": 40, "maxCharsZh": 24, "marks": []},
    "formats.caption": {"purpose": "Caption under the card: what quality_priority is and that a codec that fails hands over to the next.", "facts": ["F13", "F14"], "maxChars": 150, "maxCharsZh": 106, "marks": ["code"], "verbatim": ["quality_priority"]},

    "apps.title": {"purpose": "Section 3 heading: the ways to use AMDL.", "facts": ["F22"], "maxChars": 44, "maxCharsZh": 27, "marks": []},

    "apps.lead": {"purpose": "One or two sentences: one backend, several front ends, use whichever fits.", "facts": ["F22", "F24", "F25"], "maxChars": 180, "maxCharsZh": 108, "marks": []},

    "apps.shown": {"purpose": "Small note under the stepper: these are illustrations of the real screens with made-up music, in English like the apps.", "facts": ["F45"], "maxChars": 120, "maxCharsZh": 72, "marks": []},

    "apps.more": {"purpose": "Short label before the two text links (HTTP API, hooks) under the apps section, for example 'Also:'.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},

    "apps.desktop.tab": {"purpose": "Stepper label 1: desktop app.", "facts": ["F23"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "apps.desktop.caption": {"purpose": "The panel's one or two sentences under the mock: a native app for macOS, Windows and Linux; a client of your server, or standalone with the backend inside, setting up ffmpeg and the wrapper for you.", "facts": ["F23", "F24"], "maxChars": 160, "maxCharsZh": 96, "marks": []},
    "apps.desktop.doc": {"purpose": "Button to the desktop install guide.", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "apps.desktop.label": {"purpose": "Accessible description of the mock: the desktop app's album page, Glass Weather mid-download, five of twelve tracks saved, the folder it saves to.", "facts": ["F20"], "maxChars": 200, "maxCharsZh": 120, "marks": []},

    "apps.console.tab": {"purpose": "Stepper label 2: web console.", "facts": ["F25"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "apps.console.caption": {"purpose": "The panel's one or two sentences: the server's own page, opened in a browser, with nothing to install on the device: from any device on the network when the server runs on a NAS or in Docker (port 18080), on this machine for a local install.", "facts": ["F25", "F26", "F33"], "maxChars": 160, "maxCharsZh": 96, "marks": ["code"]},
    "apps.console.doc": {"purpose": "Button to the web console section of the README.", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "apps.console.label": {"purpose": "Accessible description: the console's Overview, one failed link under Needs attention, three albums downloading as cards in their cover colors, one queued link.", "facts": [], "maxChars": 200, "maxCharsZh": 120, "marks": []},

    "apps.terminal.tab": {"purpose": "Stepper label 3: terminal.", "facts": ["F27", "F28"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "apps.terminal.caption": {"purpose": "The panel's one or two sentences: `amdl` with no arguments opens an interactive terminal UI (paste a link or search; jobs show each track's stage); every command also prints JSON for scripts.", "facts": ["F22", "F27", "F28"], "maxChars": 160, "maxCharsZh": 100, "marks": ["code"], "verbatim": ["amdl"]},
    "apps.terminal.doc": {"purpose": "Button to the terminal interface docs.", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "apps.terminal.label": {"purpose": "Accessible description: the terminal UI with the banner, Glass Weather downloading track by track, the prompt, and search results for Mira Solenne.", "facts": ["F27"], "maxChars": 200, "maxCharsZh": 120, "marks": []},

    "apps.agents.tab": {"purpose": "Stepper label 4: agents (MCP).", "facts": ["F29"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "apps.agents.caption": {"purpose": "The panel's one or two sentences: `amdl mcp` gives AI agents 37 tools, from a search to a finished download; no tool signs in, so an Apple ID password never passes through a model.", "facts": ["F29", "F30"], "maxChars": 160, "maxCharsZh": 104, "marks": ["code"], "verbatim": ["amdl mcp"]},
    "apps.agents.doc": {"purpose": "Button to the MCP docs.", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "apps.agents.label": {"purpose": "Accessible description of the chat: the user asks for the new Nightjar Coast album in Dolby Atmos; the agent calls search_catalog, start_download and wait_for_job, then says where the 11 tracks were saved.", "facts": ["F30"], "maxChars": 220, "maxCharsZh": 186, "marks": [], "verbatim": ["Nightjar Coast", "search_catalog", "start_download", "wait_for_job"]},
    "apps.agents.chat.you": {"purpose": "Speaker label of the user's message.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "apps.agents.chat.agent": {"purpose": "Speaker label of the agent's reply.", "facts": [], "maxChars": 10, "maxCharsZh": 6, "marks": []},
    "apps.agents.chat.ask": {"purpose": "The user's request in the chat, natural and short: download the new Nightjar Coast album in Dolby Atmos. Keep the artist name Nightjar Coast verbatim.", "facts": [], "maxChars": 72, "maxCharsZh": 58, "marks": [], "verbatim": ["Nightjar Coast"]},
    "apps.agents.chat.reply": {"purpose": "The agent's reply: done, Low Tide Signals (2026), all 11 tracks in Dolby Atmos, saved to the folder. Must contain `/volume1/music/library/Nightjar Coast/Low Tide Signals/` verbatim in a code mark and the album name verbatim.", "facts": ["F20"], "maxChars": 170, "maxCharsZh": 173, "marks": ["code"], "verbatim": ["/volume1/music/library/Nightjar Coast/Low Tide Signals/", "Low Tide Signals"]},
    "apps.agents.tools": {"purpose": "Summary line of the expandable list (closed by default) of all 37 MCP tools.", "facts": ["F29"], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.groups.catalog": {"purpose": "Tool group name: catalog (search, albums, playlists, links, quality).", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.groups.jobs": {"purpose": "Tool group name: jobs (downloads in the queue).", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.groups.library": {"purpose": "Tool group name: records of what was downloaded.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.groups.apple": {"purpose": "Tool group name: your Apple Music library.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.groups.system": {"purpose": "Tool group name: server and accounts.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "apps.agents.config": {"purpose": "Label above the mcpServers JSON: the client config amdl cli setup prints.", "facts": ["F29"], "maxChars": 48, "maxCharsZh": 43, "marks": ["code"], "verbatim": ["amdl cli setup"]},

    "apps.api.link": {"purpose": "Text link to the API reference, naming the HTTP API (REST and Server-Sent Events).", "facts": ["F31"], "maxChars": 28, "maxCharsZh": 17, "marks": []},

    "apps.hooks.link": {"purpose": "Text link to the hooks documentation, naming hooks (a webhook or a command when a job finishes or fails).", "facts": ["F32"], "maxChars": 28, "maxCharsZh": 17, "marks": []},

    "install.title": {"purpose": "Section 4 heading: install.", "facts": [], "maxChars": 44, "maxCharsZh": 27, "marks": []},
    "install.lead": {"purpose": "One or two sentences: what you need (an Apple Music subscription and a wrapper, the service that holds your Apple Music login) and the two ways to install.", "facts": ["F04", "F05", "F33", "F34"], "maxChars": 200, "maxCharsZh": 120, "marks": []},
    "install.tabsLabel": {"purpose": "Accessible name of the Binary/Docker switch: how to install.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "install.tabs.binary": {"purpose": "Tab: install the binary.", "facts": ["F34"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "install.tabs.docker": {"purpose": "Tab: Docker.", "facts": ["F33"], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "install.binary.steps.0.title": {"purpose": "Step 1 title: install amdl.", "facts": ["F34"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.binary.steps.0.text": {"purpose": "Step 1 text: the script checks SHA256SUMS and installs into ~/.local/bin (a user programs folder on Windows).", "facts": ["F34"], "maxChars": 170, "maxCharsZh": 124, "marks": ["code"], "verbatim": ["SHA256SUMS", "~/.local/bin"]},
    "install.binary.steps.1.title": {"purpose": "Step 2 title: check ffmpeg.", "facts": ["F36"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.binary.steps.1.text": {"purpose": "Step 2 text: every track goes through ffmpeg 8 or later; amdl doctor says whether it is there and prints the install command.", "facts": ["F36"], "maxChars": 170, "maxCharsZh": 113, "marks": ["code"], "verbatim": ["amdl doctor"]},
    "install.binary.steps.2.title": {"purpose": "Step 3 title: get a wrapper.", "facts": ["F37"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.binary.steps.2.text": {"purpose": "Step 3 text: native on Linux x86_64, in a QEMU guest elsewhere; skip it if a wrapper already runs on a NAS or server.", "facts": ["F37", "F39"], "maxChars": 170, "maxCharsZh": 102, "marks": ["code"]},
    "install.binary.steps.3.title": {"purpose": "Step 4 title: run the backend.", "facts": ["F38"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.binary.steps.3.text": {"purpose": "Step 4 text: runs the backend as a service that starts at login; `amdl service status` prints the console's address; sign the wrapper in there or with `amdl wrapper login`. For a wrapper on another machine, add `--wrapper <its address>` to `amdl service install`.", "facts": ["F38"], "maxChars": 200, "maxCharsZh": 148, "marks": ["code"], "verbatim": ["amdl service status", "--wrapper"]},
    "install.docker.steps.0.title": {"purpose": "Step 1 title: get the Compose file.", "facts": ["F33"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.docker.steps.0.text": {"purpose": "Step 1 text: in an empty directory; create data/downloads first and give it to the backend's user (PUID:PGID, 1000:1000 by default).", "facts": ["F33"], "maxChars": 190, "maxCharsZh": 137, "marks": ["code"], "verbatim": ["data/downloads", "1000:1000"]},
    "install.docker.steps.1.title": {"purpose": "Step 2 title: turn on the wrapper.", "facts": ["F33", "F40"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.docker.steps.1.text": {"purpose": "Step 2 text: in docker-compose.yml, uncomment the wrapper-manager service and the volumes: block at the end.", "facts": ["F33"], "maxChars": 190, "maxCharsZh": 155, "marks": ["code"], "verbatim": ["docker-compose.yml", "wrapper-manager", "volumes:"]},
    "install.docker.steps.2.title": {"purpose": "Step 3 title: start both.", "facts": [], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.docker.steps.2.text": {"purpose": "Step 3 text: Compose builds wrapper-manager from AMDL-Web's wrapper fork, so the host needs internet access.", "facts": ["F40"], "maxChars": 190, "maxCharsZh": 114, "marks": ["code"]},
    "install.docker.steps.3.title": {"purpose": "Step 4 title: point at the wrapper and sign in.", "facts": ["F41"], "maxChars": 28, "maxCharsZh": 17, "marks": []},
    "install.docker.steps.3.text": {"purpose": "Step 4 text, before the address block: open http://localhost:18080/, go to Settings › Account and set Session service to the address below, then save and restart from the notice. Addresses in `code`, UI labels in **bold**.", "facts": ["F41"], "maxChars": 150, "maxCharsZh": 146, "marks": ["code", "strong"], "verbatim": ["http://localhost:18080/", "Settings › Account", "Session service"]},
    "install.docker.steps.3.after": {"purpose": "Step 4 text, after the address block: sign in with your Apple ID and paste a link into New download; finished files land in ./data/downloads. Paths in `code`, UI labels in **bold**.", "facts": ["F41"], "maxChars": 140, "maxCharsZh": 112, "marks": ["code", "strong"], "verbatim": ["New download", "./data/downloads"]},
    "install.prereleaseLabel": {"purpose": "Label pill of the 2.0 note, for example the version number with a word for 'note'.", "facts": ["F12"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "install.prerelease": {"purpose": "The 2.0 note (removed on release day): until 2.0.0 is released, pass a release candidate's tag to the install script with --version (-Version on Windows), for example v2.0.0-rc.13, and in the Compose file replace latest with the same version without the v (2.0.0-rc.13); Homebrew and Scoop arrive with 2.0.1; a database from rc.1 to rc.7 cannot be opened.", "facts": ["F43", "F35"], "maxChars": 320, "maxCharsZh": 238, "marks": ["code"], "verbatim": ["--version", "-Version", "v2.0.0-rc.13", "2.0.0-rc.13", "latest"]},
    "install.nas.label": {"purpose": "Label pill of the NAS note.", "facts": [], "maxChars": 12, "maxCharsZh": 8, "marks": []},
    "install.nas.text": {"purpose": "NAS note: the wrapper needs Linux x86_64 with user namespaces (unshare -Ur true must succeed); on arm64, or on a host without user namespaces such as Synology DSM, use wrapper-manager v2's QEMU variant, which needs /dev/kvm.", "facts": ["F39"], "maxChars": 190, "maxCharsZh": 138, "marks": ["code"], "verbatim": ["unshare -Ur true", "/dev/kvm"]},
    "install.nas.link": {"purpose": "Link label to the wrapper setup docs.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "install.more": {"purpose": "Closing line: every option is in the install guide and the deployment guide; coming from 1.x, read the 2.0 upgrade guide first. Use exactly the three links in `links`, one each, in that order.", "facts": ["F44"], "maxChars": 160, "maxCharsZh": 96, "marks": ["link"], "links": ["{blob}/docs/install.md", "{blob}/docs/deployment.md", "{blob}/docs/upgrading-to-2.0.md"]},

    "cta.title": {"purpose": "Big headline of the closing band: an invitation to start, plain.", "facts": [], "maxChars": 48, "maxCharsZh": 29, "marks": []},
    "cta.button": {"purpose": "Its one button, to the install guide.", "facts": [], "maxChars": 22, "maxCharsZh": 14, "marks": []},

    "footer.tagline": {"purpose": "One line under the footer logo: what AMDL is.", "facts": ["F01", "F03", "F10"], "maxChars": 90, "maxCharsZh": 54, "marks": []},
    "footer.notice": {"purpose": "The notice: needs your own Apple Music subscription, for personal use (one install serves one person), relies on Apple's undocumented web-player API which can change without notice.", "facts": ["F03", "F04", "F08"], "maxChars": 230, "maxCharsZh": 138, "marks": []},
    "footer.docs.title": {"purpose": "Footer column heading: documentation.", "facts": [], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "footer.docs.install": {"purpose": "Link: install guide.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.docs.deployment": {"purpose": "Link: deployment guide (Docker, wrapper).", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.docs.cli": {"purpose": "Link: command line docs.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.docs.api": {"purpose": "Link: API reference.", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.docs.automation": {"purpose": "Link: automation (hooks).", "facts": [], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.docs.upgrade": {"purpose": "Link: upgrading to 2.0.", "facts": ["F44"], "maxChars": 24, "maxCharsZh": 15, "marks": []},
    "footer.project.title": {"purpose": "Footer column heading: the project.", "facts": [], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "footer.project.github": {"purpose": "Link: the GitHub repository.", "facts": ["F11"], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "footer.project.releases": {"purpose": "Link: releases (downloads of each version).", "facts": [], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "footer.project.license": {"purpose": "Link: the license, naming AGPL-3.0.", "facts": ["F10"], "maxChars": 28, "maxCharsZh": 25, "marks": [], "verbatim": ["AGPL-3.0"]},
    "footer.project.docs": {"purpose": "Link: the documentation index.", "facts": [], "maxChars": 16, "maxCharsZh": 10, "marks": []},
    "footer.legal": {"purpose": "Bottom line: open source under AGPL-3.0, and that AMDL is not affiliated with Apple.", "facts": ["F10"], "maxChars": 90, "maxCharsZh": 62, "marks": [], "verbatim": ["AGPL-3.0"]}
  }
}
```

`facts` repeats section 7's "Fact" column without the sources; when a fact changes,
change both. 145 keys in all. The block is stored as `content/schema.json`, and the
answer is checked against it before it is used (tests, 9.3).

---

## 9. Implementation constraints and test plan

### 9.1 Constraints to keep

- **Zero-dependency Node build.** `node apps/site/build.mjs` with Node.js 26 and nothing
  installed: no npm packages, no bundler, no generator. Modules in this directory:
  `build.mjs` (template, build, CSP), `blocks.mjs` (escaping, icons, code blocks, tabs),
  a covers module (6.2), a mocks module (M1–M4 and the MCP tool list), `og.mjs`,
  `serve.mjs`. `build.mjs` keeps exporting `REF`, `REPO`, `LANGS`, `PRIVATE_ADDRESS`,
  `UsageError`, `build`, `inline`, `loadContent`, `parseArgs` (the Pages workflow
  `.github/workflows/pages.yml` imports `REF`; `make site` runs `build.mjs --base … --out
  …` then `og.mjs --out …`).
- **Two languages, identical keys.** `content/en.json` and `content/zh-CN.json`, the schema
  of section 8 nested, same keys and array lengths, no empty strings. **Who writes them:**
  the copywriter's two files are placed at `content/en.json` and `content/zh-CN.json`
  before you start. If they are not there, generate placeholders from
  `content/schema.json` (each leaf = its key, cut to `maxChars`) so the build runs, and
  leave the copy tests failing until the real files land. Never write wording yourself;
  only the verbatim text this document gives (labels, commands, paths, data) is yours to
  place. Pages at `en/` and
  `zh-CN/`, plus the root page (`renderRoot`, unchanged: it sends a visitor to the language
  stored in `localStorage` key `amdl-site-lang`, else the first `zh*`/`en*` in
  `navigator.languages`, else English). Each language page writes its own code to
  `amdl-site-lang`.
- **Nothing from another host.** No external script, stylesheet, font, image or
  `preconnect`. The CSS and JS are inlined (`static/site.css` and the mocks' stylesheet,
  `static/site.js`); the only file besides the pages is `favicon.svg` (and `og.png`, which
  `og.mjs` adds when publishing).
- **Content-Security-Policy** (`build.mjs#csp`): `default-src 'none'`; `script-src` and
  `style-src` list the hashes of exactly the inline scripts and styles (the head script,
  the page script, the page style, the noscript style); `img-src 'self'`;
  `base-uri 'none'`; `form-action 'none'`. Hence: **no `style=""` attribute anywhere**
  (including inside SVG), **no inline event handlers**, no `javascript:` URLs. A width, a
  position or a color that varies per element is a class (generate the classes into the
  stylesheet: for example `.sh-1 … .sh-3` for the shelf, `.lc-tidal` for a live card's
  palette, `.p62` for a 62% fill) or an SVG presentation attribute.
- **SVG paint references** (`fill="url(#cv-…)"`, `filter="url(#…)"`) are local fragment
  references and allowed; they need the "loads nothing" test to permit `url(#` (9.3).
- **Theme**: `prefers-color-scheme` until the visitor picks Light or Dark in the switch,
  which sets `data-theme` on `<html>` and stores it in `localStorage` key
  `amdl-site-theme`; System removes both. The head script applies the stored theme before
  the first paint (today's `HEAD_SCRIPT`). Every `localStorage` access is in `try`/`catch`.
- **Open Graph card.** `og.mjs` keeps its role: it fills `static/og.html` from
  `content/en.json` and shoots it at 1200×630 with headless Chrome into `og.png`. New card:
  `--paper` background (light tokens); the mark and wordmark at (72, 64); the hero title's
  two lines at 76px, weight 600, tracking -0.045em, line 2 in `#004aad`, left column
  (x 72, max width 560, vertically centered); `hero.eyebrow` above it in the pill style
  (the permanent part only, no 2.0 badge); on the right, three sleeves fanned: Tidal Rooms
  (240px, rotated -8°, center (780, 300)), Low Tide Signals (240px, rotated 7°, center
  (1060, 312)), Glass Weather (320px, rotated 0°, center (930, 330)) in front, each with
  the shelf shadow. About 100px of each back sleeve shows beside the front one, the left
  text column (x 72–632) stays clear, and the rotated Low Tide Signals ends near x 1194,
  inside the 1200px card. A hairline at y 560 under everything. `og.html` placeholders:
  `{{kicker}}`, `{{title}}` (line 2 wrapped in a
  `<span>`), `{{covers}}` (the sprite and three `<svg>` uses). `og.html` is not a site page
  (no CSP applies to it), but keep it free of external resources too.
- **MCP tool list** on the page equals the tools `internal/capability/capability.go#All`
  declares (`Tool: "…"`), as today.
- **Paths shown** follow the default `library_path_format`
  (`configs/config.example.yaml#download.library_path_format`): the file chip, card C's
  tree, the desktop mock's SAVED TO, the agent's reply.
- **No private addresses** (`PRIVATE_ADDRESS`: `127.0.0.1`, `192.168.x.x`, `10.x.x.x`)
  anywhere in a page. Use `nas.local`, `nas`, `localhost`, `wrapper-manager`.
- **Relative links** inside the site (`./`, `../en/`, `../zh-CN/`, `#id`, `../favicon.svg`);
  absolute URLs only for the repository and with `--base` (canonical, `hreflang`,
  `og:url`, `og:image`).
- **Without JavaScript** every tab panel is shown, one after another, the tab lists,
  stage arrows, copy buttons and theme switches are hidden (`blocks.mjs#NOSCRIPT_TABS`;
  add `.stage-arrow` to its style), the Sections menu still works (`<details>`), the hero
  is static.
- **The mocks are pictures**: no `<button>`, `<a>`, `<input>` inside a mock's frame.
- The README of this directory is rewritten to match (layout table, product mocks table,
  tests list), and its **Release day** section updated: on release day the hero's
  `<span class="prerelease">` and its key `hero.prereleaseBadge` are deleted (the rest of
  the eyebrow stays), the 2.0 callout (`install.prereleaseLabel`, `install.prerelease`
  and the markup that shows them) is removed, `REF` becomes `main`. Every temporary key
  and class carries the word `prerelease`, so `rg -n "prerelease"` over this directory
  finds everything that must go, whatever the copy says.

### 9.2 Current tests: keep or rewrite

| Test (in `build.test.mjs`) | Verdict |
| --- | --- |
| arguments: defaults, `--base` slash, rejections (incl. retired `--desktop`, `--tui`), usage exit 2 | Keep as is (rules) |
| content: same keys and list lengths, no empty strings | Keep (rule) |
| a copy button takes whole commands | Keep (`blocks.mjs#commands`) |
| inline marks | Keep (`build.mjs#inline`); add a case for `markLead` (9.3) |
| the page lists exactly the MCP tools `capability.go` declares | Keep (rule); import `MCP_TOOLS` from the new mocks module |
| the path the page shows follows the default library template | Keep the rule; rewrite against the new helper (for example `libraryPath(artist, album, n, title)`) and assert the file chip text, the tree's first file and the desktop SAVED TO folder |
| Open Graph: card carries the English hero and the four stages, `class="wave"` | Rewrite: it carries both title lines, the eyebrow and three cover uses; no `{{` left |
| `pngSize`, Chrome draws it at its size | Keep |
| the site is its pages and the favicon, no app inside (`<iframe`, `<img `, `srcset=`, `demo/`) | Keep |
| every nav link leads to a section (old list desktop…install) | Keep the rule; new list `['how', 'formats', 'apps', 'install']` |
| desktop and console each show three screens as tabs | Old layout: replace with 9.3 "apps stepper" |
| each mock has its one interaction | Old layout: replace with "mocks contain no controls" |
| terminal, agents and hero show their pictures (TUI wide/narrow, CLI phases, chat, tools, `mcpServers`, four waveform groups) | Rewrite: keep TUI wide and narrow `<pre>`, every MCP tool as `<code>` (inside the closed `<details>`, still in the DOM), `mcpServers`; drop the CLI and waveform checks; add the shelf |
| each language page declares its language and links the other | Keep |
| the root page offers every language | Keep |
| every local link and anchor resolves (`href`, `src`, `aria-controls`, `aria-labelledby`) | Keep; it also covers `<use href="#cv-…">` |
| pages load nothing from another host | Keep; change `/@import\|url\(/` to `/@import\|url\((?!#)/` |
| the CSP covers exactly the inline scripts and styles; no `style=`, no `on…=` | Keep |
| no private addresses leak | Keep |

### 9.3 New tests

- **Apps stepper**: in `#apps`, a `role="tablist"` with exactly four `role="tab"` (ids
  `tab-apps-desktop`, `tab-apps-console`, `tab-apps-terminal`, `tab-apps-agents`), each
  controlling a panel whose `.stage` contains one `.mock` with `role="img"` whose
  `aria-label` is the content's `apps.<x>.label`, and two `.stage-arrow` buttons outside
  that `.mock`; `NOSCRIPT_STYLE` names `.stage-arrow`.
- **Mocks contain no controls**: inside each `.mock` frame, no `<button`, `<a `, `<input`.
- **Install tabs**: the Binary/Docker tab list's `aria-label` is `install.tabsLabel`.
- **Covers**: parse `internal/demo/catalog.go` (`{id: "(\d+)", title: "([^"]+)", artist:
  artists\[(\d)\], year: (\d+), bg: "([0-9a-f]{6})", fg: "([0-9a-f]{6})"`) and assert the
  covers module has a full, a mid and a small symbol for each of the eight albums, that
  each symbol uses its album's bg and fg (as `#rrggbb`), that the mid and small symbols
  carry no `<text>`, and that the titles and years the page shows for an album match the
  catalog. The mocks build those facts from the catalog module (M2's live-card facts line
  `<genre> · <year> · Started <hh:mm>` and cell count, the TUI's search rows
  `<artist> · <year> · <n> tracks`, its `3/<n>` and the song's length), and a test checks
  the built page shows them.
- **Demo data**: every track title a mock shows is in that album's track list in
  `catalog.go` (Glass Weather rows, the TUI's Slow Lightning, Halo Static, Small Hours,
  Paper Satellites), and the playlist `Slow Lightning Radio` exists there.
- **Shelf**: the wide shelf has five `<use href="#cv-…">` and the phone shelf three.
- **Names**: each section's `h2` holds only its title (the anchor glyph is a sibling), so
  the heading and the section it labels are named by the title alone.
- **Mock language**: no mock frame carries `lang`; each holds a `lang="en"` wrapper (or,
  in M4, `lang="en"` tool calls).
- **Theme switch**: exactly two `role="radiogroup"` (`.theme-head`, `.theme-menu`), each
  with three `role="radio"` whose `data-theme-set` are `light`, `dark`, `system`; the
  stylesheet hides `.theme-head` at ≤700px and `.theme-menu` above 700px.
- **Copy schema**: `content/schema.json` is the block of 8.2; its `keys` list equals the
  leaf keys of both content files, and every fact id a key names is in its `facts`.
- **Copy limits**: after the counting rule of section 8 (links to their text, backticks
  and `**` removed, code points counted), every leaf of `content/en.json` is at most its
  `maxChars` and every leaf of `content/zh-CN.json` at most its `maxCharsZh`.
- **Copy marks**: a leaf whose key does not list `code` has no backtick, without `strong`
  no `**`, without `link` no `[…](…)`; every link's href is in the key's `links`.
- **Copy rules**: no `!` and no `！` in any content string; every `verbatim` string of a
  key appears in both languages' leaf; `hero.lead` has exactly one `**…**`;
  `hero.title` has exactly one `\n`.
- **`markLead`**: `markLead('a **b** c')` is `a <mark class="hl">b</mark> c`, and the
  rendered hero has exactly one `<mark class="hl">` and no other `<mark`.
- **Hero file chip** text is `01. Glass Weather.m4a` and `ALAC 24-bit · 96 kHz`.
- **Card C tree** is the default template's folders for the demo album with `artist.jpg`
  in the artist folder and `cover.jpg` in the album folder.

Run: `node --test apps/site/` (part of `make node-test`), then `go test ./tools/repocheck/`
for this document and the README. Build and look: `node apps/site/build.mjs --serve 8080`,
both themes, both languages, at 1440, 1024, 768, 701, 390, 360 and 320px (no sideways
page scroll at any of them); and `make site` with Chrome for the OG card. Review step for
the covers (not a test): lay the eight small symbols in a row at 40px and the eight full
ones at 300px; at 40px no two may share a silhouette (6.1's "Main shape" column).

---

## 10. Responsive behavior

Breakpoints: phone ≤700px, tablet 701–1023px, desktop ≥1024px. No horizontal page scroll
at any width down to 320px: `html, body { overflow-x: clip }` is not the fix; every block
must fit (code and terminal bodies scroll inside themselves).

| Element | Desktop | Tablet | Phone |
| --- | --- | --- | --- |
| Gutter | 40px | 32px | 20px |
| Header | full | Docs and GitHub move into the Sections panel | wordmark, icon-only Sections, Install button; 64px tall. The language link and the theme switch also move into the panel (3.0) |
| Hero title | 68px | ~52px (clamp) | 40px, lines may wrap |
| Hero eyebrow | one line | one line | the two parts stacked, no divider |
| Hero shelf | wide SVG (5 sleeves), chip at 57%/80% | wide SVG scaled, chip text 13/11px | phone SVG (3 sleeves, top cropped), chip below, pulled up over the shelf |
| Hero Runs-on row | one line | one line | wraps, centered |
| How cards | row of three (346px each), knobs right | stacked, knobs at bottom; tree scrolls inside its plate if needed | stacked, knobs at bottom |
| Format card | two columns | one column, cover 240 centered, padding 32 | one column, cover 240, padding 24 |
| Apps stepper | 760px, labels under circles | same | full width, 12px labels, may wrap to two lines |
| Apps stage | aspect ratio 1120/700, padding 40/64, arrows on edges | height auto, padding 32/40, arrows on edges | height auto, no padding (the mock is the full width), arrows hidden; swipe not required |
| Mocks | design size scaled by `--u` | scaled (`--u` ≥ 0.4px); M4 one column | phone layouts, each width 100% / max 343px; M4 one column |
| Apps links line | one line | one line | label, then the two links, wrapping |
| Install tabs and stepper | 760px | 100% | tabs 44px tall; circles 28px, code blocks full width, long commands scroll inside the block |
| CTA | 112px padding | 96px | 72px, title 32px |
| Footer | 2fr 1fr 1fr | same | brand on top, two link columns below |

Touch targets on phones are at least 44×44px (buttons, tabs, theme radios, menu rows,
the code blocks' copy buttons, M4's tool-list summary). The header's Install button is
drawn 36px tall but keeps a 44px hit area (3.0).

---

## 11. Accessibility

- **Contrast**: all text pairs in 4.1 meet WCAG AA (4.5:1 body, 3:1 for 24px+ text and UI
  boundaries); the values were measured (for example `--muted` on `--paper` 6.27:1, white
  on `--action-bg` 8.13:1, dark-mode white on `#2f6fdf` 4.71:1, `--muted` on each tint
  above 5:1). Cover art text is decorative (`aria-hidden`).
- **Structure**: one `h1` (hero), one `h2` per section (the CTA title is an `h2`),
  the how cards' titles and the install step titles `h3`; card B's steps and the codec
  ladder are `<ol>`, the file facts a `<dl>`. Landmarks: `header`, `nav`, `main#main`, `footer`.
  Each section has `aria-labelledby` its `h2`, and the `h2` holds only the title (the
  anchor glyph is its sibling, 4.4), so the heading and the landmark are both named by the
  title alone.
- **Keyboard**: skip link first; every control reachable in order; tab lists and the
  theme radiogroup move with arrow keys (roving `tabindex`); Escape closes the Sections
  menu and returns focus to its summary; the stage arrows are buttons that select the
  previous or next tab and move focus to it.
- **Focus**: a 2px `--focus` outline with 2px offset on every focusable element
  (`:focus-visible`), never removed.
- **Pictures**: the pictures are `role="img"` with a label from the content files and
  their contents `aria-hidden`: the hero shelf (`hero.shelfLabel`), card C's folder tree
  (`how.label`) and the four mocks (`apps.<x>.label`). Everything else that carries copy
  (the how cards, the format card, the install steps) is real content, with only icons,
  knobs, circles and covers `aria-hidden`. The side column of M4 (code and tool list) is
  real, readable text. A mock's English words sit in a `lang="en"` wrapper inside it
  (section 5), so on the Chinese page they are announced in an English voice while the
  frame's Chinese `aria-label` keeps the page's language.
- **State is never color alone**: the stepper's current step is filled and bold; the
  ladder's first rung has a number and the floor rung a dashed border and its note; in
  mocks, states have icons and words as in the apps.
- **Motion**: honors `prefers-reduced-motion` (4.5).
- **Forced colors**: under `forced-colors: active`, cards keep a 1px `CanvasText` border,
  the stepper's current circle uses `Highlight`, the mark keeps its shape.
- **Language**: `<html lang="en">` / `lang="zh-CN"`; the language link has `hreflang` and
  `lang`; `ui.otherLangLabel` is written in the other language.
- **Zoom**: the layout holds at 200% zoom and at 320px width without loss of content.
- **No-JS**: as 9.1; nothing essential needs a script.
