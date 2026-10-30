// Geometry and scale rules shared by the in-page probe and the Node side.
// Pure and dependency-free (the probe injects this file with `export`
// stripped), so every rule here has a Node test.

/** Merges [start, end] intervals and returns the gaps inside [lo, hi], largest first. */
export function gaps(intervals, lo, hi) {
  const iv = intervals
    .map(([a, b]) => [Math.max(lo, a), Math.min(hi, b)])
    .filter(([a, b]) => b > a)
    .sort((x, y) => x[0] - y[0]);
  const out = [];
  let cur = lo;
  for (const [a, b] of iv) {
    if (a > cur) out.push({ start: cur, end: a, size: a - cur });
    cur = Math.max(cur, b);
  }
  if (hi > cur) out.push({ start: cur, end: hi, size: hi - cur });
  return out.sort((x, y) => y.size - x.size);
}

/**
 * A blank band worth reporting: larger than `minPx` and than `ratio` of the
 * container's height. `edge` says whether it touches the top or bottom of
 * the content box (a void on one side: unbalanced composition). Equal room
 * above and below is centred content, not a void: neither edge counts.
 */
export function blankBands(intervals, lo, hi, { minPx = 48, ratio = 0.25 } = {}) {
  const h = hi - lo;
  if (h <= 0) return [];
  const all = gaps(intervals, lo, hi).map((g) => ({ ...g, edge: g.start <= lo + 0.5 ? 'top' : g.end >= hi - 0.5 ? 'bottom' : 'inside', share: g.size / h }));
  const top = all.find((g) => g.edge === 'top');
  const bottom = all.find((g) => g.edge === 'bottom');
  const centred = top && bottom && Math.abs(top.size - bottom.size) <= Math.max(8, 0.2 * Math.max(top.size, bottom.size));
  return all.filter((g) => g.size > minPx && g.size > ratio * h && !(centred && g.edge !== 'inside'));
}

/** Snaps a px value to the nearest scale step; returns {step, off} where off is the distance. */
export function nearestStep(v, scale) {
  let best = scale[0];
  for (const s of scale) if (Math.abs(s - v) < Math.abs(best - v)) best = s;
  return { step: best, off: Math.abs(best - v) };
}

export function onScale(v, scale, tol = 0.5) {
  return nearestStep(v, scale).off <= tol;
}

/** The most common value (within tol) and how many values fall outside it. */
export function modeOf(values, tol = 1) {
  if (!values.length) return { mode: null, outliers: [] };
  const sorted = values.slice().sort((a, b) => a - b);
  let best = sorted[0];
  let bestN = 0;
  for (const v of sorted) {
    const n = sorted.filter((x) => Math.abs(x - v) <= tol).length;
    if (n > bestN) {
      best = v;
      bestN = n;
    }
  }
  return { mode: best, count: bestN, outliers: values.filter((x) => Math.abs(x - best) > tol) };
}

/**
 * Shape of a state highlight against the rounded surface that holds it.
 * hl, box: {left, top, right, bottom}; hlRadii / boxRadii: [tl, tr, br, bl];
 * clips: the surface clips its children. Returns a list of problems.
 */
export function shapeProblems(hl, hlRadiiRaw, box, boxRadiiRaw, { clips = false, tol = 1.5 } = {}) {
  const out = [];
  // A radius larger than half the side draws as half the side (a pill).
  const half = (r, w, h) => r.map((v) => Math.min(v, w / 2, h / 2));
  const hlRadii = half(hlRadiiRaw, hl.right - hl.left, hl.bottom - hl.top);
  const boxRadii = half(boxRadiiRaw, box.right - box.left, box.bottom - box.top);
  const inset = { left: hl.left - box.left, top: hl.top - box.top, right: box.right - hl.right, bottom: box.bottom - hl.bottom };
  // Not inside the surface: nothing to compare.
  if (inset.left < -tol || inset.top < -tol || inset.right < -tol || inset.bottom < -tol) return out;
  const corners = [
    ['top-left', 0, inset.left, inset.top],
    ['top-right', 1, inset.right, inset.top],
    ['bottom-right', 2, inset.right, inset.bottom],
    ['bottom-left', 3, inset.left, inset.bottom],
  ];
  for (const [name, i, ix, iy] of corners) {
    const R = boxRadii[i];
    if (R <= tol) continue;
    if (ix >= R || iy >= R) continue;
    const expected = Math.max(0, R - Math.max(ix, iy));
    const r = hlRadii[i];
    if (!clips && r + tol < expected) {
      out.push({ kind: 'square-in-round', corner: name, radius: r, expected: round(expected), parentRadius: R, inset: [round(ix), round(iy)] });
    } else if (r > R - Math.min(ix, iy) + tol && Math.min(ix, iy) > tol && Math.max(ix, iy) <= 8) {
      out.push({ kind: 'radius-too-large', corner: name, radius: r, expected: round(R - Math.min(ix, iy)), parentRadius: R, inset: [round(ix), round(iy)] });
    }
  }
  const flushX = Math.abs(inset.left) <= tol && Math.abs(inset.right) <= tol;
  const flushY = Math.abs(inset.top) <= tol && Math.abs(inset.bottom) <= tol;
  const smallY = inset.top > tol && inset.bottom > tol && inset.top + inset.bottom < 32;
  const smallX = inset.left > tol && inset.right > tol && inset.left + inset.right < 32;
  if (flushX && smallY) out.push({ kind: 'uneven-inset', detail: 'flush left and right, inset top and bottom', inset: roundAll(inset) });
  if (flushY && smallX) out.push({ kind: 'uneven-inset', detail: 'flush top and bottom, inset left and right', inset: roundAll(inset) });
  return out;
}

function round(v) {
  return Math.round(v * 10) / 10;
}

function roundAll(o) {
  const r = {};
  for (const k of Object.keys(o)) r[k] = round(o[k]);
  return r;
}

/**
 * How far a box moved along one axis, from the change of its two edges: the
 * smaller one. A box that changed size with one edge held (a right-aligned
 * label a digit shorter, a card that grew at the bottom) did not move.
 */
export function edgeShift(startDelta, endDelta) {
  return Math.abs(startDelta) <= Math.abs(endDelta) ? startDelta : endDelta;
}

/** Spread of a list of edges: max − min. */
export function spread(values) {
  if (!values.length) return 0;
  return Math.max(...values) - Math.min(...values);
}

/** Rect intersection area. */
export function overlapArea(a, b) {
  const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
  const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
  return w > 0 && h > 0 ? w * h : 0;
}

/** Cumulative layout shift as session windows (1 s gap, 5 s cap): {total, maxWindow}. */
export function clsWindows(entries) {
  let total = 0;
  let max = 0;
  let win = 0;
  let start = -Infinity;
  let last = -Infinity;
  for (const e of entries.slice().sort((a, b) => a.t - b.t)) {
    total += e.value;
    if (e.t - last > 1000 || e.t - start > 5000) {
      win = 0;
      start = e.t;
    }
    win += e.value;
    last = e.t;
    if (win > max) max = win;
  }
  return { total, maxWindow: max };
}
