// The in-page half of the audit. probe.mjs wraps this file, together with
// lib/color.mjs, lib/geometry.mjs and lib/text.mjs, into one expression that
// page.evaluate runs (CDP evaluation is not subject to the page's CSP). It
// defines window.__uia: settle, scan, state sampling, shift and frame
// recorders. It never writes styles or markup into the page.

const uia = (window.__uia = window.__uia || {});

const cs = (el, pseudo) => getComputedStyle(el, pseudo);
const px = (v) => parseFloat(v) || 0;

function rectOf(el) {
  const r = el.getBoundingClientRect();
  return { left: r.left + scrollX, top: r.top + scrollY, right: r.right + scrollX, bottom: r.bottom + scrollY, width: r.width, height: r.height };
}

function viewRect(el) {
  const r = el.getBoundingClientRect();
  return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
}

function visible(el) {
  if (!el.isConnected) return false;
  if (el.checkVisibility) {
    if (!el.checkVisibility({ opacityProperty: true, visibilityProperty: true, contentVisibilityAuto: true })) return false;
  } else {
    const s = cs(el);
    if (s.display === 'none' || s.visibility === 'hidden' || s.opacity === '0') return false;
  }
  const r = el.getBoundingClientRect();
  return r.width > 0 && r.height > 0;
}

/** Visually hidden for screen readers: the element or a near ancestor is a clipped 1 px box. */
function srOnly(el) {
  for (let cur = el, i = 0; cur && cur.nodeType === 1 && i < 4; cur = cur.parentElement, i++) {
    const r = cur.getBoundingClientRect();
    const s = cs(cur);
    if ((r.width <= 1 && r.height <= 1) || s.clipPath === 'inset(50%)' || s.clip === 'rect(0px, 0px, 0px, 0px)') return true;
  }
  return false;
}

const STATE_CLASS = /^(is-|has-|was-)/;

function sig(el) {
  const cls = [...el.classList].filter((c) => !STATE_CLASS.test(c)).sort();
  return el.tagName.toLowerCase() + (cls.length ? '.' + cls.join('.') : '');
}

const KEY_ATTRS = ['data-section', 'data-page', 'data-key', 'data-dialog', 'data-group', 'data-panel', 'role'];

/** A readable selector path: up to four steps, stopping at an id or a section hook. */
function sel(el) {
  const parts = [];
  let cur = el;
  for (let depth = 0; cur && cur.nodeType === 1 && depth < 4; depth++) {
    let p = cur.tagName.toLowerCase();
    if (cur.id) {
      parts.unshift(`${p}#${cur.id}`);
      break;
    }
    const cls = [...cur.classList].slice(0, 3);
    if (cls.length) p += '.' + cls.join('.');
    for (const a of KEY_ATTRS) if (cur.hasAttribute(a) && a !== 'role') p += `[${a}=${cur.getAttribute(a)}]`;
    const parent = cur.parentElement;
    if (parent && !cls.length) {
      const same = [...parent.children].filter((c) => c.tagName === cur.tagName);
      if (same.length > 1) p += `:nth-of-type(${same.indexOf(cur) + 1})`;
    }
    parts.unshift(p);
    if (cur.hasAttribute('data-section') || cur.hasAttribute('data-dialog')) break;
    cur = parent;
  }
  return parts.join(' > ');
}

function ownText(el) {
  let t = '';
  for (const n of el.childNodes) if (n.nodeType === 3) t += n.nodeValue;
  return t.replace(/\s+/g, ' ').trim();
}

/** The box text inside `el` can paint in: its own and its ancestors' overflow clips. */
function clipBox(el, visibleOnly = false) {
  let box = { left: -Infinity, top: -Infinity, right: Infinity, bottom: Infinity };
  for (let cur = el, i = 0; cur && cur.nodeType === 1 && (visibleOnly || i < 6); cur = cur.parentElement, i++) {
    const s = cs(cur);
    // A scroller's hidden part is scrolled away, not cut: only hidden/clip
    // truncate. What is on screen (visibleOnly) excludes both.
    const cuts = (v) => v === 'hidden' || v === 'clip' || (visibleOnly && (v === 'auto' || v === 'scroll'));
    if (cuts(s.overflowX) || cuts(s.overflowY) || (s.webkitLineClamp && s.webkitLineClamp !== 'none')) {
      const r = rectOf(cur);
      box = { left: Math.max(box.left, r.left), top: Math.max(box.top, r.top), right: Math.min(box.right, r.right), bottom: Math.min(box.bottom, r.bottom) };
    }
    if (s.position === 'fixed') break;
  }
  return box;
}

function clipLines(lines, box) {
  return lines
    .map((l) => ({ left: Math.max(l.left, box.left), top: Math.max(l.top, box.top), right: Math.min(l.right, box.right), bottom: Math.min(l.bottom, box.bottom) }))
    .filter((l) => l.right - l.left > 1 && l.bottom - l.top > 2);
}

function textLines(el) {
  const rects = [];
  for (const n of el.childNodes) {
    if (n.nodeType !== 3 || !n.nodeValue.trim()) continue;
    const r = document.createRange();
    r.selectNodeContents(n);
    for (const b of r.getClientRects()) if (b.width > 0 && b.height > 0) rects.push({ left: b.left + scrollX, top: b.top + scrollY, right: b.right + scrollX, bottom: b.bottom + scrollY });
  }
  return clipLines(mergeLines(rects), clipBox(el));
}

function mergeLines(rects) {
  const lines = [];
  for (const r of rects.sort((a, b) => a.top - b.top || a.left - b.left)) {
    const l = lines.find((x) => Math.abs(x.top - r.top) < 2 && Math.abs(x.bottom - r.bottom) < 2);
    if (l) {
      l.left = Math.min(l.left, r.left);
      l.right = Math.max(l.right, r.right);
    } else lines.push({ ...r });
  }
  return lines;
}

function radii(s) {
  return [px(s.borderTopLeftRadius), px(s.borderTopRightRadius), px(s.borderBottomRightRadius), px(s.borderBottomLeftRadius)];
}

function effectiveOpacity(el) {
  let o = 1;
  for (let cur = el; cur && cur.nodeType === 1; cur = cur.parentElement) o *= parseFloat(cs(cur).opacity);
  return o;
}

/** Luminance of what the page shows behind everything. */
function pageLum() {
  const b = document.body ? parseColor(cs(document.body).backgroundColor) : null;
  const h = parseColor(cs(document.documentElement).backgroundColor);
  const c = b && b.a > 0 ? over(b, h && h.a > 0 ? over(h, canvasColor()) : canvasColor()) : h && h.a > 0 ? over(h, canvasColor()) : canvasColor();
  return Math.round(luminance(c) * 100) / 100;
}

function canvasColor() {
  let scheme = cs(document.documentElement).colorScheme || '';
  if (!scheme || scheme === 'normal') {
    const meta = document.querySelector('meta[name=color-scheme]');
    scheme = meta ? meta.content : 'light';
  }
  const dark = scheme.includes('dark') && (!scheme.includes('light') || matchMedia('(prefers-color-scheme: dark)').matches);
  return dark ? { r: 18, g: 18, b: 18, a: 1 } : { r: 255, g: 255, b: 255, a: 1 };
}

