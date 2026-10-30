// Reads the per-frame record the probe keeps during a transition and names
// what flickered. A frame: {t, text, skel, h, focus, scroll, theme, bg,
// pendingImgs, bareImgs, added, removed}. Pure, so each rule has a test.

/**
 * opts: {skeletonMinMs (a loader shown for less than this is a flash),
 * latencyMs (how fast the data answered), expectScroll, expectTheme}
 */
export function analyzeFrames(frames, opts = {}) {
  const out = [];
  if (!frames || frames.length < 2) return out;
  const { skeletonMinMs = 300, latencyMs = 0, expectScroll = null, expectThemeChange = false, ignoreFocus = false } = opts;
  const first = frames[0];
  const last = frames[frames.length - 1];
  const t0 = first.t;

  // Blank: the region's text drops below half of both ends, then returns.
  const floor = Math.min(first.text, last.text);
  if (floor >= 20) {
    let start = null;
    for (const f of frames) {
      if (f.text < floor * 0.5) {
        if (start === null) start = f;
      } else if (start) {
        out.push({ kind: 'blank', ms: Math.round(f.t - start.t), at: Math.round(start.t - t0), detail: `text ${start.text} chars, down from ${floor}` });
        start = null;
      }
    }
  }

  // Skeleton flash: a loader that comes and goes faster than the eye settles,
  // or any loader at all for data that answered within one frame budget.
  let skelStart = null;
  for (const f of frames) {
    if (f.skel > 0 && skelStart === null) skelStart = f;
    if (f.skel === 0 && skelStart) {
      const ms = f.t - skelStart.t;
      if (ms < skeletonMinMs) out.push({ kind: 'skeleton-flash', ms: Math.round(ms), at: Math.round(skelStart.t - t0), detail: `${skelStart.skel} placeholders for ${Math.round(ms)} ms (data answered in ${latencyMs} ms)` });
      skelStart = null;
    }
  }

  // Focus: held before and left on <body> at the end, or on <body> for more
  // than a moment on the way (a frame between unmounting the focused control
  // and focusing the next one is not a loss anyone notices).
  if (!ignoreFocus && first.focus && first.focus !== 'body') {
    const lost = frames.find((f) => !f.focus || f.focus === 'body');
    const back = lost && frames.find((f) => f.t > lost.t && f.focus && f.focus !== 'body');
    const gone = lost && (!back || back.t - lost.t > 150);
    if (gone) out.push({ kind: 'focus-lost', at: Math.round(lost.t - t0), detail: back ? `focus on <body> for ${Math.round(back.t - lost.t)} ms after ${first.focus}` : `focus moved from ${first.focus} to <body>` });
  }

  // Scroll: jumps that are not the transition's intended end position, and a
  // jump to the top before the new content is in (the old page moves first).
  const scrolls = frames.map((f) => f.scroll);
  const moved = scrolls.some((s) => Math.abs(s - first.scroll) > 2);
  if (moved) {
    if (expectScroll === null) {
      out.push({ kind: 'scroll-jump', detail: `scroll ${first.scroll} → ${last.scroll} (min ${Math.min(...scrolls)})` });
    } else {
      const reach = frames.find((f) => Math.abs(f.scroll - expectScroll) <= 2);
      const content = frames.find((f) => f.added > 0 || f.removed > 0);
      if (reach && content && reach.t < content.t - 1) out.push({ kind: 'scroll-before-commit', at: Math.round(reach.t - t0), detail: `scrolled ${Math.round(content.t - reach.t)} ms before the new content arrived` });
      const bounce = scrolls.filter((s, i) => i > 0 && Math.abs(s - scrolls[i - 1]) > 2).length;
      if (bounce > 1) out.push({ kind: 'scroll-jump', detail: `scroll moved ${bounce} times` });
    }
  }

  // Theme: the page background or theme attribute changes more often than the
  // transition asked for.
  // bg is the page background's luminance: a jump of more than 0.2 is light ↔ dark.
  const jumps = frames.filter((f, i) => i > 0 && Math.abs(f.bg - frames[i - 1].bg) > 0.2);
  const allowed = expectThemeChange ? 1 : 0;
  if (jumps.length > allowed) out.push({ kind: 'theme-flash', at: Math.round(jumps[0].t - t0), detail: `page background jumped light ↔ dark ${jumps.length} times (${frames.map((f) => f.bg).filter((v, i, a) => i === 0 || v !== a[i - 1]).join(' → ')})` });
  else if (!expectThemeChange && Math.abs(first.bg - last.bg) > 0.2) out.push({ kind: 'theme-flash', at: 0, detail: `first frame background luminance ${first.bg}, final ${last.bg}` });

  // Images that pop in over nothing: pending with no tinted placeholder.
  const bare = Math.max(0, ...frames.map((f) => f.bareImgs || 0));
  if (bare > 0) out.push({ kind: 'image-pop', detail: `${bare} images loading over a transparent box` });

  return out;
}

/** The first frame from which nothing changes any more, for timing a settle. */
export function settledAt(frames) {
  for (let i = frames.length - 1; i > 0; i--) {
    const a = frames[i];
    const b = frames[i - 1];
    if (a.text !== b.text || a.skel !== b.skel || a.h !== b.h || a.added || a.removed) return frames[i].t - frames[0].t;
  }
  return 0;
}
