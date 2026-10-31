// The motion of ui/sortable.js: where rows are drawn and laid out, the
// FLIP slide of rows and list boxes, and the lifted copy of a row.

// The rows' slide and the drop glide; --ease-out in tokens.css.
export const MOVE_MS = 180;
export const EASE = 'cubic-bezier(0.2, 0, 0, 1)';

export const reducedMotion = () => window.matchMedia('(prefers-reduced-motion: reduce)').matches;

export function translateY(el) {
  const t = getComputedStyle(el).transform;
  return t && t !== 'none' ? new DOMMatrixReadOnly(t).m42 : 0;
}

const flipKey = (el) => (el.dataset.sortId != null ? `r:${el.dataset.sortId}` : `f:${el.dataset.sortFlip}`);

const flipEls = (c) => Array.from(c.querySelectorAll('[data-sort-id], [data-sort-flip]'));

// Where each moving element is drawn now, relative to the container.
export function snapshot(c) {
  if (!c || reducedMotion()) return null;
  const top = c.getBoundingClientRect().top;
  const map = new Map();
  for (const el of flipEls(c)) map.set(flipKey(el), el.getBoundingClientRect().top - top);
  // A box as drawn: its rect under its ::before's transform, which may be mid-way.
  for (const el of c.querySelectorAll('[data-sort-box]')) {
    const r = el.getBoundingClientRect();
    const t = getComputedStyle(el, '::before').transform;
    const m = t && t !== 'none' ? new DOMMatrixReadOnly(t) : null;
    map.set(`b:${el.dataset.sortBox}`, { top: r.top + (m ? m.m42 : 0) - top, height: r.height * (m ? m.m22 : 1) });
  }
  return map;
}

// Sets a box's ::before transform (CSS reads --sort-y, --sort-scale, --sort-dur).
function boxTo(el, y, scale, ms) {
  el.style.setProperty('--sort-dur', `${ms}ms`);
  el.style.setProperty('--sort-y', `${y}px`);
  el.style.setProperty('--sort-scale', String(scale));
}

// Layout positions (transforms taken out) of rows and lists, relative to the container.
export function measure(c) {
  const top = c.getBoundingClientRect().top;
  const rows = Array.from(c.querySelectorAll('[data-sort-id]')).map((el) => {
    const r = el.getBoundingClientRect();
    const list = el.closest('[data-sort-list]');
    return { id: el.dataset.sortId, list: list && list.dataset.sortList, top: r.top - translateY(el) - top, height: r.height };
  });
  const blocks = Array.from(c.querySelectorAll('[data-sort-list]')).map((el) => {
    const r = el.getBoundingClientRect();
    return { id: el.dataset.sortList, top: r.top - top, bottom: r.bottom - top };
  });
  return { rows, blocks };
}

/** A copy of row `el` (drawn at `r`), fixed on <body>, not focusable and hidden from assistive tech. */
export function makeGhost(el, r) {
  const ghost = document.createElement('div');
  ghost.className = 'sortable-ghost';
  ghost.setAttribute('aria-hidden', 'true');
  const clone = el.cloneNode(true);
  for (const n of [clone, ...clone.querySelectorAll('[id], [tabindex], [data-sort-id]')]) {
    n.removeAttribute('id');
    n.removeAttribute('tabindex');
    n.removeAttribute('data-sort-id');
  }
  clone.inert = true;
  Object.assign(clone.style, { width: '100%', height: '100%', margin: '0' });
  Object.assign(ghost.style, {
    position: 'fixed',
    left: `${r.left}px`,
    top: `${r.top}px`,
    width: `${r.width}px`,
    height: `${r.height}px`,
    margin: '0',
    zIndex: '200',
    pointerEvents: 'none',
  });
  ghost.appendChild(clone);
  document.body.appendChild(ghost);
  return ghost;
}

/** Slides each moving element of `c` from where `snap` saw it (FLIP), and its boxes. */
export function playFlip(c, snap) {
  const top = c.getBoundingClientRect().top;
  const els = flipEls(c);
  for (const el of els) {
    el.style.transition = 'none';
    el.style.transform = '';
  }
  const moved = [];
  let slot = null;
  for (const el of els) {
    // The slot is empty space: it appears where it lands, and its own
    // CSS animation (restarted below) fades it in as the rows clear it.
    if (el.hasAttribute('data-sort-slot')) {
      slot = el;
      continue;
    }
    const first = snap.get(flipKey(el));
    if (first == null) continue;
    const dy = first - (el.getBoundingClientRect().top - top);
    if (Math.abs(dy) < 0.5) continue;
    el.style.transform = `translateY(${dy}px)`;
    moved.push(el);
  }
  // A list's box grows or shrinks with its rows instead of jumping.
  const boxes = [];
  for (const el of c.querySelectorAll('[data-sort-box]')) {
    const first = snap.get(`b:${el.dataset.sortBox}`);
    if (!first) continue;
    const r = el.getBoundingClientRect();
    if (r.height < 1) continue;
    const dy = first.top - (r.top - top);
    const scale = first.height / r.height;
    boxTo(el, dy, scale, 0);
    if (Math.abs(dy) >= 0.5 || Math.abs(first.height - r.height) >= 0.5) boxes.push(el);
  }
  void c.offsetHeight;
  for (const el of els) el.style.transition = moved.includes(el) ? `transform ${MOVE_MS}ms ${EASE}` : '';
  for (const el of moved) el.style.transform = '';
  for (const el of boxes) boxTo(el, 0, 1, MOVE_MS);
  if (slot && slot.getAnimations) {
    for (const a of slot.getAnimations({ subtree: true })) {
      a.cancel();
      a.play();
    }
  }
}

/** Ends every slide in `c` at once: no transforms, boxes at rest. */
export function resetFlip(c) {
  for (const el of flipEls(c)) {
    el.style.transition = 'none';
    el.style.transform = '';
  }
  for (const el of c.querySelectorAll('[data-sort-box]')) boxTo(el, 0, 1, 0);
  void c.offsetHeight;
  for (const el of flipEls(c)) el.style.transition = '';
}