/** The colour behind `el`, composited from its ancestors; null when an image or gradient is in the way. */
function backdrop(el) {
  const layers = [];
  for (let cur = el; cur && cur.nodeType === 1; cur = cur.parentElement) {
    const s = cs(cur);
    if (s.backgroundImage && s.backgroundImage !== 'none') return null;
    const c = parseColor(s.backgroundColor);
    if (c && c.a > 0) {
      layers.push({ ...c, a: c.a * parseFloat(s.opacity) });
      if (c.a >= 1 && parseFloat(s.opacity) >= 1) break;
    }
  }
  let bg = canvasColor();
  for (const l of layers.reverse()) bg = over(l, bg);
  return bg;
}

function ownPaint(el, s = cs(el)) {
  const bg = parseColor(s.backgroundColor);
  const hasBg = (bg && bg.a > 0.04) || (s.backgroundImage && s.backgroundImage !== 'none');
  const hasBorder = ['Top', 'Right', 'Bottom', 'Left'].some((k) => px(s[`border${k}Width`]) > 0 && s[`border${k}Style`] !== 'none' && (parseColor(s[`border${k}Color`]) || { a: 0 }).a > 0.1);
  const hasShadow = s.boxShadow && s.boxShadow !== 'none';
  return { hasBg, hasBorder, hasShadow, bg };
}

const REPLACED = new Set(['IMG', 'SVG', 'VIDEO', 'CANVAS', 'INPUT', 'SELECT', 'TEXTAREA', 'PROGRESS', 'METER', 'IFRAME', 'svg']);

/** The open modal (the page under it is covered), else the body. */
function scopeRoot(cfg) {
  const modals = [...document.querySelectorAll(cfg.selectors.modal || '[aria-modal=true]')].filter(visible);
  return modals.length ? modals[modals.length - 1] : document.body;
}

/** Elements on different layers (the page, a fixed bar, a dialog) may overlap by design. */
function layerOf(el) {
  for (let cur = el; cur && cur.nodeType === 1; cur = cur.parentElement) {
    const s = cs(cur);
    if (s.position === 'fixed' || s.position === 'sticky' || cur.getAttribute('role') === 'dialog' || cur.getAttribute('aria-modal') === 'true') return cur;
  }
  return document.body;
}

uia.settle = async function settle({ quietMs = 350, timeoutMs = 6000 } = {}) {
  const start = performance.now();
  let last = performance.now();
  const mo = new MutationObserver(() => (last = performance.now()));
  mo.observe(document.documentElement, { subtree: true, childList: true, attributes: true, characterData: true });
  try {
    if (document.fonts && document.fonts.ready) await Promise.race([document.fonts.ready, new Promise((r) => setTimeout(r, 1500))]);
    for (;;) {
      await new Promise((r) => requestAnimationFrame(() => setTimeout(r, 50)));
      const now = performance.now();
      const imgsPending = [...document.images].some((i) => !i.complete && i.loading !== 'lazy' && visible(i));
      if (now - last >= quietMs && !imgsPending) break;
      if (now - start > timeoutMs) return { settled: false, ms: Math.round(now - start) };
    }
    const anims = document.getAnimations ? document.getAnimations().filter((a) => a.playState === 'running' && a.effect && a.effect.getTiming().iterations !== Infinity) : [];
    await Promise.race([Promise.all(anims.map((a) => a.finished.catch(() => {}))), new Promise((r) => setTimeout(r, 1200))]);
    return { settled: true, ms: Math.round(performance.now() - start) };
  } finally {
    mo.disconnect();
  }
};

