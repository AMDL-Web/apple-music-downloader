// Drag-to-reorder for rows in one or more lists inside a container. A row is
// picked up by a mouse press that moves past 4 px (only inside `handle` when
// one is given) or a touch held still for 300 ms; it lifts as a clone that
// follows the pointer vertically (fixed to <body>, so a scrolling sheet does
// not clip it), its place shows a slot as tall as the row, and the other rows
// slide out of the way (FLIP). On release the clone glides into the slot and
// the order commits; Escape or a cancelled pointer sends it back. While a
// drag runs the page does not scroll, select text or open a callout menu,
// and the container keeps its height so nothing outside it moves. The index
// math is lib/sortable.js. Keyboard moves belong to the caller, which calls
// animate() before committing so the rows slide there too.
//
// DOM contract: rows carry data-sort-id (spread row(id) on them) and sit
// inside an element with data-sort-list="<list>"; other elements that move
// when rows do (a heading between lists) carry data-sort-flip="<key>". A
// list whose box is drawn by its ::before marks it data-sort-box="<key>" and
// gives the ::before `transform: translateY(var(--sort-y)) scaleY(var(--sort-scale))`
// from its top, moved over var(--sort-dur), so the box grows and shrinks
// with the rows sliding in or out (a transform: no layout runs per frame).

import { useState, useRef, useLayoutEffect, useEffect } from './preact.js';
import { insertionIndex, listAt, locate, placeItem, samePlace } from '../lib/sortable.js';
import { MOVE_MS, EASE, reducedMotion, translateY, snapshot, measure, makeGhost, playFlip, resetFlip } from './sortable-flip.js';

const MOUSE_SLOP = 4;
const TOUCH_SLOP = 8;
const LONG_PRESS_MS = 300;
// Presses that never start a drag: the row's own controls.
const IGNORE = 'button, input, select, textarea, [data-sort-ignore]';

/**
 * Options: container (a ref to the element holding the lists), lists
 * ({ <list>: ids[] }, the committed order), canDrag(id), resolve({ id, from,
 * list, index, current }) → { list, index } (the caller's rules; default:
 * the place the pointer is over), onDrop({ id, from, to, lists }) (lists: the
 * order after the drop; not called when the item ends where it started),
 * handle (a selector a mouse press must start in; touch long-press works
 * anywhere on the row).
 *
 * Returns { drag, lists, row, animate }: drag is null or { id, from, to,
 * height } while a row is held; lists is the order to draw (the held row at
 * `to`); row(id) gives the props a row spreads (data-sort-id, the slot
 * marker and height, the press handler); animate() makes the next render
 * slide rows from where they are drawn now.
 */
