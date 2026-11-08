// The demo catalog's eight album covers as SVG symbols (DESIGN.md section 6).
// Each album has three symbols from one list of layers: the full cover,
// cv-<key>, for 160 design px and larger; a mid one, cv-<key>-m, for 96 to
// 159, which keeps the picture and its texture but drops the sleeve type; and
// a small one, cv-<key>-s, under 96, which also leaves out every detail layer
// and may give a layer a simpler form. The full and mid symbols share the
// picture, a group cv-<key>-art. The page carries them once, in a hidden
// sprite, and draws a cover with <use href="#cv-<key>">. Only presentation attributes: the page's
// Content-Security-Policy allows no style attribute.

// ---- the catalog -----------------------------------------------------------

export const ARTISTS = {
  910001: 'Mira Solenne',
  910002: 'Nightjar Coast',
  910003: 'The Velvet Orchard',
  910004: '青柠电台',
};

// internal/demo/catalog.go#albums, in its order (ids 920001 to 920008).
export const ALBUMS = [
  { key: 'glass', id: 920001, title: 'Glass Weather', artist: 910001, genre: 'Pop', year: 2025, date: '2025-06-14', bg: '#1f3b5c', fg: '#f2c14e',
    tracks: [['Glass Weather', '3:52'], ['Paper Satellites', '4:14'], ['Slow Lightning', '4:36'], ['Northbound', '4:59'], ['Halo Static', '3:21'], ['Small Hours', '3:44'], ['Weathervane', '4:06'], ['Blue Hour Again', '4:28'], ['Signal Fires', '4:51'], ['Undertow', '3:48'], ['Night Market', '3:26'], ['Afterglow (Reprise)', '4:16']] },
  { key: 'tidal', id: 920002, title: 'Tidal Rooms', artist: 910001, genre: 'Pop', year: 2022, date: '2022-04-14', bg: '#0f5257', fg: '#a8e6cf',
    tracks: [['Tidal Rooms', '4:31'], ['Seaglass', '4:08'], ['Low Ceilings', '3:46'], ['Hollow Bells', '4:23'], ['Lighthouse Keeper', '4:00'], ['Drift', '3:38'], ['Open Water', '3:15'], ['Moon Tide', '3:54'], ['Coastline', '3:32'], ['Harbor Song', '3:06']] },
  { key: 'tide', id: 920003, title: 'Low Tide Signals', artist: 910002, genre: 'Alternative', year: 2026, date: '2026-05-14', bg: '#2b2d42', fg: '#ef8354',
    tracks: [['Low Tide Signals', '3:59'], ['Radio Silence', '4:52'], ['Static Bloom', '3:15'], ['Dune Lines', '3:07'], ['Faultline', '3:29'], ['Kestrel', '4:22'], ['Cold Open', '4:44'], ['Saltwater Wires', '4:36'], ['Night Ferry', '4:58'], ['Long Exposure', '3:24'], ['Breakwater', '3:02']] },
  { key: 'salt', id: 920004, title: 'Salt & Static', artist: 910002, genre: 'Alternative', year: 2023, date: '2023-03-14', bg: '#5c2a3e', fg: '#f4a259',
    tracks: ['Salt & Static', 'Harbor Lights', 'Sodium Glow', 'Wreckage', 'Steady Hands', 'Gull Cry', 'Tin Roof', 'Ember Coast', 'Last Ferry'].map((t) => [t]) },
  { key: 'orchard', id: 920005, title: 'Orchard Lights', artist: 910003, genre: 'Jazz', year: 2024, date: '2024-11-14', bg: '#3d2c1e', fg: '#e9c46a',
    tracks: ['Orchard Lights', 'Velvet Hour', 'Brass Moon', 'Late Bloom', 'Copper Rain', 'Quiet Waltz', 'Amber Room', 'Last Call at Nine'].map((t) => [t]) },
  { key: 'bloom', id: 920006, title: 'Late Bloom', artist: 910003, genre: 'Jazz', year: 2026, date: '2026-08-14', bg: '#264653', fg: '#f4d35e',
    tracks: ['Late Bloom', 'Garden Steps', 'Under Lanterns', 'Fig Tree', 'Evening Rounds'].map((t) => [t]) },
  { key: 'summer', id: 920007, title: '夏夜信号', artist: 910004, genre: 'Mandopop', year: 2025, date: '2025-04-14', bg: '#14213d', fg: '#fca311',
    tracks: ['夏夜信号', '便利店灯光', '天台', '末班车', '汽水', '晚风收音机', '霓虹', '海报', '慢慢走', '再见夏天'].map((t) => [t]) },
  { key: 'shore', id: 920008, title: '海边的耳机', artist: 910004, genre: 'Mandopop', year: 2023, date: '2023-03-14', bg: '#1b4332', fg: '#95d5b2',
    tracks: ['海边的耳机', '潮汐表', '白色衬衫', '灯塔', '冰柠檬', '防波堤', '落日', '空房间', '回程'].map((t) => [t]) },
];

export const album = (key) => ALBUMS.find((a) => a.key === key);

// catalog.go#playlists: name, curator, the album whose cover it borrows.
export const PLAYLISTS = [
  ['Glass and Neon', 'AMDL Editors', 920001],
  ['Coastal Nights', 'Nightjar Coast', 920003],
  ['Orchard Sessions', 'The Velvet Orchard', 920005],
  ['夏天的电台', '青柠电台', 920007],
  ['Signals After Dark', 'AMDL Editors', 920002],
  ['Slow Lightning Radio', 'Mira Solenne', 920004],
];

// An album's link: the title lowercased, spaces to dashes, other characters dropped.
export const albumUrl = (a) => `https://music.apple.com/us/album/${a.title.toLowerCase().replace(/ /g, '-').replace(/[^a-z0-9-]/g, '')}/${a.id}`;

// ---- drawing helpers -------------------------------------------------------

// Seeded randomness, so every build draws the same rain, stars and windows.
export function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const r1 = (n) => Math.round(n * 10) / 10;
const attrs = (a) =>
  Object.entries(a)
    .filter(([, v]) => v !== undefined && v !== null && v !== false)
    .map(([k, v]) => ` ${k}="${typeof v === 'number' ? r1(v) : v}"`)
    .join('');
const el = (tag, a = {}, inner) => (inner === undefined ? `<${tag}${attrs(a)}/>` : `<${tag}${attrs(a)}>${inner}</${tag}>`);
const g = (a, ...kids) => el('g', a, kids.join(''));
const rect = (x, y, width, height, a = {}) => el('rect', { x, y, width, height, ...a });
const circle = (cx, cy, r, a = {}) => el('circle', { cx, cy, r, ...a });
const line = (x1, y1, x2, y2, a = {}) => el('line', { x1, y1, x2, y2, ...a });
const path = (d, a = {}) => el('path', { d, ...a });
const ellipse = (cx, cy, rx, ry, a = {}) => el('ellipse', { cx, cy, rx, ry, ...a });
const text = (x, y, s, a = {}) => el('text', { x, y, ...a }, s.replace(/&/g, '&amp;'));
const stops = (list) => list.map(([o, c, op = 1]) => el('stop', { offset: o, 'stop-color': c, 'stop-opacity': op === 1 ? undefined : op })).join('');
const linear = (id, list, a = {}) => el('linearGradient', { id, x1: 0, y1: 0, x2: 0, y2: 1, ...a }, stops(list));
const radial = (id, list, a = {}) => el('radialGradient', { id, ...a }, stops(list));
const clip = (id, inner) => el('clipPath', { id }, inner);
// An arc of a circle from angle a0 to a1 (degrees; 0 is east, 90 south).
const arc = (cx, cy, r, a0, a1) => {
  const p = (a) => [r1(cx + r * Math.cos((a * Math.PI) / 180)), r1(cy + r * Math.sin((a * Math.PI) / 180))];
  const [x0, y0] = p(a0);
  const [x1, y1] = p(a1);
  return `M${x0} ${y0}A${r} ${r} 0 ${Math.abs(a1 - a0) > 180 ? 1 : 0} 1 ${x1} ${y1}`;
};
// A four-point sparkle: two crossing lines.
const sparkle = (x, y, len, a) => path(`M${r1(x - len / 2)} ${r1(y)}H${r1(x + len / 2)}M${r1(x)} ${r1(y - len / 2)}V${r1(y + len / 2)}`, { 'stroke-width': 1, 'stroke-linecap': 'round', ...a });