/** The main scan over the settled page. cfg comes from the project config (core/config.mjs fills defaults). */
uia.scan = function scan(cfg) {
  const F = [];
  const add = (check, el, msg, data = {}, severity = 'medium') => F.push({ check, sel: el ? sel(el) : 'document', sig: el ? sig(el) : 'document', rect: el ? rectOf(el) : null, msg, data, severity });
  const ignore = cfg.selectors.ignore;
  const scope = scopeRoot(cfg);
  const els = [...scope.querySelectorAll('*')].filter((el) => !(ignore && el.closest(ignore)) && visible(el));
  const vw = document.documentElement.clientWidth;
  const metrics = { elements: els.length };
  const components = [];
  const rhythm = [];
  const styleCache = new Map();
  const S = (el) => {
    let s = styleCache.get(el);
    if (!s) styleCache.set(el, (s = cs(el)));
    return s;
  };
  const leaves = els.filter((el) => ownText(el) && !srOnly(el));

  // ---- overflow
  const se = document.scrollingElement || document.documentElement;
  if (se.scrollWidth > se.clientWidth + 1) {
    const wide = els.filter((el) => viewRect(el).right > vw + 1 && S(el).position !== 'fixed').sort((a, b) => viewRect(b).right - viewRect(a).right).slice(0, 3);
    add('overflow', wide[0] || null, `page scrolls sideways: ${se.scrollWidth}px content in a ${se.clientWidth}px viewport`, { scrollWidth: se.scrollWidth, clientWidth: se.clientWidth, widest: wide.map(sel) }, 'high');
  }
  const truncated = [];
  for (const el of leaves) {
    const s = S(el);
    const over = el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0;
    const overY = el.scrollHeight > el.clientHeight + 2 && el.clientHeight > 0;
    const text = el.textContent.replace(/\s+/g, ' ').trim();
    if (over && s.textOverflow === 'ellipsis') {
      truncated.push({ el, text, shown: (text.length * el.clientWidth) / el.scrollWidth });
      // A cut title is by design (full text in its tooltip); a cut number or label is not.
      if (text.length <= 14 && !el.closest('[dir=auto]') && (/\d/.test(text) || text.split(' ').length <= 2)) add('overflow', el, `a short value is cut by an ellipsis: "${text}"`, { width: el.clientWidth, needs: el.scrollWidth }, 'high');
    } else if (over && (s.overflowX === 'hidden' || s.overflowX === 'clip') && s.whiteSpace !== 'normal') {
      add('overflow', el, `text clipped without an ellipsis: "${text.slice(0, 60)}"`, { width: el.clientWidth, needs: el.scrollWidth }, 'high');
    } else if (over && s.overflowX === 'visible' && el.scrollWidth > el.clientWidth + 4 && s.display !== 'inline') {
      add('overflow', el, `text spills out of its box by ${el.scrollWidth - el.clientWidth}px: "${text.slice(0, 60)}"`, { width: el.clientWidth, needs: el.scrollWidth }, 'medium');
    }
    const clamped = (s.webkitLineClamp && s.webkitLineClamp !== 'none') || s.display === '-webkit-box';
    if (overY && (s.overflowY === 'hidden' || s.overflowY === 'clip') && !clamped && s.textOverflow !== 'ellipsis') {
      add('overflow', el, `text clipped vertically: ${el.scrollHeight}px in ${el.clientHeight}px`, { text: text.slice(0, 80) }, 'medium');
    }
  }
  const byParentSig = new Map();
  for (const t of truncated) {
    const row = t.el.closest(cfg.selectors.row) || t.el.parentElement;
    const key = `${sig(row.parentElement || row)}|${sig(t.el)}`;
    if (!byParentSig.has(key)) byParentSig.set(key, []);
    byParentSig.get(key).push(t);
  }
  for (const group of byParentSig.values()) {
    for (const d of hiddenDistinctions(group)) {
      const el = group.find((g) => g.text === d.texts[0]).el;
      add('overflow', el, `ellipsis hides what tells ${d.texts.length} rows apart: all read "${d.prefix}…"`, { texts: d.texts.slice(0, 4) }, 'medium');
    }
  }
  // Overlapping text boxes that are not nested.
  const textBoxes = leaves.map((el) => ({ el, lines: clipLines(textLines(el), clipBox(el, true)) })).filter((x) => x.lines.length);
  textBoxes.sort((a, b) => a.lines[0].top - b.lines[0].top);
  let overlaps = 0;
  for (let i = 0; i < textBoxes.length && overlaps < 12; i++) {
    for (let j = i + 1; j < textBoxes.length; j++) {
      const A = textBoxes[i];
      const B = textBoxes[j];
      if (B.lines[0].top > A.lines[A.lines.length - 1].bottom) break;
      if (A.el.contains(B.el) || B.el.contains(A.el)) continue;
      if (layerOf(A.el) !== layerOf(B.el)) continue;
      for (const la of A.lines)
        for (const lb of B.lines) {
          const area = overlapArea(la, lb);
          if (area > 12 && S(A.el).position !== 'fixed' && S(B.el).position !== 'fixed') {
            overlaps++;
            add('overflow', A.el, `text overlaps other text (${sel(B.el)})`, { area: Math.round(area), other: B.el.textContent.trim().slice(0, 40) }, 'high');
          }
        }
    }
  }

  // ---- noise and prose
  for (const el of leaves) {
    const t = el.textContent.replace(/\s+/g, ' ').trim();
    if (t.length > 160) continue;
    const why = noiseReason(t);
    if (why && !(cfg.allow.noise && el.closest(cfg.allow.noise))) add('noise', el, `shows ${why}: "${t}"`, { text: t }, why.includes('leaked') ? 'high' : 'low');
  }
  for (const el of leaves) {
    if (el.closest(cfg.selectors.interactive)) continue;
    if (cfg.allow.prose && el.closest(cfg.allow.prose)) continue;
    // Content (titles, names) arrives with dir=auto; prose is the UI's own copy.
    if (el.closest('[dir=auto]')) continue;
    const t = el.textContent.replace(/\s+/g, ' ').trim();
    const score = proseScore(t);
    if (score >= 2) add('prose', el, `explanatory text: "${t.slice(0, 120)}"`, { text: t, score }, 'low');
  }
  // Duplicated text within one row or card.
  const seenDup = new Set();
  for (const el of leaves) {
    const t = el.textContent.replace(/\s+/g, ' ').trim();
    if (t.length < 4 || t.length > 80 || seenDup.has(el)) continue;
    const rowScope = el.closest(cfg.selectors.row);
    if (!rowScope) continue;
    const twins = leaves.filter((o) => o !== el && rowScope.contains(o) && !o.contains(el) && !el.contains(o) && o.textContent.replace(/\s+/g, ' ').trim() === t);
    if (twins.length && !(cfg.allow.duplicate && el.closest(cfg.allow.duplicate))) {
      twins.forEach((o) => seenDup.add(o));
      add('noise', el, `the same text twice in one row: "${t}"`, { also: twins.map(sel).slice(0, 2) }, 'low');
    }
  }

  // ---- empty space and reserved invisible boxes
  const containers = els.filter((el) => el.matches(cfg.selectors.container) && viewRect(el).height >= 60);
  const bandsFound = [];
  for (const c of containers) {
    const s = S(c);
    const r = rectOf(c);
    const lo = r.top + px(s.borderTopWidth) + px(s.paddingTop);
    const hi = r.bottom - px(s.borderBottomWidth) - px(s.paddingBottom);
    const cPaint = ownPaint(c, s);
    const iv = [];
    for (const d of c.querySelectorAll('*')) {
      if (!visible(d) || srOnly(d)) continue;
      const ds = S(d);
      if (REPLACED.has(d.tagName) || d.tagName.toLowerCase() === 'svg') {
        const b = rectOf(d);
        iv.push([b.top, b.bottom]);
        continue;
      }
      const p = ownPaint(d, ds);
      if (p.hasBorder || p.hasShadow || (p.hasBg && !(cPaint.bg && p.bg && sameColor(p.bg, cPaint.bg)))) {
        const b = rectOf(d);
        iv.push([b.top, b.bottom]);
        continue;
      }
      for (const l of textLines(d)) iv.push([l.top, l.bottom]);
    }
    const bands = blankBands(iv, lo, hi, cfg.thresholds.emptySpace);
    for (const b of bands) bandsFound.push({ c, b, depth: depthOf(c) });
  }
  bandsFound.sort((a, b) => b.depth - a.depth);
  const kept = [];
  for (const f of bandsFound) {
    const inner = kept.find((k) => f.c.contains(k.c) && Math.min(k.b.end, f.b.end) - Math.max(k.b.start, f.b.start) > 0.6 * Math.min(k.b.size, f.b.size));
    if (inner) continue;
    kept.push(f);
    const hidden = [...f.c.querySelectorAll('*')].filter((d) => {
      if (visible(d)) return false;
      const b = rectOf(d);
      return b.height > 4 && b.top < f.b.end && b.bottom > f.b.start;
    });
    const why = hidden.length ? ` (invisible: ${sel(hidden[0])})` : S(f.c).minHeight !== '0px' && S(f.c).minHeight !== 'auto' ? ` (min-height ${S(f.c).minHeight})` : '';
    add('empty-space', f.c, `${Math.round(f.b.size)}px blank band ${f.b.edge === 'inside' ? 'inside' : 'at the ' + f.b.edge + ' of'} the box (${Math.round(f.b.share * 100)}% of its height)${why}`, { band: Math.round(f.b.size), share: Math.round(f.b.share * 100), edge: f.b.edge, at: Math.round(f.b.start - rectOf(f.c).top), cause: hidden.length ? sel(hidden[0]) : null }, f.b.size > 96 ? 'high' : 'medium');
  }
  const reported = [];
  for (const el of scope.querySelectorAll('*')) {
    if (ignore && el.closest(ignore)) continue;
    const s = S(el);
    if (s.display === 'none' || s.position === 'absolute' || s.position === 'fixed') continue;
    const invisible = s.visibility === 'hidden' || s.opacity === '0';
    if (!invisible || reported.some((r) => r.contains(el))) continue;
    const r = el.getBoundingClientRect();
    if (r.height < 12 || r.width * r.height < 400 || srOnly(el)) continue;
    let anc = el.parentElement;
    while (anc && !visible(anc)) anc = anc.parentElement;
    if (!anc) continue;
    reported.push(el);
    add('empty-space', el, `invisible element holds ${Math.round(r.width)}×${Math.round(r.height)}px of room`, { width: Math.round(r.width), height: Math.round(r.height), how: s.visibility === 'hidden' ? 'visibility:hidden' : 'opacity:0' }, r.height >= 24 ? 'medium' : 'low');
  }

  // ---- alignment: icons against their text, columns down a list, row heights
  for (const el of els) {
    const s = S(el);
    if (!(s.display.includes('flex') && !s.flexDirection.startsWith('column'))) continue;
    const kids = [...el.children].filter(visible);
    const icon = kids.find((k) => k.tagName.toLowerCase() === 'svg' || k.matches(cfg.selectors.icon));
    if (!icon) continue;
    const textEl = kids.find((k) => k !== icon && !srOnly(k) && k.textContent.trim() && !k.matches(cfg.selectors.icon)) || (ownText(el) ? el : null);
    if (!textEl) continue;
    const lines = textEl === el ? textLines(el) : leafLines(textEl);
    if (!lines.length) continue;
    const ir = rectOf(icon);
    const ic = (ir.top + ir.bottom) / 2;
    const first = lines[0];
    const fc = (first.top + first.bottom) / 2;
    const dy = ic - fc;
    if (lines.length > 1) {
      const blockC = (lines[0].top + lines[lines.length - 1].bottom) / 2;
      if (Math.abs(ic - blockC) < 2 && Math.abs(dy) > 3) add('alignment', icon, `icon centred on ${lines.length} wrapped lines instead of the first line (${dy.toFixed(1)}px low)`, { dy: +dy.toFixed(1) }, 'medium');
      continue;
    }
    if (Math.abs(dy) > cfg.thresholds.iconCentrePx) add('alignment', icon, `icon ${Math.abs(dy).toFixed(1)}px ${dy > 0 ? 'below' : 'above'} the centre of its text`, { dy: +dy.toFixed(1), text: textEl.textContent.trim().slice(0, 30) }, Math.abs(dy) > 3 ? 'medium' : 'low');
  }
  const parents = new Set(els.map((e) => e.parentElement).filter(Boolean));
  for (const p of parents) {
    const rows = [...p.children].filter(visible);
    const bySig = new Map();
    for (const r of rows) {
      const k = sig(r);
      if (!bySig.has(k)) bySig.set(k, []);
      bySig.get(k).push(r);
    }
    for (const [k, group] of bySig) {
      if (group.length < 3) continue;
      const stacked = group.every((g, i) => i === 0 || rectOf(g).top >= rectOf(group[i - 1]).bottom - 1);
      if (stacked) columnCheck(group, add, cfg);
      else rowHeightCheck(group, add, k);
    }
  }

  // ---- spacing scale (computed values)
  const scale = cfg.scale;
  const offScale = new Map();
  const note = (el, prop, v, list) => {
    if (v === 0 || Math.abs(v) <= 1) return;
    if (onScale(Math.abs(v), list, 0.6)) return;
    const key = `${sig(el)}|${prop}|${v}`;
    if (!offScale.has(key)) offScale.set(key, { el, prop, v, n: 0 });
    offScale.get(key).n++;
  };
  for (const el of els) {
    if (srOnly(el)) continue;
    const s = S(el);
    for (const side of ['Top', 'Right', 'Bottom', 'Left']) note(el, `padding-${side.toLowerCase()}`, px(s[`padding${side}`]), scale.space);
    if (s.display.includes('flex') || s.display.includes('grid')) {
      if (s.rowGap !== 'normal') note(el, 'row-gap', px(s.rowGap), scale.space);
      if (s.columnGap !== 'normal') note(el, 'column-gap', px(s.columnGap), scale.space);
    }
    if (s.position === 'static' || s.position === 'relative') for (const side of ['Top', 'Bottom']) note(el, `margin-${side.toLowerCase()}`, px(s[`margin${side}`]), scale.space);
    const p = ownPaint(el, s);
    if (p.hasBg || p.hasBorder) {
      const rr = radii(s);
      if (rr[0] > 0 && rr[0] < 999 && rr[0] * 2 < Math.min(el.offsetWidth, el.offsetHeight) - 1) note(el, 'border-radius', rr[0], scale.radius);
    }
    if (ownText(el)) note(el, 'font-size', px(s.fontSize), scale.font);
  }
  for (const o of offScale.values()) add('tokens', o.el, `${o.prop} ${o.v}px is off the ${o.prop.startsWith('font') ? 'type' : o.prop.includes('radius') ? 'radius' : 'spacing'} scale${o.n > 1 ? ` (${o.n} elements)` : ''}`, { prop: o.prop, value: o.v, count: o.n, nearest: nearestStep(o.v, o.prop.startsWith('font') ? scale.font : o.prop.includes('radius') ? scale.radius : scale.space).step }, 'low');

  // ---- accessibility: contrast, names, nav state, phone targets
  let contrastN = 0;
  for (const el of leaves) {
    if (contrastN > 40) break;
    if (el.closest('[aria-disabled=true],:disabled,[disabled]') || el.closest(cfg.allow.contrast || '#\\00')) continue;
    const s = S(el);
    const fg = parseColor(s.color);
    const bg = backdrop(el);
    if (!fg || !bg) continue;
    const op = effectiveOpacity(el);
    const ink = over({ ...fg, a: fg.a * op }, bg);
    const ratio = contrast(ink, bg);
    const size = px(s.fontSize);
    const large = size >= 24 || (size >= 18.66 && parseInt(s.fontWeight, 10) >= 700);
    const need = large ? 3 : 4.5;
    if (ratio < need - 0.05) {
      contrastN++;
      add('a11y', el, `text contrast ${ratio.toFixed(2)}:1, needs ${need}:1 ("${el.textContent.trim().slice(0, 40)}")`, { ratio: +ratio.toFixed(2), need, fg: colorText(ink), bg: colorText(bg) }, ratio < need - 1 ? 'high' : 'medium');
    }
  }
  for (const el of els) {
    if (el.tagName.toLowerCase() !== 'svg' || !el.closest(cfg.selectors.interactive) || el.closest('[aria-disabled=true],:disabled')) continue;
    const s = S(el);
    // Sprite icons paint with currentColor through <use>; the svg's own fill is the initial black.
    const fill = parseColor(s.color);
    const bg = backdrop(el.parentElement);
    if (!fill || !bg || fill.a === 0) continue;
    const ratio = contrast(over({ ...fill, a: fill.a * effectiveOpacity(el) }, bg), bg);
    if (ratio < 2.95) add('a11y', el, `icon contrast ${ratio.toFixed(2)}:1, needs 3:1`, { ratio: +ratio.toFixed(2) }, 'low');
  }
  const interactive = els.filter((el) => el.matches(cfg.selectors.interactive) && !srOnly(el));
  for (const el of interactive) {
    const name = (el.getAttribute('aria-label') || el.getAttribute('aria-labelledby') || el.getAttribute('title') || el.textContent || el.getAttribute('alt') || (el.querySelector('img[alt]') || {}).alt || el.value || el.placeholder || '').trim();
    const labelled = el.labels && el.labels.length;
    if (!name && !labelled) add('a11y', el, 'control without an accessible name', {}, 'high');
    const badge = el.querySelector(cfg.selectors.badge);
    if (badge && el.getAttribute('aria-label') && /\d/.test(badge.textContent) && !el.getAttribute('aria-label').includes(badge.textContent.trim())) add('a11y', el, `badge "${badge.textContent.trim()}" is not in the label "${el.getAttribute('aria-label')}"`, {}, 'low');
  }
  for (const nav of els.filter((el) => el.matches(cfg.selectors.nav))) {
    const links = [...nav.querySelectorAll('a[href]')].filter(visible);
    if (links.length >= 2 && !nav.querySelector('[aria-current]:not([aria-current=false])')) add('a11y', nav, 'navigation without an aria-current item', { links: links.length }, 'medium');
  }
  metrics.interactive = interactive.length;

  // ---- components, for the cross-page consistency report
  for (const [kind, q] of Object.entries(cfg.components)) {
    for (const el of els.filter((e) => e.matches(q))) {
      const s = S(el);
      const r = viewRect(el);
      components.push({ kind, sig: sig(el), sel: sel(el), h: Math.round(r.height), fs: px(s.fontSize), fw: s.fontWeight, pl: px(s.paddingLeft), pr: px(s.paddingRight), radius: radii(s)[0], lh: s.lineHeight, text: el.textContent.trim().slice(0, 24) });
    }
  }

  // ---- rhythm: the configured relationships
  for (const rel of cfg.rhythm) {
    for (const el of els.filter((e) => e.matches(rel.selector))) {
      const s = S(el);
      const r = rectOf(el);
      if (rel.measure === 'padding') {
        const kids = [...el.children].filter((k) => visible(k) && !srOnly(k) && S(k).position !== 'absolute');
        if (!kids.length) continue;
        const top = Math.min(...kids.map((k) => rectOf(k).top)) - r.top - px(s.borderTopWidth);
        const bottom = r.bottom - Math.max(...kids.map((k) => rectOf(k).bottom)) - px(s.borderBottomWidth);
        const left = Math.min(...kids.map((k) => rectOf(k).left)) - r.left - px(s.borderLeftWidth);
        const right = r.right - Math.max(...kids.map((k) => rectOf(k).right)) - px(s.borderRightWidth);
        rhythm.push({ name: rel.name, sel: sel(el), sig: sig(el), values: { top: rnd(top), right: rnd(right), bottom: rnd(bottom), left: rnd(left) }, rect: r });
      } else if (rel.measure === 'gapToNext') {
        let next = el.nextElementSibling;
        while (next && (!visible(next) || srOnly(next) || S(next).position === 'absolute' || S(next).position === 'fixed')) next = next.nextElementSibling;
        if (!next) continue;
        const nr = rectOf(next);
        rhythm.push({ name: rel.name, sel: sel(el), sig: sig(el), next: sig(next), values: { gap: rnd(nr.top - r.bottom) }, rect: r });
      } else if (rel.measure === 'edges') {
        const within = rel.within && el.parentElement ? el.parentElement.closest(rel.within) : null;
        const wr = within ? viewRect(within) : { left: 0, right: vw };
        rhythm.push({ name: rel.name, sel: sel(el), sig: sig(el), values: { left: rnd(viewRect(el).left - wr.left), right: rnd(wr.right - viewRect(el).right) }, rect: r });
      } else if (rel.measure === 'firstTextLeft') {
        const lead = [...el.querySelectorAll('*')].find((d) => visible(d) && ownText(d));
        if (lead) rhythm.push({ name: rel.name, sel: sel(el), sig: sig(el), values: { left: rnd(textLines(lead)[0]?.left - r.left) }, rect: r });
      }
    }
  }

  return { findings: F, metrics, components, rhythm };
};

