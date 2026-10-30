// The project config contract, with the defaults every project shares. A
// project config (see ../console/audit.config.mjs) names its server,
// routes and states, how to reach each, the variants and its selectors; the
// core knows nothing else about it.

export const DEFAULTS = {
  variants: {
    widths: [1440, 1280, 768, 375, 320],
    themes: ['light', 'dark'],
    // Interaction states, touch targets and modifier clicks are sampled on
    // these widths only: the matrix would explode otherwise.
    stateWidths: [1280, 375],
    phoneBelow: 640,
    heights: { phone: 812, tablet: 1024, desktop: 900 },
  },
  selectors: {
    interactive: 'a[href], button, [role=button], [role=tab], [role=switch], [role=menuitem], [role=option], [role=radio], input:not([type=hidden]), select, textarea, summary, [tabindex="0"]',
    container: 'section, article, aside, dialog, [role=dialog], .card',
    row: 'li, tr, [role=row]',
    icon: 'svg, .icon',
    badge: '.badge',
    nav: 'nav',
    region: 'main',
    scroller: null,
    skeleton: '[aria-busy=true]',
    ignore: null,
  },
  components: {},
  rhythm: [],
  allow: { noise: null, prose: null, duplicate: null, contrast: null },
  thresholds: {
    emptySpace: { minPx: 48, ratio: 0.25 },
    iconCentrePx: 1.5,
    columnPx: 1.5,
    touchPx: 44,
    statesPerSignature: 2,
    statesPerPage: 60,
    skeletonMinMs: 300,
    rhythmTolPx: 1,
  },
  css: { files: [], tokens: [], patterns: { space: /^--s-/, radius: /^--r-/, font: /^--fs-/ }, allowValues: [0, 1] },
  scenarios: [],
};

function merge(a, b) {
  if (Array.isArray(b) || typeof b !== 'object' || b === null || b instanceof RegExp) return b === undefined ? a : b;
  const out = { ...a };
  for (const [k, v] of Object.entries(b)) out[k] = k in (a || {}) && typeof a[k] === 'object' && a[k] !== null && !Array.isArray(a[k]) && !(a[k] instanceof RegExp) ? merge(a[k], v) : v;
  return out;
}

export function withDefaults(cfg) {
  const out = merge(DEFAULTS, cfg);
  if (typeof out.start !== 'function') throw new Error('config.start() must start the app and return {baseUrl, control, stop}');
  if (!Array.isArray(out.scenarios) || !out.scenarios.length) throw new Error('config.scenarios is empty');
  const ids = new Set();
  for (const s of out.scenarios) {
    if (!s.id || ids.has(s.id)) throw new Error(`scenario id missing or repeated: ${s.id}`);
    ids.add(s.id);
  }
  return out;
}

/** The part of the config the page sees (plain data, no functions). */
export function pageConfig(cfg, scale) {
  return { selectors: cfg.selectors, components: cfg.components, rhythm: cfg.rhythm, allow: cfg.allow, thresholds: cfg.thresholds, scale };
}