const SANS = "Geist, 'Helvetica Neue', Helvetica, Arial, sans-serif";
const MONO = "'Geist Mono', 'SF Mono', Menlo, Consolas, monospace";
const SERIF = "'Songti SC', 'STSong', 'Noto Serif CJK SC', 'Source Han Serif SC', serif";
// The Velvet Orchard's title face; a textLength squeezes it on systems
// without a narrow face, so it reads condensed everywhere.
const NARROW = "'Arial Narrow', 'Roboto Condensed', 'Liberation Sans Narrow', 'Helvetica Neue', Arial, sans-serif";
// Nightjar Coast's rubber stamp.
const STAMP = "'Courier New', 'Courier Prime', 'Liberation Mono', monospace";

const grain = (dark, light) => [rect(0, 0, 300, 300, { filter: 'url(#cv-grain)', opacity: dark }), rect(0, 0, 300, 300, { filter: 'url(#cv-grain-light)', opacity: light })].join('');

// A layer: { full, small } where small is undefined (same as full), null (left
// out: a detail layer) or its own markup. A type layer (the sleeve's words and
// catalog mark) is also left out of the mid symbol; type layers come last.
const L = (full, small) => ({ full, small });
const D = (full) => ({ full, small: null });
const T = (full) => ({ full, small: null, type: true });

// Shared defs, used by every cover.
const SHARED = [
  el('filter', { id: 'cv-grain', x: 0, y: 0, width: '100%', height: '100%', 'color-interpolation-filters': 'sRGB' },
    el('feTurbulence', { type: 'fractalNoise', baseFrequency: '0.85', numOctaves: 2, seed: 3, stitchTiles: 'stitch' }) +
    el('feColorMatrix', { type: 'matrix', values: '0 0 0 0 0  0 0 0 0 0  0 0 0 0 0  0 0 0 -1.6 1.05' })),
  el('filter', { id: 'cv-grain-light', x: 0, y: 0, width: '100%', height: '100%', 'color-interpolation-filters': 'sRGB' },
    el('feTurbulence', { type: 'fractalNoise', baseFrequency: '0.85', numOctaves: 2, seed: 11, stitchTiles: 'stitch' }) +
    el('feColorMatrix', { type: 'matrix', values: '0 0 0 0 1  0 0 0 0 1  0 0 0 0 1  0 0 0 1.6 -0.62' })),
  el('filter', { id: 'cv-static', x: 0, y: 0, width: '100%', height: '100%', 'color-interpolation-filters': 'sRGB' },
    el('feTurbulence', { type: 'turbulence', baseFrequency: '0.95 0.06', numOctaves: 1, seed: 5 }) +
    el('feColorMatrix', { type: 'saturate', values: 0 })),
  el('filter', { id: 'cv-ripple', x: '-5%', y: '-5%', width: '110%', height: '110%' },
    el('feTurbulence', { type: 'fractalNoise', baseFrequency: '0.02 0.18', numOctaves: 2, seed: 9 }) +
    el('feDisplacementMap', { in: 'SourceGraphic', scale: 7, xChannelSelector: 'R', yChannelSelector: 'G' })),
  el('filter', { id: 'cv-soft4', x: '-50%', y: '-50%', width: '200%', height: '200%' }, el('feGaussianBlur', { stdDeviation: 4 })),
  linear('cv-fade-up', [[0, '#ffffff'], [1, '#000000']], { x1: 0, y1: 1, x2: 0, y2: 0 }),
].join('');

// Mira Solenne's sleeve type: small wide caps top left, a short rule, a catalog
// mark bottom right.
const miraType = (title, ink, rule, mark, [x, y] = [20, 30], withRule = true) =>
  T(g({},
    text(x, y, 'MIRA SOLENNE', { 'font-family': SANS, 'font-size': 11, 'font-weight': 600, 'letter-spacing': 2.6, fill: ink, 'fill-opacity': 0.92 }),
    text(x, y + 15, title, { 'font-family': SANS, 'font-size': 11, 'font-weight': 400, 'letter-spacing': 2.6, fill: ink, 'fill-opacity': 0.62 }),
    withRule ? line(x, y + 23, x + 42, y + 23, { stroke: rule, 'stroke-width': 1, opacity: 0.7 }) : '',
    text(282, 286, mark, { 'text-anchor': 'end', 'font-family': MONO, 'font-size': 8, 'letter-spacing': 1, fill: ink, 'fill-opacity': 0.55 })));

// Nightjar Coast's rubber stamp: the band name in a box, misregistered.
const stamp = (x, y, angle, ink, pale) =>
  g({ transform: `rotate(${angle} ${x + 64} ${y + 15})` },
    rect(x, y, 128, 30, { fill: 'none', stroke: ink, 'stroke-width': 1.5 }),
    text(x + 9.2, y + 20.8, 'NIGHTJAR COAST', { 'font-family': STAMP, 'font-size': 10.5, 'font-weight': 700, 'letter-spacing': 2, textLength: 112, lengthAdjust: 'spacing', fill: pale, 'fill-opacity': 0.35 }),
    text(x + 8, y + 20, 'NIGHTJAR COAST', { 'font-family': STAMP, 'font-size': 10.5, 'font-weight': 700, 'letter-spacing': 2, textLength: 112, lengthAdjust: 'spacing', fill: ink }));

// The Velvet Orchard's label band: name and title in dark condensed caps. The
// title is set large and squeezed to `width` (about 82% of a regular face).
const orchardBand = (title, width, mark, ink) =>
  T(g({},
    text(20, 25, 'THE VELVET ORCHARD', { 'font-family': SANS, 'font-size': 10, 'font-weight': 700, 'letter-spacing': 2.2, fill: ink }),
    text(20, 54, title, { 'font-family': NARROW, 'font-size': 28, 'font-weight': 700, textLength: width, lengthAdjust: 'spacingAndGlyphs', fill: ink }),
    text(280, 25, mark, { 'text-anchor': 'end', 'font-family': MONO, 'font-size': 9, fill: ink }),
    line(0, 64, 300, 64, { stroke: ink, 'stroke-width': 1.5 })));

// ---- the eight covers ------------------------------------------------------