function rnd(v) {
  return Math.round(v * 10) / 10;
}

function depthOf(el) {
  let d = 0;
  for (let cur = el; cur; cur = cur.parentElement) d++;
  return d;
}

function leafLines(el) {
  const out = [];
  const walk = (n) => {
    for (const c of n.childNodes) {
      if (c.nodeType === 3 && c.nodeValue.trim()) {
        const r = document.createRange();
        r.selectNodeContents(c);
        for (const b of r.getClientRects()) if (b.width > 0) out.push({ left: b.left + scrollX, top: b.top + scrollY, right: b.right + scrollX, bottom: b.bottom + scrollY });
      } else if (c.nodeType === 1 && visible(c)) walk(c);
    }
  };
  walk(el);
  return clipLines(mergeLines(out), clipBox(el));
}

/** Rows stacked down a list: each column's left and right edge should agree from row to row. */
function columnCheck(rows, add, cfg) {
  const cols = new Map();
  for (const row of rows) {
    const rr = rectOf(row);
    const walk = (el, path, depth) => {
      if (depth > 4) return;
      const kids = [...el.children].filter(visible);
      const counts = new Map();
      for (const k of kids) {
        const sg = sig(k);
        counts.set(sg, (counts.get(sg) || 0) + 1);
        const key = `${path}>${sg}#${counts.get(sg)}`;
        if (!cols.has(key)) cols.set(key, []);
        const r = rectOf(k);
        cols.get(key).push({ el: k, left: r.left - rr.left, right: rr.right - r.right, mid: (r.top + r.bottom) / 2 - rr.top, h: r.height, rowH: rr.height });
        walk(k, key, depth + 1);
      }
    };
    walk(row, '', 0);
  }
  for (const [key, list] of cols) {
    if (list.length < 3 || list.length < rows.length * 0.6) continue;
    const el0 = list[0].el;
    const s = getComputedStyle(el0);
    if (s.position === 'absolute' || s.display === 'inline') continue;
    const tol = cfg.thresholds.columnPx;
    const modes = { left: modeOf(list.map((x) => x.left), tol), right: modeOf(list.map((x) => x.right), tol) };
    for (const edge of ['left', 'right']) {
      const { mode, count, outliers } = modes[edge];
      if (count >= Math.max(3, list.length * 0.6) && outliers.length && outliers.length <= list.length * 0.34) {
        const bad = list.find((x) => Math.abs(x[edge] - mode) > tol);
        // Off on one edge but on the column's other edge: a left- or right-aligned
        // column of different widths, as it should be.
        const other = edge === 'left' ? 'right' : 'left';
        if (Math.abs(bad[other] - modes[other].mode) <= tol) continue;
        add('alignment', bad.el, `${edge} edge ${Math.abs(bad[edge] - mode).toFixed(1)}px off the column the other ${count} rows share`, { key, mode: rnd(mode), value: rnd(bad[edge]), rows: list.length }, Math.abs(bad[edge] - mode) > 3 ? 'medium' : 'low');
        break;
      }
    }
  }
}