export function useSortable(options) {
  const opt = useRef(options);
  opt.current = options;
  const [drag, setDrag] = useState(null);
  const s = useRef({ press: null, active: null, snap: null, layout: null, settle: null, done: null, off: [] }).current;

  const box = () => opt.current.container.current;

  const listen = (target, type, fn, o) => {
    target.addEventListener(type, fn, o);
    s.off.push(() => target.removeEventListener(type, fn, o));
  };
  const unlisten = () => {
    for (const f of s.off.splice(0)) f();
  };

  const release = () => {
    if (s.press && s.press.timer) clearTimeout(s.press.timer);
    s.press = null;
    unlisten();
    delete document.documentElement.dataset.sorting;
  };

  const show = (a) => setDrag({ id: a.id, from: a.from, to: a.to, height: a.height });

  const move = (y) => {
    const a = s.active;
    if (!a || s.settle || s.done) return;
    a.y = y;
    const c = box();
    const cr = c.getBoundingClientRect();
    const top = Math.max(cr.top, Math.min(y - a.grab, cr.bottom - a.height));
    a.ghost.style.transform = `translate3d(0, ${top - a.top0}px, 0)`;
    if (!s.layout) return;
    const center = top + a.height / 2 - cr.top;
    const list = listAt(s.layout.blocks, center);
    if (list == null) return;
    const mids = s.layout.rows.filter((r) => r.list === list && r.id !== a.id).map((r) => r.top + r.height / 2);
    const index = insertionIndex(mids, center);
    const resolve = opt.current.resolve;
    const to = resolve ? resolve({ id: a.id, from: a.from, list, index, current: a.to }) : { list, index };
    if (!to || samePlace(to, a.to)) return;
    a.to = to;
    s.snap = snapshot(box());
    show(a);
  };

  const begin = (press, y) => {
    const c = box();
    const el = c && c.querySelector(`[data-sort-id="${CSS.escape(press.id)}"]`);
    const from = locate(opt.current.lists, press.id);
    if (!el || !from) {
      release();
      return;
    }
    const r = el.getBoundingClientRect();
    const ghost = makeGhost(el, r);
    requestAnimationFrame(() => {
      if (ghost.isConnected && !s.settle) ghost.dataset.lifted = '';
    });
    c.style.minHeight = `${c.getBoundingClientRect().height}px`;
    const focused = c.contains(document.activeElement);
    s.active = { id: press.id, from, to: from, height: r.height, grab: y - r.top, top0: r.top, left0: r.left, y, ghost, focused };
    s.layout = measure(box());
    if (press.timer) clearTimeout(press.timer);
    press.timer = null;
    try {
      c.setPointerCapture(press.pointerId);
    } catch {
      // A pointer that already went away: pointerup or pointercancel follows.
    }
    document.documentElement.dataset.sorting = '';
    const sel = window.getSelection && window.getSelection();
    if (sel && sel.rangeCount) sel.removeAllRanges();
    listen(window, 'keydown', onKey, true);
    listen(window, 'scroll', onScroll, true);
    show(s.active);
    move(y);
  };

  // Ends a drag: draws the held row where it lands (its start, when cancelled),
  // then the layout effect glides the clone there.
  const end = (commit) => {
    const a = s.active;
    if (!a || s.settle || s.done) return;
    release();
    if (!commit && !samePlace(a.to, a.from)) {
      a.to = a.from;
      s.snap = snapshot(box());
    }
    s.settle = { commit };
    show(a);
  };

  const glide = () => {
    const a = s.active;
    const { commit } = s.settle;
    const finish = () => {
      if (s.done) return;
      const moved = commit && !samePlace(a.to, a.from);
      s.done = { ghost: a.ghost, id: a.id, focused: a.focused };
      s.settle = null;
      s.active = null;
      s.layout = null;
      if (moved) opt.current.onDrop({ id: a.id, from: a.from, to: a.to, lists: placeItem(opt.current.lists, a.id, a.to.list, a.to.index), focused: a.focused });
      setDrag(null);
    };
    const slot = box().querySelector(`[data-sort-id="${CSS.escape(a.id)}"]`);
    if (!slot || reducedMotion()) {
      finish();
      return;
    }
    const r = slot.getBoundingClientRect();
    const top = r.top - translateY(slot);
    delete a.ghost.dataset.lifted;
    a.ghost.style.transition = `transform ${MOVE_MS}ms ${EASE}`;
    a.ghost.style.transform = `translate3d(${r.left - a.left0}px, ${top - a.top0}px, 0)`;
    const timer = setTimeout(finish, MOVE_MS + 60);
    a.ghost.addEventListener('transitionend', (e) => {
      if (e.target !== a.ghost) return;
      clearTimeout(timer);
      finish();
    });
  };

  function onKey(e) {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    end(false);
  }

  function onScroll() {
    if (s.active) move(s.active.y);
  }

  // The click that follows a drop would open what the row links to.
  const swallowClick = () => {
    const swallow = (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
    };
    window.addEventListener('click', swallow, true);
    setTimeout(() => window.removeEventListener('click', swallow, true), 0);
  };

  // Mouse and pen drags, and a touch press until its long-press fires.
  const onPointerMove = (e) => {
    const p = s.press;
    if (!p || e.pointerId !== p.pointerId) return;
    if (s.active) {
      if (p.type === 'touch') return;
      e.preventDefault();
      move(e.clientY);
      return;
    }
    const d = Math.hypot(e.clientX - p.x, e.clientY - p.y);
    if (p.type === 'touch') {
      if (d > TOUCH_SLOP) release();
    } else if (d > MOUSE_SLOP) {
      begin(p, e.clientY);
    }
  };

  const onPointerUp = (e) => {
    const p = s.press;
    if (!p || e.pointerId !== p.pointerId) return;
    if (s.active && p.type !== 'touch') {
      swallowClick();
      end(true);
    } else if (!s.active) {
      release();
    }
  };

  // A touch drag ignores pointercancel: Chrome sends one when its own
  // long-press fires under a held row, though the touch goes on.
  const onPointerCancel = (e) => {
    const p = s.press;
    if (!p || e.pointerId !== p.pointerId) return;
    if (!s.active) release();
    else if (p.type !== 'touch') end(false);
  };

  // Once held, a touch drag follows touch events on the node the finger went
  // down on: they keep coming there even after the row is drawn anew in the
  // other list, where they no longer bubble to the container. Cancelling them
  // keeps the page from scrolling; the first touchmove after the hold is
  // still cancelable, since the finger held still and no scroll began.
  const onTouchMove = (e) => {
    if (!s.active || !s.press || s.press.type !== 'touch') return;
    if (e.cancelable) e.preventDefault();
    const t = e.targetTouches[0] || e.changedTouches[0];
    if (t && e.currentTarget === s.press.node) move(t.clientY);
  };
  const onTouchEnd = (e) => {
    if (!s.active || !s.press || s.press.type !== 'touch') return;
    if (e.type === 'touchend') {
      swallowClick();
      end(true);
    } else {
      end(false);
    }
  };
  const onContextMenu = (e) => {
    if (s.press || s.active) e.preventDefault();
  };
  const onSelectStart = (e) => {
    if (s.press || s.active) e.preventDefault();
  };

  const onPointerDown = (e, id) => {
    if (s.press || s.active || s.settle || s.done) return;
    if (e.button !== 0 || !e.isPrimary) return;
    if (e.target.closest(IGNORE)) return;
    const o = opt.current;
    if (o.canDrag && !o.canDrag(id)) return;
    if (o.handle && e.pointerType === 'mouse' && !e.target.closest(o.handle)) return;
    const p = { id, pointerId: e.pointerId, type: e.pointerType, x: e.clientX, y: e.clientY, node: e.target, timer: null };
    s.press = p;
    listen(window, 'pointermove', onPointerMove, { passive: false });
    listen(window, 'pointerup', onPointerUp);
    listen(window, 'pointercancel', onPointerCancel);
    listen(window, 'contextmenu', onContextMenu, true);
    listen(document, 'selectstart', onSelectStart, true);
    if (p.type === 'touch') {
      listen(p.node, 'touchmove', onTouchMove, { passive: false });
      listen(p.node, 'touchend', onTouchEnd);
      listen(p.node, 'touchcancel', onTouchEnd);
      p.timer = setTimeout(() => {
        if (s.press === p) begin(p, p.y);
      }, LONG_PRESS_MS);
    }
  };

  // After each render: slide what moved (FLIP), note the layout for hit
  // tests, glide a released clone, and tidy up after a drop.
  useLayoutEffect(() => {
    const c = box();
    if (!c) return;
    const snap = s.snap;
    s.snap = null;
    if (s.done) {
      const { ghost, id, focused } = s.done;
      s.done = null;
      resetFlip(c);
      ghost.remove();
      c.style.minHeight = '';
      // A row moved between lists is a new element; keep focus on it.
      if (focused && !c.contains(document.activeElement)) {
        const el = c.querySelector(`[data-sort-id="${CSS.escape(id)}"]`);
        if (el) el.focus({ preventScroll: true });
      }
    }
    if (snap) playFlip(c, snap);
    if (s.active) s.layout = measure(box());
    if (s.settle && !s.settle.started) {
      s.settle.started = true;
      glide();
    }
  });

  // The container's touchmove listener is there before any touch starts, so
  // touch streams that start in it are cancelable (one added at touchstart
  // would see a passive stream). On unmount, a drag cut short takes its
  // clone with it.
  useEffect(() => {
    const c = box();
    const hold = (e) => {
      if (s.active && e.cancelable) e.preventDefault();
    };
    if (c) c.addEventListener('touchmove', hold, { passive: false });
    return () => {
      if (c) c.removeEventListener('touchmove', hold, { passive: false });
      release();
      if (s.active) s.active.ghost.remove();
      if (s.done) s.done.ghost.remove();
    };
  }, []);

  const lists = drag ? placeItem(options.lists, drag.id, drag.to.list, drag.to.index) : options.lists;
  const row = (id) => {
    const slot = drag && drag.id === id;
    return {
      'data-sort-id': id,
      'data-sort-slot': slot ? '' : undefined,
      style: slot ? { height: `${drag.height}px` } : undefined,
      onPointerDown: (e) => onPointerDown(e, id),
      onDragStart: (e) => e.preventDefault(),
    };
  };
  const animate = () => {
    s.snap = snapshot(box());
  };
  return { drag, lists, row, animate };
}