const COVERS = {
  glass() {
    const rnd = mulberry32(920001);
    const span = (a, b) => a + (b - a) * rnd();
    const hill1 = 'M-10 237L0 236C60 226 110 241 170 232S260 222 300 233L310 234V310H-10Z';
    const hill2 = 'M-10 263L0 262C70 250 130 268 200 258S270 250 300 256L310 257V310H-10Z';
    const defs = [
      linear('cv-glass-sky', [[0, '#112032'], [0.55, '#1f3b5c'], [1, '#2a4a6e']]),
      linear('cv-glass-sheen', [[0, '#ffffff', 0.07], [0.5, '#ffffff', 0], [1, '#ffffff', 0.04]], { x1: 0, y1: 0, x2: 1, y2: 1 }),
      radial('cv-glass-glow', [[0, '#f2c14e', 0.38], [1, '#f2c14e', 0]]),
      radial('cv-glass-sun', [[0, '#f9e3af'], [0.55, '#f2c14e'], [1, '#e0a63a']], { fx: 0.356, fy: 0.356 }),
      g({ id: 'cv-glass-scene' }, circle(129, 207, 66, { fill: 'url(#cv-glass-sun)' }), path(hill1, { fill: '#112032', 'fill-opacity': 0.92 }), path(hill2, { fill: '#0c1828' })),
      el('filter', { id: 'cv-glass-blur', x: '-5%', y: '-5%', width: '110%', height: '110%' }, el('feGaussianBlur', { stdDeviation: 1.2 })),
      clip('cv-glass-pane1', rect(0, 0, 100, 300)),
      clip('cv-glass-pane2', rect(100, 0, 100, 300)),
      clip('cv-glass-pane3', rect(200, 0, 100, 300)),
    ].join('');
    // The panes clip in the page's space, so the shift goes on the use inside.
    // The scene behind the glass is soft, so the drops sit in a sharper plane.
    const pane = (n, t) => g({ 'clip-path': `url(#cv-glass-pane${n})` }, el('use', { href: '#cv-glass-scene', transform: t, filter: 'url(#cv-glass-blur)' }));
    // A drop on the glass: a lens of the sky, a dark rim on top, the sun's
    // color caught along its lower edge and a small glint.
    const drop = (x, y, rx) => {
      const ry = rx * 1.15;
      const at = (deg, k = 1) => [r1(x + rx * k * Math.cos((deg * Math.PI) / 180)), r1(y + ry * k * Math.sin((deg * Math.PI) / 180))];
      const [ax, ay] = at(35, 0.86);
      const [bx, by] = at(145, 0.86);
      return g({},
        ellipse(x, y, rx, ry, { fill: '#3a5370', 'fill-opacity': 0.55 }),
        path(`M${r1(x - rx)} ${r1(y)}A${r1(rx)} ${r1(ry)} 0 0 1 ${r1(x + rx)} ${r1(y)}`, { fill: 'none', stroke: '#112032', 'stroke-opacity': 0.6, 'stroke-width': 1 }),
        path(`M${ax} ${ay}A${r1(rx * 0.86)} ${r1(ry * 0.86)} 0 0 1 ${bx} ${by}`, { fill: 'none', stroke: '#f9e3af', 'stroke-opacity': 0.55, 'stroke-width': 1, 'stroke-linecap': 'round' }),
        circle(x - rx * 0.35, y - ry * 0.4, Math.max(0.6, rx * 0.18), { fill: '#ffffff', 'fill-opacity': 0.7 }));
    };
    // A trail above a drop: a thin wet streak that tapers upward and wobbles,
    // ending at the drop.
    const trail = (x, y, rx, len, w, wob) => {
      const yb = y - rx * 0.6;
      const yt = yb - len;
      return path(
        `M${r1(x - w / 2)} ${r1(yb)}C${r1(x - w / 2 + wob * 0.4)} ${r1(yb - len * 0.35)} ${r1(x + wob - w * 0.2)} ${r1(yb - len * 0.7)} ${r1(x + wob)} ${r1(yt)}C${r1(x + wob + w * 0.2)} ${r1(yb - len * 0.7)} ${r1(x + w / 2 + wob * 0.4)} ${r1(yb - len * 0.35)} ${r1(x + w / 2)} ${r1(yb)}Z`,
        { fill: '#ffffff', 'fill-opacity': r1(span(0.12, 0.2) * 100) / 100 },
      );
    };
    const rain = [];
    for (let i = 0; i < 12; i++) {
      const x = span(14, 286);
      const y = span(70, 280);
      const rx = span(2.5, 6);
      const streak = i % 3 !== 2 ? trail(x, y, rx, span(18, 64), Math.min(span(2, 4), rx * 1.2), span(-2.5, 2.5)) : '';
      rain.push(streak + drop(x, y, rx));
    }
    const mist = [];
    for (let i = 0; i < 70; i++) mist.push(circle(span(0, 300), 300 * rnd() ** 2, span(0.5, 1.1), { fill: '#ffffff', 'fill-opacity': span(0.12, 0.25) }));
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 300, { fill: 'url(#cv-glass-sky)' })),
        L(circle(129, 207, 150, { fill: 'url(#cv-glass-glow)' })),
        L(pane(1) + pane(2, 'translate(2 -1.5)') + pane(3, 'translate(-1 1)'), el('use', { href: '#cv-glass-scene' })),
        D(rect(100, 0, 100, 300, { fill: 'url(#cv-glass-sheen)' }) + rect(200, 0, 100, 300, { fill: '#000000', 'fill-opacity': 0.06 })),
        D(line(100, 0, 100, 300, { stroke: '#ffffff', 'stroke-opacity': 0.14, 'stroke-width': 1 }) + line(200, 0, 200, 300, { stroke: '#ffffff', 'stroke-opacity': 0.14, 'stroke-width': 1 })),
        D(g({}, ...mist)),
        D(g({}, ...rain)),
        L(trail(62, 92, 7, 46, 4, 2) + drop(62, 92, 7) + drop(226, 140, 5) + trail(170, 58, 6, 40, 3.5, -2) + drop(170, 58, 6),
          drop(62, 92, 7) + drop(226, 140, 5) + drop(170, 58, 6)),
        D(grain(0.18, 0.08)),
        miraType('GLASS WEATHER', '#f9e3af', '#f2c14e', 'MS·25'),
      ],
    };
  },

  tidal() {
    const rnd = mulberry32(920002);
    const frames = [
      [34, 24, 232, 290, 3, 1],
      [74, 60, 172, 230, 2.4, 0.8],
      [106, 90, 126, 180, 1.8, 0.62],
      [131, 113, 92, 140, 1.4, 0.48],
      [150, 131, 66, 108, 1.1, 0.36],
    ];
    const frame = ([x, y, w, h, sw, so]) => rect(x, y, w, h, { fill: '#082d2f', 'fill-opacity': 0.16, stroke: '#a8e6cf', 'stroke-width': sw, 'stroke-opacity': so });
    const floor = [];
    for (let i = 0; i < frames.length - 1; i++) {
      const [x, y, w, h] = frames[i];
      const [x2, y2, w2, h2] = frames[i + 1];
      floor.push(line(x, y + h, x2, y2 + h2), line(x + w, y + h, x2 + w2, y2 + h2));
    }
    const ripples = [206, 214, 223, 233, 246, 262, 280].map((y, i) => {
      const dash = ['14 9', '18 10', '22 12', '26 14', '30 16', '36 18', '44 22'][i];
      const off = r1(rnd() * 40);
      return line(-off, y, 300, y, { stroke: '#a8e6cf', 'stroke-opacity': r1(0.38 - (0.2 * i) / 6) || 0.18, 'stroke-width': r1(0.8 + (0.6 * i) / 6), 'stroke-dasharray': dash });
    });
    const defs = [
      linear('cv-tidal-ground', [[0, '#0f5257'], [1, '#082d2f']]),
      linear('cv-tidal-water', [[0, '#0f5257', 0.55], [1, '#082d2f', 0.95]]),
      radial('cv-tidal-window', [[0, '#ffffff'], [0.5, '#d8f4e9'], [1, '#a8e6cf']]),
      radial('cv-tidal-glow', [[0, '#a8e6cf', 0.28], [1, '#a8e6cf', 0]]),
      g({ id: 'cv-tidal-rooms' }, ...frames.map(frame), g({ stroke: '#a8e6cf', 'stroke-opacity': 0.2, 'stroke-width': 0.8 }, ...floor)),
      g({ id: 'cv-tidal-rooms-s' }, frame(frames[0]), frame(frames[2]), frame(frames[4])),
      clip('cv-tidal-below', rect(0, 196, 300, 104)),
      clip('cv-tidal-dash', rect(166, 220, 10, 20) + rect(178, 220, 10, 20) + rect(190, 220, 10, 20)),
    ].join('');
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 300, { fill: 'url(#cv-tidal-ground)' })),
        L(el('use', { href: '#cv-tidal-rooms' }), el('use', { href: '#cv-tidal-rooms-s' })),
        L(circle(183, 163, 60, { fill: 'url(#cv-tidal-glow)' }) + circle(183, 163, 19, { fill: 'url(#cv-tidal-window)' }), circle(183, 163, 19, { fill: 'url(#cv-tidal-window)' })),
        L(rect(0, 196, 300, 104, { fill: 'url(#cv-tidal-water)' })),
        D(g({ 'clip-path': 'url(#cv-tidal-below)' },
          el('use', { href: '#cv-tidal-rooms', transform: 'translate(0 392) scale(1 -1)', opacity: 0.3, filter: 'url(#cv-ripple)' }),
          ellipse(183, 229, 17, 4.5, { fill: '#d8f4e9', 'fill-opacity': 0.5, 'clip-path': 'url(#cv-tidal-dash)' }))),
        L(line(0, 196, 300, 196, { stroke: '#d8f4e9', 'stroke-opacity': 0.6, 'stroke-width': 1.2 })),
        L(ripples.join(''), ripples[0] + ripples[2]),
        D(grain(0.16, 0.06)),
        miraType('TIDAL ROOMS', '#d8f4e9', '#a8e6cf', 'MS·22', [46, 39], false),
      ],
    };
  },

  tide() {
    const rnd = mulberry32(920003);
    const span = (a, b) => a + (b - a) * rnd();
    const P = '#f8c7b2';
    const O = '#ef8354';
    const legX = (y, side) => 104 + side * 16 * ((y - 58) / 156);
    const bracing = [];
    for (let y = 206; y - 16 >= 74 - 4; y -= 16) {
      bracing.push(line(legX(y, -1), y, legX(y - 16, 1), y - 16), line(legX(y, 1), y, legX(y - 16, -1), y - 16));
    }
    const bars = [[220, 30, 0.85], [228, 26, 0.72], [237, 22, 0.6], [247, 17, 0.5], [258, 12, 0.42], [270, 8, 0.35], [283, 5, 0.3]].map(([y, w, o]) => rect(232 - w / 2, y, w, 2.4, { rx: 1.2, fill: O, 'fill-opacity': o }));
    const arcs = (radii, widths, ops, dashed, wide) =>
      radii.map((r, i) => {
        const a = { fill: 'none', stroke: O, 'stroke-width': wide ?? widths[i], 'stroke-opacity': ops[i], 'stroke-linecap': 'round', 'stroke-dasharray': dashed(i) ? '3 4' : undefined };
        return path(`${arc(104, 56, r, -50, 50)}${arc(104, 56, r, 130, 230)}`, a);
      }).join('');
    const legs = (w) => line(88, 214, 104, 58, { stroke: P, 'stroke-width': w }) + line(120, 214, 104, 58, { stroke: P, 'stroke-width': w });
    const hut = rect(80, 202, 48, 12, { fill: '#171824', stroke: P, 'stroke-width': 1 });
    const speck = [];
    for (let i = 0; i < 40; i++) {
      const x = span(60, 230);
      const y = span(20, 120);
      speck.push(ellipse(x, y, span(0.6, 1.4), span(0.5, 1.1), { fill: '#2b2d42', transform: `rotate(${Math.round(span(0, 180))} ${r1(x)} ${r1(y)})` }));
    }
    const defs = [
      el('pattern', { id: 'cv-tide-dots', patternUnits: 'userSpaceOnUse', width: 5, height: 5, patternTransform: 'rotate(22)' }, circle(2.5, 2.5, 1.15, { fill: O })),
      el('mask', { id: 'cv-tide-fade', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 40, 300, 174, { fill: 'url(#cv-fade-up)' })),
      radial('cv-tide-glow', [[0, O, 0.35], [1, O, 0]]),
      radial('cv-tide-flat', [[0, O, 0.3], [1, O, 0]]),
      clip('cv-tide-sky', rect(0, 0, 300, 214)),
    ].join('');
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 300, { fill: '#2b2d42' })),
        L(rect(0, 0, 300, 214, { fill: 'url(#cv-tide-dots)', mask: 'url(#cv-tide-fade)', opacity: 0.55 }), ellipse(104, 214, 120, 70, { fill: 'url(#cv-tide-flat)', 'clip-path': 'url(#cv-tide-sky)' })),
        L(line(0, 214, 300, 214, { stroke: P, 'stroke-opacity': 0.7, 'stroke-width': 1 })),
        L(circle(232, 78, 46, { fill: 'url(#cv-tide-glow)' }) + circle(232, 78, 22, { fill: O }) + circle(234, 76.6, 22, { fill: 'none', stroke: P, 'stroke-width': 1.2, 'stroke-opacity': 0.45 }),
          circle(232, 78, 46, { fill: 'url(#cv-tide-glow)' }) + circle(232, 78, 22, { fill: O })),
        L(bars.join(''), bars.slice(0, 4).join('')),
        D(g({ fill: 'none', stroke: P, 'stroke-opacity': 0.25, 'stroke-width': 1 },
          path('M0 230C80 224 150 238 300 228'), path('M0 248C90 242 180 258 300 246'), path('M0 268C70 263 200 277 300 265'), path('M0 289C100 283 190 297 300 287'))),
        D(line(104, 80, 30, 214, { stroke: P, 'stroke-opacity': 0.35, 'stroke-width': 0.8 }) + line(104, 80, 178, 214, { stroke: P, 'stroke-opacity': 0.35, 'stroke-width': 0.8 })),
        L(legs(2), legs(3) + hut),
        D(g({ stroke: P, 'stroke-width': 0.9, 'stroke-opacity': 0.8 }, ...bracing)),
        D(line(94, 120, 114, 120, { stroke: P, 'stroke-width': 1.6 }) + path('M95 93A5 7 0 0 0 95 107', { fill: 'none', stroke: P, 'stroke-width': 1 }) + path('M113 131A5 7 0 0 1 113 145', { fill: 'none', stroke: P, 'stroke-width': 1 }) + line(95, 100, 100.6, 100, { stroke: P, 'stroke-width': 0.8 }) + line(108, 138, 113, 138, { stroke: P, 'stroke-width': 0.8 })),
        D(hut),
        D(line(104, 218, 104, 290, { stroke: P, 'stroke-opacity': 0.3, 'stroke-width': 1.2, 'stroke-dasharray': '6 5', filter: 'url(#cv-ripple)' })),
        L(circle(104, 56, 10, { fill: O, 'fill-opacity': 0.35 }) + circle(104, 56, 3.4, { fill: O })),
        L(arcs([20, 38, 58, 80, 104], [2.2, 2, 1.8, 1.6, 1.4], [1, 0.8, 0.6, 0.42, 0.28], (i) => i >= 3),
          arcs([20, 38, 58], [], [1, 0.8, 0.6], () => false, 3)),
        D(g({}, ...speck)),
        D(grain(0.22, 0.1)),
        T(g({},
          stamp(18, 236, -4, O, P),
          text(18, 288, 'LOW TIDE SIGNALS', { 'font-family': SANS, 'font-size': 9, 'font-weight': 700, 'letter-spacing': 2, fill: P, 'fill-opacity': 0.82 }),
          text(282, 288, 'NJC-011', { 'text-anchor': 'end', 'font-family': MONO, 'font-size': 8, fill: P, 'fill-opacity': 0.6 }))),
      ],
    };
  },

  salt() {
    const rnd = mulberry32(920004);
    const span = (a, b) => a + (b - a) * rnd();
    const P = '#fad5b4';
    const screen = 'url(#cv-salt-screen)';
    // A salt crystal printed flat in the two inks: the side in pale, set 1.5
    // off (misregistered), then the top in orange. No shaded third face.
    const cube = (cx, cy, s) => {
      const h = s * 0.866;
      return g({},
        path(`M${r1(cx - h)} ${cy - s / 2}L${cx} ${cy}L${r1(cx + h)} ${cy - s / 2}L${r1(cx + h)} ${cy + s / 2}L${cx} ${cy + s}L${r1(cx - h)} ${cy + s / 2}Z`, { fill: P, transform: 'translate(1.5 1.5)' }),
        path(`M${cx} ${cy - s}L${r1(cx + h)} ${cy - s / 2}L${cx} ${cy}L${r1(cx - h)} ${cy - s / 2}Z`, { fill: '#f4a259' }));
    };
    const crystals = [];
    for (let i = 0; i < 26; i++) {
      const x = span(10, 290);
      const y = span(244, 296);
      const s = span(2.5, 6.5);
      crystals.push(rect(x - s / 2, y - s / 2, s, s, { fill: P, 'fill-opacity': span(0.5, 0.9), transform: `rotate(${Math.round(span(0, 45))} ${r1(x)} ${r1(y)})` }));
    }
    const band = g({},
      rect(34, 150, 232, 56, { fill: 'url(#cv-salt-sea)' }),
      line(34, 190, 266, 190, { stroke: P, 'stroke-opacity': 0.35, 'stroke-width': 1 }),
      path('M197 206L200 166L208 166L211 206Z', { fill: '#1f0e15' }),
      rect(196, 160, 16, 4, { fill: '#1f0e15' }),
      rect(199, 150, 10, 10, { fill: '#1f0e15' }));
    const defs = [
      clip('cv-salt-screen', rect(34, 40, 232, 184, { rx: 26 })),
      linear('cv-salt-sea', [[0, '#321722'], [1, '#5c2a3e']]),
      linear('cv-salt-beam', [[0, '#f4a259', 0.85], [1, '#f4a259', 0]], { gradientUnits: 'userSpaceOnUse', x1: 204, y1: 0, x2: 34, y2: 0 }),
      radial('cv-salt-glow', [[0, '#f4a259', 0.5], [1, '#f4a259', 0]]),
      el('mask', { id: 'cv-salt-noband', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 0, 300, 300, { fill: '#ffffff' }) + rect(0, 150, 300, 56, { fill: '#000000' })),
      el('pattern', { id: 'cv-salt-scan', patternUnits: 'userSpaceOnUse', width: 300, height: 3 }, rect(0, 0, 300, 1, { fill: '#000000', 'fill-opacity': 0.2 })),
      // The grains stay out of the stamp's box, so its letters print clean.
      el('mask', { id: 'cv-salt-nostamp', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 0, 300, 300, { fill: '#ffffff' }) + rect(148, 247, 140, 40, { fill: '#000000', transform: 'rotate(3 218 267)' })),
    ].join('');
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 300, { fill: '#5c2a3e' })),
        L(rect(34, 40, 232, 184, { rx: 26, fill: '#321722', stroke: P, 'stroke-opacity': 0.22, 'stroke-width': 1 })),
        D(rect(40, 46, 220, 172, { rx: 21, fill: 'none', stroke: '#000000', 'stroke-opacity': 0.25, 'stroke-width': 2 })),
        L(g({ 'clip-path': screen }, rect(34, 40, 232, 184, { filter: 'url(#cv-static)', opacity: 0.5, mask: 'url(#cv-salt-noband)' })),
          g({ stroke: P, 'stroke-opacity': 0.25, 'stroke-width': 1.6 }, ...[60, 88, 116, 144, 172, 200].map((y) => line(40, y, 260, y)))),
        L(band.replace('</g>', '') + circle(204, 155, 10, { fill: 'url(#cv-salt-glow)' }) + circle(204, 155, 3.4, { fill: '#f4a259' }) + '</g>',
          band.replace('</g>', '') + circle(204, 155, 3.4, { fill: '#f4a259' }) + '</g>'),
        L(g({ 'clip-path': screen }, path('M204 155L34 128L34 186Z', { fill: 'url(#cv-salt-beam)' }))),
        D(g({ 'clip-path': screen }, rect(34, 40, 232, 184, { fill: 'url(#cv-salt-scan)' }))),
        D(ellipse(110, 78, 70, 26, { fill: '#ffffff', 'fill-opacity': 0.06, transform: 'rotate(-12 110 78)' })),
        L(rect(0, 236, 300, 64, { fill: '#4a2132' }) + line(0, 236, 300, 236, { stroke: P, 'stroke-opacity': 0.2, 'stroke-width': 1 })),
        D(g({ mask: 'url(#cv-salt-nostamp)' }, ...crystals)),
        L(cube(62, 262, 14) + cube(118, 276, 10) + cube(150, 288, 7)),
        D(grain(0.22, 0.1)),
        T(g({},
          stamp(154, 252, 3, '#f4a259', P),
          text(18, 26, 'SALT & STATIC', { 'font-family': SANS, 'font-size': 9, 'font-weight': 700, 'letter-spacing': 2, fill: P, 'fill-opacity': 0.82 }),
          text(282, 26, 'NJC-009', { 'text-anchor': 'end', 'font-family': MONO, 'font-size': 8, fill: P, 'fill-opacity': 0.6 }))),
      ],
    };
  },

  orchard() {
    const G = '#e9c46a';
    const DK = '#211810';
    // Points every `step` units of arc length along a quadratic path.
    const along = (segs, step) => {
      const pts = [];
      let carry = step / 2;
      for (const [p0, p1, p2] of segs) {
        let prev = p0;
        for (let i = 1; i <= 400; i++) {
          const t = i / 400;
          const x = (1 - t) ** 2 * p0[0] + 2 * (1 - t) * t * p1[0] + t * t * p2[0];
          const y = (1 - t) ** 2 * p0[1] + 2 * (1 - t) * t * p1[1] + t * t * p2[1];
          carry -= Math.hypot(x - prev[0], y - prev[1]);
          prev = [x, y];
          if (carry <= 0) {
            pts.push([x, y]);
            carry += step;
          }
        }
      }
      return pts;
    };
    const wires = [
      ['M0 120Q75 160 150 128Q225 98 300 136', [[[0, 120], [75, 160], [150, 128]], [[150, 128], [225, 98], [300, 136]]]],
      ['M0 150Q100 196 200 160Q255 140 300 162', [[[0, 150], [100, 196], [200, 160]], [[200, 160], [255, 140], [300, 162]]]],
    ];
    const full = wires
      .map(([d, segs]) =>
        path(d, { fill: 'none', stroke: '#f5e4bc', 'stroke-opacity': 0.6, 'stroke-width': 1.2 }) +
        along(segs, 20).map(([x, y]) => circle(x, y + 3.6, 9, { fill: 'url(#cv-orchard-bulb)' }) + rect(x - 1.3, y + 0.2, 2.6, 1.6, { fill: DK }) + circle(x, y + 3.6, 3, { fill: G })).join(''))
      .join('');
    const small = wires.map(([d, segs]) => path(d, { fill: 'none', stroke: '#f5e4bc', 'stroke-opacity': 0.7, 'stroke-width': 2 }) + along(segs, 40).map(([x, y]) => circle(x, y, 6, { fill: G })).join('')).join('');
    const front = [30, 112, 194, 276].map((x) => circle(x, 252, 36, { fill: DK }) + rect(x - 3, 284, 6, 16, { fill: DK })).join('') + rect(0, 262, 300, 38, { fill: DK });
    return {
      defs: radial('cv-orchard-bulb', [[0, G, 0.55], [1, G, 0]]),
      layers: [
        L(rect(0, 0, 300, 300, { fill: '#3d2c1e' })),
        L(rect(0, 0, 300, 64, { fill: G })),
        D([24, 76, 128, 180, 232, 284].map((x) => circle(x, 182, 22, { fill: '#544539' })).join('')),
        D([0, 62, 124, 186, 248, 310].map((x) => circle(x, 214, 28, { fill: '#4a3828' })).join('')),
        L(front),
        L(full, small),
        D(grain(0.2, 0.08)),
        orchardBand('ORCHARD LIGHTS', 218, 'VO 4024', DK),
      ],
    };
  },

  bloom() {
    const rnd = mulberry32(920006);
    const span = (a, b) => a + (b - a) * rnd();
    const Y = '#f4d35e';
    const PALE = '#faebb7';
    const DK = '#14262d';
    const petal = (deg, out, rx, ry, a) => {
      const cx = 90 + out * Math.cos((deg * Math.PI) / 180);
      const cy = 198 + out * Math.sin((deg * Math.PI) / 180);
      return ellipse(cx, cy, rx, ry, { transform: `rotate(${deg} ${r1(cx)} ${r1(cy)})`, ...a });
    };
    const petals = [[-90, Y], [-18, PALE], [54, Y], [126, PALE]].map(([d, c]) => petal(d, 24, 26, 13, { fill: c })).join('') + petal(198, 20, 18, 9, { fill: Y, 'fill-opacity': 0.78 });
    const outlines = [-90, -18, 54, 126].map((d) => petal(d, 24, 26, 13, {})).join('') + petal(198, 20, 18, 9, {});
    const stars = [];
    for (let i = 0; i < 9; i++) stars.push(sparkle(span(160, 290), span(76, 170), span(5, 9), { stroke: PALE, 'stroke-opacity': span(0.5, 0.85) }));
    const stamens = [];
    for (let i = 0; i < 8; i++) stamens.push(circle(90 + 13 * Math.cos((i * Math.PI) / 4 + 0.3), 198 + 13 * Math.sin((i * Math.PI) / 4 + 0.3), 1.6, { fill: PALE }));
    return {
      defs: el('mask', { id: 'cv-bloom-crescent', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 0, 300, 300, { fill: '#ffffff' }) + circle(246, 98, 18, { fill: '#000000' })),
      layers: [
        L(rect(0, 0, 300, 300, { fill: '#264653' })),
        D(g({ fill: 'none', stroke: PALE, 'stroke-opacity': 0.07, 'stroke-width': 1 }, ...[40, 70, 100, 130, 160].map((r) => circle(90, 198, r)))),
        L(rect(0, 0, 300, 64, { fill: Y })),
        L(path('M90 210C96 240 104 268 122 300', { fill: 'none', stroke: '#8d8d59', 'stroke-width': 3, 'stroke-linecap': 'round' }) + ellipse(106, 254, 18, 7, { fill: '#8d8d59', transform: 'rotate(35 106 254)' }) + ellipse(100, 278, 13, 5, { fill: '#8d8d59', transform: 'rotate(-25 100 278)' }),
          path('M90 210C96 240 104 268 122 300', { fill: 'none', stroke: '#8d8d59', 'stroke-width': 3 }) + ellipse(106, 254, 18, 7, { fill: '#8d8d59', transform: 'rotate(35 106 254)' })),
        L(petals),
        D(g({ fill: 'none', stroke: DK, 'stroke-opacity': 0.3, 'stroke-width': 1, transform: 'translate(1.5 1)' }, outlines)),
        L(circle(90, 198, 9, { fill: DK }) + stamens.join(''), circle(90, 198, 9, { fill: DK })),
        L(circle(238, 104, 20, { fill: PALE, mask: 'url(#cv-bloom-crescent)' })),
        D(g({}, ...stars)),
        D(grain(0.18, 0.07)),
        orchardBand('LATE BLOOM', 156, 'VO 4026', DK),
      ],
    };
  },

  summer() {
    const rnd = mulberry32(920007);
    const span = (a, b) => a + (b - a) * rnd();
    const pick = (a, b) => Math.round(span(a, b));
    const DK = '#0b1221';
    const PALE = '#fed694';
    const O = '#fca311';
    // The near row: buildings standing on y 250, edge to edge.
    const near = [];
    for (let x = -4; x < 300; ) {
      const w = pick(14, 34);
      const cap = x > 240 ? 70 : 110;
      near.push({ x, w, h: pick(40, cap) });
      x += w;
    }
    // Two spires and a water tower on three of the taller roofs.
    const tall = near.filter((b) => b.x > 0 && b.x + b.w < 236).sort((a, b) => b.h - a.h);
    const spires = tall.slice(0, 2);
    const tower = tall[3];
    let sky = near.map((b) => `M${b.x} 250V${250 - b.h}H${b.x + b.w}V250Z`).join('');
    for (const b of spires) sky += `M${r1(b.x + b.w / 2 - 3)} ${250 - b.h}L${r1(b.x + b.w / 2)} ${250 - b.h - 18}L${r1(b.x + b.w / 2 + 3)} ${250 - b.h}Z`;
    const tx = tower.x + tower.w / 2;
    const ty = 250 - tower.h;
    sky += `M${r1(tx - 6)} ${ty}V${ty - 8}H${r1(tx - 5)}V${ty}ZM${r1(tx + 5)} ${ty}V${ty - 8}H${r1(tx + 6)}V${ty}ZM${r1(tx - 8)} ${ty - 8}V${ty - 19}Q${r1(tx)} ${ty - 23} ${r1(tx + 8)} ${ty - 19}V${ty - 8}Z`;
    const far = [];
    for (let x = -10; x < 300; ) {
      const w = pick(16, 30);
      far.push(`M${x} 250V${250 - Math.min(x > 236 ? 84 : 130, 50 + pick(20, 60))}H${x + w}V250Z`);
      x += w;
    }
    // Windows: a 7 x 9 grid inside each near building, about a third lit.
    const lit = [];
    for (const b of near) {
      for (let wx = b.x + 3; wx + 3 <= b.x + b.w - 2; wx += 7) {
        for (let wy = 250 - b.h + 6; wy + 4 <= 246; wy += 9) if (rnd() < 0.35) lit.push([wx, wy, rnd() < 0.15 ? O : PALE]);
      }
    }
    for (let i = lit.length - 1; i > 0; i--) {
      const j = Math.floor(rnd() * (i + 1));
      [lit[i], lit[j]] = [lit[j], lit[i]];
    }
    const big = lit.filter(([wx]) => wx > 0 && wx + 6 < 300).slice(0, 14);
    const stars = [];
    while (stars.length < 30) {
      const x = span(4, 296);
      const y = span(4, 128);
      if (Math.hypot(x - 178, y - 92) < 66 || (x > 238 && y < 140)) continue;
      stars.push(circle(x, y, span(0.6, 1.3), { fill: PALE, 'fill-opacity': span(0.5, 0.9) }));
    }
    const ticks = [];
    for (let i = 0; i <= 40; i++) {
      const x = 12 + i * 6.9;
      ticks.push(line(x, 290, x, 290 - (i % 8 === 0 ? 7 : i % 4 === 0 ? 5 : 3)));
    }
    const labels = [88, 92, 96, 100, 104, 108].map((f, i) => text(12 + i * 8 * 6.9, 280.5, String(f), { 'text-anchor': 'middle' })).join('');
    const vertical = (x, ys, chars, a) => chars.map((ch, i) => text(x, ys[i], ch, { 'text-anchor': 'middle', ...a })).join('');
    const defs = [
      linear('cv-summer-sky', [[0, DK], [0.6, '#14213d'], [1, '#1d2f55']]),
      radial('cv-summer-halo', [[0, O, 0.16], [1, O, 0]]),
      // A thin crescent: the moon less a circle set up and to the right.
      el('mask', { id: 'cv-summer-crescent', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 0, 300, 300, { fill: '#ffffff' }) + circle(184, 84, 33, { fill: '#000000' })),
      el('mask', { id: 'cv-summer-crescent-s', maskUnits: 'userSpaceOnUse', x: 0, y: 0, width: 300, height: 300 }, rect(0, 0, 300, 300, { fill: '#ffffff' }) + circle(188, 81, 30, { fill: '#000000' })),
    ].join('');
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 300, { fill: 'url(#cv-summer-sky)' })),
        D(g({}, ...stars, sparkle(40, 52, 9, { stroke: PALE, 'stroke-opacity': 0.9 }), sparkle(40, 52, 4, { stroke: PALE, transform: 'rotate(45 40 52)', 'stroke-opacity': 0.6 }))),
        L(circle(178, 90, 52, { fill: 'url(#cv-summer-halo)' }) + circle(178, 90, 34, { fill: O, mask: 'url(#cv-summer-crescent)' }),
          circle(178, 90, 34, { fill: O, mask: 'url(#cv-summer-crescent-s)' })),
        D(path(far.join(''), { fill: '#1d2f55' })),
        L(path(sky, { fill: DK })),
        L(g({ 'fill-opacity': 0.85 }, ...lit.map(([x, y, c]) => rect(x, y, 3, 4, { fill: c }))),
          g({ 'fill-opacity': 0.9 }, ...big.map(([x, y, c]) => rect(x, y, 6, 7, { fill: c })))),
        L(rect(0, 248, 300, 52, { fill: DK }) + line(0, 248, 300, 248, { stroke: '#303c54', 'stroke-width': 1 })),
        D(rect(214, 222, 36, 26, { rx: 3, fill: '#14213d', stroke: '#303c54', 'stroke-width': 1 }) + line(220, 248, 220, 236, { stroke: '#303c54', 'stroke-width': 1.4 }) + line(244, 248, 244, 236, { stroke: '#303c54', 'stroke-width': 1.4 }) + line(214, 230, 250, 230, { stroke: '#303c54', 'stroke-width': 0.8 })),
        L(g({ stroke: PALE, 'stroke-width': 1.4, 'stroke-linecap': 'round' }, line(72, 300, 72, 170), line(55, 176, 89, 176), line(58, 186, 86, 186), line(61, 196, 83, 196))),
        L(g({ fill: 'none', stroke: O, 'stroke-width': 1.4, 'stroke-linecap': 'round' }, path(arc(72, 170, 14, -60, 20), { 'stroke-opacity': 0.9 }), path(arc(72, 170, 26, -60, 20), { 'stroke-opacity': 0.6 }), path(arc(72, 170, 40, -60, 20), { 'stroke-opacity': 0.35 })),
          g({ fill: 'none', stroke: O, 'stroke-width': 2.4, 'stroke-linecap': 'round' }, path(arc(72, 170, 14, -60, 20), { 'stroke-opacity': 0.9 }), path(arc(72, 170, 26, -60, 20), { 'stroke-opacity': 0.6 }))),
        D(grain(0.2, 0.08)),
        // The radio dial counts as type: hairlines and numbers.
        T(g({},
          line(12, 290, 288, 290, { stroke: PALE, 'stroke-opacity': 0.5, 'stroke-width': 1 }),
          g({ stroke: PALE, 'stroke-opacity': 0.5, 'stroke-width': 0.8 }, ...ticks),
          g({ 'font-family': MONO, 'font-size': 6, fill: PALE, 'fill-opacity': 0.7 }, labels),
          text(12, 298, 'FM', { 'font-family': MONO, 'font-size': 6, 'font-weight': 700, fill: PALE, 'fill-opacity': 0.7 }),
          line(158.3, 277, 158.3, 298, { stroke: O, 'stroke-width': 1.6, 'stroke-linecap': 'round' }))),
        T(g({},
          vertical(272, [46, 74, 102, 130], ['夏', '夜', '信', '号'], { 'font-family': SERIF, 'font-size': 24, 'font-weight': 700, fill: PALE }),
          vertical(250, [44, 56, 68, 80], ['青', '柠', '电', '台'], { 'font-family': SERIF, 'font-size': 10, 'font-weight': 600, fill: O }),
          circle(250, 30, 3, { fill: '#b8d86b' }))),
      ],
    };
  },

  shore() {
    const PALE = '#cfecdc';
    const MID = '#588c72';
    const DK = '#0e241b';
    let foam = 'M0 178';
    for (let x = 0; x < 300; x += 24) foam += x === 0 ? 'Q12 171 24 178' : `T${x + 24} 178`;
    const waves = [[140, '10 7', 60, 280], [147, '14 8', 20, 250], [155, '18 9', 90, 300], [163, '22 10', 0, 220], [170, '26 12', 40, 300]].map(([y, d, x1, x2]) => line(x1, y, x2, y, { stroke: PALE, 'stroke-opacity': 0.35, 'stroke-width': 1, 'stroke-dasharray': d }));
    const glitter = [30, 24, 19, 15, 11, 8].map((w, i) => rect(225 - w / 2, 136 + i * 6.4, w, 1.6, { rx: 0.8, fill: PALE, 'fill-opacity': 0.75 - i * 0.08 }));
    // An earbud: a round head and a stem pointing at `deg` (0 is east).
    const bud = (x, y, deg, detail) => {
      const mesh = [];
      if (detail) for (let i = -1; i <= 1; i++) for (let j = -1; j <= 1; j++) mesh.push(circle(x + i * 2.6, y + j * 2.6, 0.7, { fill: PALE, 'fill-opacity': 0.5 }));
      return g({},
        rect(x + 6, y - 3, 22, 6, { rx: 3, fill: DK, transform: `rotate(${deg} ${x} ${y})` }),
        circle(x, y, 11, { fill: DK }),
        path(arc(x, y, 7, 175, 275), { fill: 'none', stroke: PALE, 'stroke-opacity': 0.6, 'stroke-width': 1.4, 'stroke-linecap': 'round' }),
        ...mesh);
    };
    const end = (x, y, deg) => [r1(x + 28 * Math.cos((deg * Math.PI) / 180)), r1(y + 28 * Math.sin((deg * Math.PI) / 180))];
    const [e1x, e1y] = end(96, 236, 52);
    const [e2x, e2y] = end(140, 252, 128);
    const cable = g({ fill: 'none', stroke: DK, 'stroke-width': 2.2, 'stroke-linecap': 'round' },
      path(`M${e1x} ${e1y}Q${r1(e1x + 4)} ${r1(e1y + 10)} 121 266`),
      path(`M${e2x} ${e2y}Q${r1(e2x - 2)} ${r1(e2y + 8)} 123 266`));
    const ridges = [-60, -30, 0, 30, 60].map((a) => {
      const x = 238 + 14 * Math.sin((a * Math.PI) / 180);
      const y = 262 - 14 * Math.cos((a * Math.PI) / 180);
      return line(238, 270, x, y);
    });
    const vertical = (x, ys, chars, a) => chars.map((ch, i) => text(x, ys[i], ch, { 'text-anchor': 'middle', ...a })).join('');
    const defs = [
      linear('cv-shore-sky', [[0, '#1b4332'], [1, '#365a4b']]),
      radial('cv-shore-halo', [[0, PALE, 0.3], [1, PALE, 0]]),
      el('pattern', { id: 'cv-shore-sand', patternUnits: 'userSpaceOnUse', width: 5, height: 5 }, circle(2.5, 2.5, 0.7, { fill: MID })),
      linear('cv-shore-cable', [[0, DK], [1, PALE]], { gradientUnits: 'userSpaceOnUse', x1: 230, y1: 0, x2: 280, y2: 0 }),
      clip('cv-shore-sky', rect(0, 0, 300, 132)),
    ].join('');
    return {
      defs,
      layers: [
        L(rect(0, 0, 300, 132, { fill: 'url(#cv-shore-sky)' })),
        // A low sun, half under the sea line.
        L(g({ 'clip-path': 'url(#cv-shore-sky)' }, circle(225, 132, 64, { fill: 'url(#cv-shore-halo)' }), circle(225, 132, 40, { fill: PALE }))),
        L(rect(0, 132, 300, 44, { fill: MID }) + line(0, 132, 300, 132, { stroke: PALE, 'stroke-opacity': 0.6, 'stroke-width': 1 }), rect(0, 132, 300, 44, { fill: MID })),
        D(waves.join('') + glitter.join('')),
        L(rect(0, 176, 300, 124, { fill: '#95d5b2' })),
        D(rect(0, 176, 300, 124, { fill: 'url(#cv-shore-sand)', opacity: 0.35 })),
        D(path(foam, { fill: 'none', stroke: MID, 'stroke-opacity': 0.4, 'stroke-width': 2, transform: 'translate(0 5)' })),
        L(path(foam, { fill: 'none', stroke: PALE, 'stroke-width': 3, 'stroke-linejoin': 'round', 'stroke-linecap': 'round' })),
        D(g({ fill: PALE, stroke: MID, 'stroke-width': 0.8, 'stroke-linejoin': 'round' },
          path('M238 270L223.5 255A20 20 0 0 1 252.5 255Z'),
          g({ fill: 'none' }, ...ridges))),
        L(cable + rect(120, 264, 4, 8, { rx: 2, fill: DK }) + path('M122 272C170 292 210 252 236 222S268 170 300 178', { fill: 'none', stroke: 'url(#cv-shore-cable)', 'stroke-width': 2.2, 'stroke-linecap': 'round' })),
        L(bud(96, 236, 52, true) + bud(140, 252, 128, true), bud(96, 236, 52, false) + bud(140, 252, 128, false)),
        D(grain(0.16, 0.06)),
        T(g({},
          vertical(26, [40, 66, 92, 118, 144], ['海', '边', '的', '耳', '机'], { 'font-family': SERIF, 'font-size': 20, 'font-weight': 700, fill: PALE }),
          vertical(48, [40, 52, 64, 76], ['青', '柠', '电', '台'], { 'font-family': SERIF, 'font-size': 9, fill: '#95d5b2' }),
          circle(48, 29, 3, { fill: '#b8d86b' }),
          text(282, 22, 'FM 98.6', { 'text-anchor': 'end', 'font-family': MONO, 'font-size': 7, fill: PALE, 'fill-opacity': 0.7 }))),
      ],
    };
  },
};