function rowHeightCheck(group, add, k) {
  const byTop = new Map();
  for (const g of group) {
    const t = Math.round(rectOf(g).top / 2) * 2;
    if (!byTop.has(t)) byTop.set(t, []);
    byTop.get(t).push(g);
  }
  for (const row of byTop.values()) {
    if (row.length < 2) continue;
    const hs = row.map((g) => g.getBoundingClientRect().height);
    if (spread(hs) > 2) add('alignment', row[hs.indexOf(Math.min(...hs))], `${row.length} ${k} side by side with heights ${hs.map((h) => Math.round(h)).join('/')}`, { heights: hs.map(Math.round) }, 'low');
  }
}

// ---- interaction states -------------------------------------------------

uia.stateTargets = function stateTargets(cfg) {
  const ignore = cfg.selectors.ignore;
  const all = [...scopeRoot(cfg).querySelectorAll(cfg.selectors.interactive)].filter((el) => !(ignore && el.closest(ignore)) && visible(el) && !srOnly(el) && !el.matches(':disabled,[aria-disabled=true]'));
  const per = new Map();
  const picked = [];
  for (const el of all) {
    const k = sig(el) + '|' + sig(el.parentElement || el);
    const n = per.get(k) || 0;
    if (n >= cfg.thresholds.statesPerSignature) continue;
    per.set(k, n + 1);
    picked.push(el);
    if (picked.length >= cfg.thresholds.statesPerPage) break;
  }
  uia.targets = picked;
  return picked.map((el, i) => ({ i, sel: sel(el), sig: sig(el) }));
};

