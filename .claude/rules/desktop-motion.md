---
paths:
  - "apps/desktop/internal/ui/**"
  - "apps/desktop/internal/widgets/**"
---

# Desktop motion

The maintainer's rules for every transition in the desktop app:

- What exists in both scenes moves continuously from one to the other.
- What was clicked grows into the new page and shrinks back into it; nothing
  slides in from a direction unrelated to the click.
- A nearly critically damped spring, never draggy.
- Nothing pops, brightens or snaps after the motion ends: shadows, badges,
  text and the screen under the page settle while it moves.
- A cover or flight shows no square backing or shadow outside its rounded
  shape.
- No white flash or fog. Overlapping layers stay apart, the back one blurred
  or dimmed; the old and new content of a switch never show through each
  other.
- One screen's switch (tabs, filters, option capsules) looks and moves as
  every other's.
- A page's color arrives gradually: the background cross-fades as a whole page,
  directly from one page's colors to the other's, never as a strip and never
  through a darker ground in between.
- The flight's values are the maintainer's, tuned in the app's panel
  (Ctrl+Option+M, `apps/desktop/internal/ui#tuneDefaults`); change them only
  with the maintainer. A motion complaint is checked on frames of the
  maintainer's own recording before any fix.
- A transition the maintainer has seen without objecting keeps its timing, order
  and spring. An audit finding against it, even one citing these rules, is
  reported to the maintainer, not fixed; only their complaint or recording
  reopens it.