// ---- output ----------------------------------------------------------------

let cached;
function drawn() {
  if (!cached) cached = Object.fromEntries(ALBUMS.map((a) => [a.key, COVERS[a.key]()]));
  return cached;
}

/** One album's picture group (for the sprite's defs) and its full, mid and small <symbol>s. */
export function symbols(key) {
  const { layers } = drawn()[key];
  const first = layers.findIndex((l) => l.type);
  if (first >= 0 && layers.slice(first).some((l) => !l.type)) throw new Error(`${key}: type layers must come last`);
  const art = layers.filter((l) => !l.type).map((l) => l.full).join('');
  const type = layers.filter((l) => l.type).map((l) => l.full).join('');
  const small = layers.map((l) => (l.small === undefined ? l.full : l.small ?? '')).join('');
  const use = `<use href="#cv-${key}-art"/>`;
  return {
    art: `<g id="cv-${key}-art">${art}</g>`,
    full: `<symbol id="cv-${key}" viewBox="0 0 300 300">${use}${type}</symbol>`,
    mid: `<symbol id="cv-${key}-m" viewBox="0 0 300 300">${use}</symbol>`,
    small: `<symbol id="cv-${key}-s" viewBox="0 0 300 300">${small}</symbol>`,
  };
}

/** The hidden sprite: shared and per-cover defs with the pictures, then the twenty-four symbols. */
export function sprite(extraDefs = '') {
  const all = drawn();
  const each = ALBUMS.map((a) => symbols(a.key));
  const defs = SHARED + ALBUMS.map((a) => all[a.key].defs).join('') + each.map((s) => s.art).join('') + extraDefs;
  const syms = each.map((s) => s.full + s.mid + s.small).join('');
  return `<svg class="sprite" aria-hidden="true" focusable="false" xmlns="http://www.w3.org/2000/svg"><defs>${defs}</defs>${syms}</svg>`;
}

/** One cover: a 300 x 300 picture of an album; size and radius come from CSS.
 * `size` is the variant: 'full' (160 design px and up), 'mid' (96 to 159) or 'small'. */
export const cover = (key, { size = 'full', cls = '' } = {}) =>
  `<span class="cv-wrap${cls ? ` ${cls}` : ''}"><svg class="cv" viewBox="0 0 300 300" aria-hidden="true" focusable="false"><use href="#cv-${key}${{ full: '', mid: '-m', small: '-s' }[size]}"/></svg></span>`;