function paintOf(el) {
  const s = cs(el);
  const out = { el: boxPaint(s), before: null, after: null };
  for (const p of ['::before', '::after']) {
    const ps = cs(el, p);
    if (ps.content && ps.content !== 'none' && ps.display !== 'none') out[p === '::before' ? 'before' : 'after'] = { ...boxPaint(ps), pos: ps.position, inset: [ps.top, ps.right, ps.bottom, ps.left], w: ps.width, h: ps.height };
  }
  return out;
}

function boxPaint(s) {
  return { bg: s.backgroundColor, img: s.backgroundImage, shadow: s.boxShadow, outline: s.outlineStyle !== 'none' && px(s.outlineWidth) > 0 ? `${s.outlineWidth} ${s.outlineStyle} ${s.outlineColor} / ${s.outlineOffset}` : 'none', border: s.borderColor + ' ' + s.borderWidth, radii: radii(s), opacity: s.opacity, color: s.color };
}

/** The element and up to three ancestors: a focus style may live on a wrapper (:focus-within). */
function chainOf(el) {
  const out = [el];
  for (let cur = el.parentElement, i = 0; cur && cur !== document.body && i < 3; cur = cur.parentElement, i++) out.push(cur);
  return out;
}

uia.restPaint = (i) => chainOf(uia.targets[i]).map(paintOf);
uia.ancestor = (i, k) => chainOf(uia.targets[i])[k];

/** Compares a forced state against rest and returns findings. */
uia.stateCheck = function stateCheck(i, state, restChain, cfg) {
  const el = uia.targets[i];
  const chain = chainOf(el);
  const now = paintOf(el);
  const rest = restChain[0];
  const out = [];
  const add = (msg, data, severity = 'medium') => out.push({ check: 'shape', sel: sel(el), sig: sig(el), rect: rectOf(el), msg: `${state}: ${msg}`, data: { state, ...data }, severity });
  const changed = (a, b) => a && b && (a.bg !== b.bg || a.img !== b.img || a.shadow !== b.shadow || a.outline !== b.outline || a.border !== b.border);
  const layers = [];
  if (changed(now.el, rest.el)) layers.push({ name: 'element', paint: now.el, rect: rectOf(el) });
  for (const k of ['before', 'after']) {
    if (now[k] && (!rest[k] || changed(now[k], rest[k]) || now[k].opacity !== rest[k].opacity)) {
      const r = rectOf(el);
      const [t, ri, b, l] = now[k].inset.map((v) => (v === 'auto' ? null : px(v)));
      const rect = now[k].pos === 'absolute' && t !== null && b !== null && l !== null && ri !== null ? { left: r.left + l, top: r.top + t, right: r.right - ri, bottom: r.bottom - b } : r;
      layers.push({ name: k, paint: now[k], rect });
    }
  }
  if (!layers.length) {
    // Already focused at rest (autofocus): its focus style is what rest shows.
    if (state === 'focus-visible' && el === document.activeElement) return out;
    if (state === 'focus-visible') {
      const wrapper = chain.slice(1).some((a, k) => restChain[k + 1] && changed(paintOf(a).el, restChain[k + 1].el));
      if (!wrapper) add('no visible focus indicator', {}, 'high');
    }
    return out;
  }
  for (const layer of layers) {
    const p = layer.paint;
    const ring = p.outline !== 'none';
    const hasFill = (parseColor(p.bg) || { a: 0 }).a > 0.02 || (p.img && p.img !== 'none');
    if (!hasFill && !ring && (!p.shadow || p.shadow === 'none')) continue;
    // The rounded surface that holds this highlight.
    let surf = el.parentElement;
    while (surf && surf !== document.body) {
      const ss = cs(surf);
      const pp = ownPaint(surf, ss);
      if (radii(ss).some((r) => r > 1) && (pp.hasBg || pp.hasBorder || pp.hasShadow)) break;
      surf = surf.parentElement;
    }
    if (!surf || surf === document.body) continue;
    const ss = cs(surf);
    const sr = rectOf(surf);
    const box = { left: sr.left + px(ss.borderLeftWidth), top: sr.top + px(ss.borderTopWidth), right: sr.right - px(ss.borderRightWidth), bottom: sr.bottom - px(ss.borderBottomWidth) };
    const clips = ss.overflow !== 'visible' || ss.clipPath !== 'none' || ss.contain.includes('paint');
    let hl = layer.rect;
    // Scrolled out of its surface: nothing of it shows, its ring included.
    if (hl.bottom <= box.top || hl.top >= box.bottom || hl.right <= box.left || hl.left >= box.right) continue;
    if (ring) {
      const m = p.outline.match(/^([\d.]+)px .* \/ (-?[\d.]+)px$/);
      const grow = m ? +m[1] + +m[2] : 0;
      hl = { left: hl.left - grow, top: hl.top - grow, right: hl.right + grow, bottom: hl.bottom + grow };
      if (clips && (hl.left < box.left - 0.5 || hl.top < box.top - 0.5 || hl.right > box.right + 0.5 || hl.bottom > box.bottom + 0.5)) add(`focus ring clipped by ${sel(surf)}`, { surface: sel(surf) }, 'medium');
      continue;
    }
    const innerR = radii(ss).map((r) => Math.max(0, r - px(ss.borderTopWidth)));
    for (const prob of shapeProblems(hl, p.radii, box, innerR, { clips })) {
      const msg =
        prob.kind === 'square-in-round'
          ? `${layer.name} highlight has a ${prob.radius}px ${prob.corner} corner inside a ${prob.parentRadius}px rounded ${sel(surf).split(' > ').pop()} (insets ${prob.inset.join('/')}; needs ≈${prob.expected}px or a clip)`
          : prob.kind === 'radius-too-large'
            ? `${layer.name} highlight ${prob.corner} radius ${prob.radius}px is larger than the surface radius minus inset (${prob.expected}px)`
            : `${layer.name} highlight is ${prob.detail} of ${sel(surf).split(' > ').pop()} (${JSON.stringify(prob.inset)})`;
      add(msg, { ...prob, surface: sel(surf) }, prob.kind === 'square-in-round' ? 'high' : 'medium');
    }
  }
  return out;
};

// ---- phone touch targets ------------------------------------------------

uia.touchTargets = function touchTargets(cfg) {
  const out = [];
  const min = cfg.thresholds.touchPx;
  const ignore = cfg.selectors.ignore;
  const els = [...scopeRoot(cfg).querySelectorAll(cfg.selectors.interactive)].filter((el) => !(ignore && el.closest(ignore)) && visible(el) && !srOnly(el));
  const seen = new Set();
  const y0 = scrollY;
  for (const el of els) {
    const r0 = el.getBoundingClientRect();
    if (r0.width >= min - 0.5 && r0.height >= min - 0.5) continue;
    const k = sig(el) + '|' + sig(el.parentElement || el);
    if (seen.has(k)) continue;
    // A link inside running text is exempt (WCAG 2.5.8).
    if (el.tagName === 'A' && el.parentElement && ownText(el.parentElement).length > 20) continue;
    el.scrollIntoView({ block: 'center', inline: 'nearest' });
    const r = el.getBoundingClientRect();
    // Off screen even now (a skip link waiting for focus): not a target yet.
    if (r.bottom <= 0 || r.top >= innerHeight || r.right <= 0 || r.left >= innerWidth) continue;
    const cx = (r.left + r.right) / 2;
    const cy = (r.top + r.bottom) / 2;
    const half = min / 2 - 1;
    const pts = [];
    if (r.width < min) pts.push([cx - half, cy], [cx + half, cy]);
    if (r.height < min) pts.push([cx, cy - half], [cx, cy + half]);
    const label = el.closest('label');
    const hits = pts.map(([x, y]) => {
      const h = document.elementFromPoint(x, y);
      return !!h && (h === el || el.contains(h) || (label && label.contains(h)));
    });
    if (hits.every(Boolean)) continue;
    // Icon buttons side by side: 44 px tall hit areas and at least 28 px wide pass.
    if (r.width >= 28 && r.height < min && hits.slice(-2).every(Boolean) && pts.length === 4) continue;
    seen.add(k);
    out.push({ check: 'a11y', sel: sel(el), sig: sig(el), rect: rectOf(el), msg: `touch target ${Math.round(r.width)}×${Math.round(r.height)}px, under ${min}px with no larger hit area`, data: { w: Math.round(r.width), h: Math.round(r.height), hits }, severity: 'medium' });
  }
  scrollTo(0, y0);
  return out;
};

// ---- modifier clicks on in-app links ---------------------------------------

uia.modifierClicks = function modifierClicks(cfg) {
  const out = [];
  const links = [...document.querySelectorAll('a[href]')].filter((a) => visible(a) && !a.target && a.getAttribute('href') && !a.getAttribute('href').startsWith('http'));
  const seen = new Set();
  let prevented = false;
  const last = (e) => {
    prevented = e.defaultPrevented;
    e.preventDefault();
  };
  window.addEventListener('click', last);
  try {
    for (const a of links) {
      const k = sig(a);
      if (seen.has(k)) continue;
      seen.add(k);
      prevented = false;
      a.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true, metaKey: true, ctrlKey: true, button: 0 }));
      if (prevented) out.push({ check: 'a11y', sel: sel(a), sig: k, rect: rectOf(a), msg: 'Cmd/Ctrl-click on this link is swallowed: it cannot open a new tab', data: { href: a.getAttribute('href') }, severity: 'medium' });
    }
  } finally {
    window.removeEventListener('click', last);
  }
  return out;
};

// ---- layout shift across a transition ------------------------------------

/** Inside a fixed layer: it does not move with the page, so its document position follows the scroll. */
function inFixed(el) {
  for (let cur = el; cur && cur.nodeType === 1; cur = cur.parentElement) {
    const p = cs(cur).position;
    if (p === 'fixed' || p === 'sticky') return true;
  }
  return false;
}

/** How far the element's scrolling ancestors have scrolled it: a move by that much is scrolling, not a shift. */
function scrolledBy(el) {
  let x = 0;
  let y = 0;
  for (let cur = el.parentElement; cur && cur !== document.body && cur !== document.documentElement; cur = cur.parentElement) {
    if (cur.scrollTop || cur.scrollLeft) {
      y += cur.scrollTop;
      x += cur.scrollLeft;
    }
  }
  return { x, y };
}

function contentRect(el) {
  const r = rectOf(el);
  const s = scrolledBy(el);
  return { ...r, left: r.left + s.x, right: r.right + s.x, top: r.top + s.y, bottom: r.bottom + s.y };
}

uia.shiftStart = function shiftStart(cfg) {
  const entries = [];
  const po = new PerformanceObserver((list) => {
    for (const e of list.getEntries()) entries.push({ t: e.startTime, value: e.value, input: e.hadRecentInput, sources: (e.sources || []).map((s) => ({ sel: s.node && s.node.nodeType === 1 ? sel(s.node) : s.node ? sel(s.node.parentElement) : '?', from: s.previousRect && { top: s.previousRect.top, h: s.previousRect.height }, to: s.currentRect && { top: s.currentRect.top, h: s.currentRect.height } })) });
  });
  try {
    po.observe({ type: 'layout-shift', buffered: false });
  } catch {
    // WebKit has no layout-shift entries; box diffs still run.
  }
  const boxes = new Map();
  const ignore = cfg.selectors.ignore;
  for (const el of document.body.querySelectorAll('*')) {
    if (ignore && el.closest(ignore)) continue;
    if (!visible(el) || inFixed(el)) continue;
    boxes.set(el, { r: contentRect(el), text: el.textContent.length });
  }
  uia._shift = { entries, po, boxes };
};

uia.shiftStop = function shiftStop(cfg) {
  const st = uia._shift;
  if (!st) return null;
  st.po.disconnect();
  const moved = [];
  for (const [el, { r }] of st.boxes) {
    if (!el.isConnected || !visible(el)) continue;
    const n = contentRect(el);
    const dy = edgeShift(n.top - r.top, n.bottom - r.bottom);
    const dx = edgeShift(n.left - r.left, n.right - r.right);
    const dh = n.height - r.height;
    const dw = n.width - r.width;
    if (Math.abs(dy) > 1 || Math.abs(dx) > 1) moved.push({ el, dy, dx, dh, dw });
  }
  // Report only the outermost element of each group that moved together.
  const roots = moved.filter((m) => !moved.some((o) => o !== m && o.el.contains(m.el) && Math.abs(o.dy - m.dy) < 1 && Math.abs(o.dx - m.dx) < 1));
  const resized = [];
  for (const [el, { r }] of st.boxes) {
    if (!el.isConnected || !visible(el)) continue;
    const n = contentRect(el);
    if (Math.abs(n.height - r.height) > 1 || Math.abs(n.width - r.width) > 1) resized.push({ el, dh: n.height - r.height, dw: n.width - r.width, top: r.top });
  }
  const gone = [];
  for (const [el, { r }] of st.boxes) if (!el.isConnected || !visible(el)) gone.push({ el, r });
  const goneRoots = gone.filter((g) => !gone.some((o) => o !== g && o.el.contains(g.el)));
  const fresh = [];
  const ignore = cfg.selectors.ignore;
  for (const el of document.body.querySelectorAll('*')) {
    if (st.boxes.has(el) || (ignore && el.closest(ignore)) || !visible(el) || inFixed(el)) continue;
    if (el.parentElement && !st.boxes.has(el.parentElement) && el.parentElement !== document.body) continue;
    fresh.push({ el, r: contentRect(el) });
    if (fresh.length > 200) break;
  }
  const hOverlap = (a, b) => Math.min(a.right, b.right) - Math.max(a.left, b.left) > 0;
  const causeOf = (m) => {
    const was = st.boxes.get(m.el).r;
    const now = contentRect(m.el);
    const grew = resized.filter((x) => !x.el.contains(m.el) && !m.el.contains(x.el) && x.top <= was.top && Math.abs(x.dh) > 1);
    grew.sort((a, b) => b.top - a.top);
    if (grew[0]) return { kind: 'grew', text: `${sel(grew[0].el)} grew ${Math.round(grew[0].dh)}px` };
    const parentGrew = resized.find((x) => x.el.contains(m.el) && x.el !== m.el && Math.abs(x.dw) > 1 && Math.abs(m.dy) < 1);
    if (parentGrew) return { kind: 'grew', text: `${sel(parentGrew.el)} changed width ${Math.round(parentGrew.dw)}px` };
    const sameLine = (r) => r.top < now.bottom && r.bottom > now.top && r.right <= now.left + 1;
    const ins = fresh.filter((f) => !f.el.contains(m.el) && ((f.r.top <= now.top && hOverlap(f.r, now)) || (Math.abs(m.dx) > Math.abs(m.dy) && sameLine(f.r))));
    if (ins.length) return { kind: 'inserted', text: `${sel(ins[ins.length - 1].el)} was inserted` };
    const rem = goneRoots.filter((g) => g.r.top <= was.top && hOverlap(g.r, was));
    if (rem.length) return { kind: 'removed', text: `${sel(rem[rem.length - 1].el)} was removed` };
    return null;
  };
  uia._shift = null;
  return {
    cls: clsWindows(st.entries),
    entries: st.entries.slice(0, 40),
    moved: roots
      .sort((a, b) => Math.abs(b.dy) + Math.abs(b.dx) - Math.abs(a.dy) - Math.abs(a.dx))
      .slice(0, 12)
      .map((m) => {
        const c = causeOf(m);
        return { sel: sel(m.el), sig: sig(m.el), rect: rectOf(m.el), dy: rnd(m.dy), dx: rnd(m.dx), dh: rnd(m.dh), dw: rnd(m.dw), cause: c && c.text, causeKind: c && c.kind };
      }),
  };
};

// ---- per-frame recorder for flicker --------------------------------------

uia.framesStart = function framesStart(cfg) {
  const region = () => document.querySelector(cfg.selectors.region) || document.body;
  const frames = [];
  let added = 0;
  let removed = 0;
  const mo = new MutationObserver((recs) => {
    for (const r of recs) {
      added += r.addedNodes.length;
      removed += r.removedNodes.length;
    }
  });
  mo.observe(document.body, { subtree: true, childList: true });
  const skelSel = cfg.selectors.skeleton;
  const scroller = () => (cfg.selectors.scroller && document.querySelector(cfg.selectors.scroller)) || document.scrollingElement;
  const t0 = performance.now();
  let raf = 0;
  const snap = () => {
    const reg = region();
    const imgs = [...reg.querySelectorAll('img')].filter((i) => {
      if (i.complete && i.naturalWidth) return false;
      const r = i.getBoundingClientRect();
      if (!r.width || r.bottom < 0 || r.top > innerHeight) return false;
      const b1 = parseColor(cs(i).backgroundColor);
      const b2 = i.parentElement ? parseColor(cs(i.parentElement).backgroundColor) : null;
      return !(b1 && b1.a > 0) && !(b2 && b2.a > 0) && !(i.parentElement && cs(i.parentElement).backgroundImage !== 'none');
    });
    const ae = document.activeElement;
    frames.push({
      t: performance.now() - t0,
      text: reg.innerText.length,
      skel: skelSel ? [...reg.querySelectorAll(skelSel)].filter(visible).length : 0,
      h: Math.round(reg.getBoundingClientRect().height),
      focus: ae && ae !== document.body ? sel(ae) : 'body',
      scroll: Math.round(scroller().scrollTop),
      theme: document.documentElement.getAttribute('data-theme') || '',
      bg: pageLum(),
      bareImgs: imgs.length,
      added,
      removed,
    });
    added = 0;
    removed = 0;
    raf = requestAnimationFrame(snap);
  };
  snap();
  uia._frames = { frames, mo, stop: () => cancelAnimationFrame(raf) };
};

uia.framesStop = function framesStop() {
  const st = uia._frames;
  if (!st) return [];
  st.stop();
  st.mo.disconnect();
  uia._frames = null;
  return st.frames;
};

/** Elements whose numbers changed during a live window: they should not change width. */
function numberLike(t) {
  const digits = (t.match(/\d/g) || []).length;
  return digits > 0 && t.length <= 40 && digits / t.replace(/\s/g, '').length >= 0.25;
}

uia.tickStart = function tickStart(cfg) {
  const watch = new Map();
  for (const el of document.querySelectorAll('*')) {
    const t = ownText(el);
    if (t && numberLike(t) && visible(el)) watch.set(el, { t, w: el.getBoundingClientRect().width });
  }
  uia._tick = watch;
};

uia.tickStop = function tickStop() {
  const out = [];
  for (const [el, v] of uia._tick || []) {
    if (!el.isConnected || !visible(el)) continue;
    const t = ownText(el);
    if (t === v.t || !numberLike(t)) continue;
    const s = cs(el);
    const w = el.getBoundingClientRect().width;
    const tab = (s.fontVariantNumeric || '').includes('tabular-nums');
    if (!tab) out.push({ check: 'alignment', sel: sel(el), sig: sig(el), rect: rectOf(el), msg: `live number without tabular-nums ("${v.t}" → "${t}")`, data: { from: v.t, to: t }, severity: 'low' });
    if (Math.abs(w - v.w) > 1 && t.length === v.t.length) out.push({ check: 'layout-shift', sel: sel(el), sig: sig(el), rect: rectOf(el), msg: `live number changes width ${Math.round(v.w)} → ${Math.round(w)}px at the same length`, data: { from: v.t, to: t }, severity: 'medium' });
  }
  uia._tick = null;
  return out;
};

// ---- first paint: theme and loaders from navigation start -------------------

/** Started by the init script at document start; records frames for `ms`. */
uia.bootRecord = function bootRecord(cfg, ms = 2500) {
  const frames = [];
  const t0 = performance.now();
  const step = () => {
    const html = document.documentElement;
    const body = document.body;
    frames.push({
      t: performance.now() - t0,
      theme: html ? html.getAttribute('data-theme') || '' : '',
      bg: html ? pageLum() : 0,
      text: body ? body.innerText.length : 0,
      skel: body && cfg.skeleton ? body.querySelectorAll(cfg.skeleton).length : 0,
      h: 0,
      focus: 'body',
      scroll: 0,
      added: 0,
      removed: 0,
    });
    if (performance.now() - t0 < ms) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
  uia._boot = frames;
};

/** Layout shifts buffered since navigation (Chromium only). */
uia.loadShifts = function loadShifts() {
  return new Promise((resolve) => {
    const out = [];
    try {
      const po = new PerformanceObserver((list) => {
        for (const e of list.getEntries()) out.push({ t: e.startTime, value: e.value, sources: (e.sources || []).map((s) => (s.node && s.node.nodeType === 1 ? sel(s.node) : s.node ? sel(s.node.parentElement) : '?')) });
      });
      po.observe({ type: 'layout-shift', buffered: true });
      setTimeout(() => {
        po.disconnect();
        resolve(out);
      }, 50);
    } catch {
      resolve(null);
    }
  });
};
