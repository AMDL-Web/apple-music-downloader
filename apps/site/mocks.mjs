// The product mocks (DESIGN.md section 5) and the hero's shelf of sleeves, as
// HTML on the demo catalog. Each mock is a picture of one real screen in the
// app's own tokens (static/mocks.css): role="img" with a label from the
// content files, everything inside aria-hidden, no control inside. The words
// in a mock are the app's English labels, on both language pages, so the
// inner aria-hidden wrapper carries lang="en"; the label on the frame is in
// the page's language. In M4 only the tool calls are English: the chat rows
// are the page's copy.
import { ICON, esc, sized } from './blocks.mjs';
import { ALBUMS, ARTISTS, album, albumUrl, cover } from './covers.mjs';

// ---- shared data -----------------------------------------------------------

// Where a track lands: the default library_path_format,
// library/{AlbumArtist}/{AlbumName}/{SongNumber:02d}. {SongName}.
export const libraryPath = (artist, albumName, n, title) => `library/${artist}/${albumName}/${String(n).padStart(2, '0')}. ${title}.m4a`;
export const libraryFolder = (artist, albumName) => `library/${artist}/${albumName}/`;

const GLASS = album('glass');
const GLASS_ARTIST = ARTISTS[GLASS.artist];
// The hero's file and card C's first file.
export const GLASS_FILE = libraryPath(GLASS_ARTIST, GLASS.title, 1, GLASS.tracks[0][0]).split('/').pop();
export const GLASS_QUALITY = 'ALAC 24-bit · 96 kHz';

// Card C's folder tree: the default template's folders for the demo album,
// the artist's picture in the artist folder, the cover in the album folder.
const [ROOT, ARTIST_DIR, ALBUM_DIR] = libraryFolder(GLASS_ARTIST, GLASS.title).split('/');
export const GLASS_TREE = [
  ['', 'dir', `${ROOT}/`],
  ['└─ ', 'dir', `${ARTIST_DIR}/`],
  ['   ├─ ', 'file', 'artist.jpg'],
  ['   └─ ', 'dir', `${ALBUM_DIR}/`],
  ['      ├─ ', 'file', GLASS_FILE],
  ['      ├─ ', 'file', libraryPath(GLASS_ARTIST, GLASS.title, 2, GLASS.tracks[1][0]).split('/').pop()],
  ['      ├─ ', 'file', libraryPath(GLASS_ARTIST, GLASS.title, 3, GLASS.tracks[2][0]).split('/').pop()],
  ['      ├─ ', 'file', '…'],
  ['      └─ ', 'file', 'cover.jpg'],
];

// Every tool the MCP server declares (internal/capability/capability.go#All),
// grouped as the page lists them. A test keeps this equal to capability.go.
export const MCP_TOOLS = {
  catalog: ['search_catalog', 'list_artist_albums', 'get_album', 'get_playlist', 'lookup_link', 'probe_quality'],
  jobs: ['start_download', 'list_jobs', 'get_job', 'wait_for_job', 'cancel_job', 'retry_job', 'move_job', 'delete_job'],
  library: ['list_library', 'list_library_playlists', 'get_resource', 'get_library_playlist', 'get_library_summary', 'download_resource', 'reset_resource', 'pause_resource', 'resume_resource', 'delete_resource', 'check_library', 'get_library_check'],
  apple: ['list_apple_library', 'list_apple_library_playlist_tracks', 'sync_apple_library', 'get_apple_library_status'],
  system: ['get_status', 'get_logs', 'get_config', 'set_config', 'list_accounts', 'update_account', 'delete_account'],
};

// The mcpServers entry `amdl cli setup` prints, over eight lines.
export const MCP_CONFIG = [
  '{',
  '  "mcpServers": {',
  '    "amdl": {',
  '      "command": "/Users/me/.local/bin/amdl",',
  '      "args": ["mcp", "--server", "nas"]',
  '    }',
  '  }',
  '}',
];

// ---- icons inside mocks ----------------------------------------------------

// Drawn after Lucide (24 grid, 2px stroke), the set the console and the
// desktop app approximate. The size comes from a class.
const LU = {
  'panel-left': '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18"/>',
  house: '<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V20a1 1 0 0 0 1 1h4v-6h4v6h4a1 1 0 0 0 1-1V9.5"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  library: '<path d="M16 6l4 14M12 6v14M8 8v12M4 4v16"/>',
  server: '<rect x="2" y="2" width="20" height="8" rx="2"/><rect x="2" y="14" width="20" height="8" rx="2"/><path d="M6 6h.01M6 18h.01"/>',
  'chevron-left': '<path d="m15 18-6-6 6-6"/>',
  'chevron-right': '<path d="m9 18 6-6-6-6"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  'arrow-down': '<path d="M12 5v14M19 12l-7 7-7-7"/>',
  'layout-grid': '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/>',
  activity: '<path d="M22 12h-4l-3 9L9 3l-3 9H2"/>',
  settings: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>',
  'file-text': '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M16 13H8M16 17H8M10 9H8"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
  monitor: '<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>',
  'circle-check': '<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>',
  'circle-x': '<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6M9 9l6 6"/>',
  music: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  clock: '<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
};
const lu = (name, size, extra = '') =>
  `<svg class="li li-${size}${extra ? ` ${extra}` : ''}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">${LU[name]}</svg>`;
// The console's running eyebrow: three equalizer bars.
const eq = '<svg class="li li-12" viewBox="0 0 12 12" fill="currentColor"><rect x="1" y="5" width="2.4" height="6" rx="1"/><rect x="4.8" y="2" width="2.4" height="9" rx="1"/><rect x="8.6" y="6.5" width="2.4" height="4.5" rx="1"/></svg>';

const frame = (cls, label, inner) => `<div class="mock ${cls}" role="img" aria-label="${esc(label)}">${inner}</div>`;
const repeat = (n, s) => Array.from({ length: n }, (_, i) => (typeof s === 'function' ? s(i) : s)).join('');

// ---- M1: the desktop app's album page --------------------------------------

const DK_ROWS = [
  [1, 'Glass Weather', '98.6', '3:52'],
  [2, 'Paper Satellites', '107.9', '4:14'],
  [3, 'Slow Lightning', '117.3', '4:36'],
  [4, 'Northbound', '127.1', '4:59'],
  [5, 'Halo Static', '85.4', '3:21'],
  [6, 'Small Hours', '95.2', '3:44'],
];
export const DK_FOLDER = `/volume1/music/${libraryFolder(GLASS_ARTIST, GLASS.title)}`;

function dkRow([n, title, size, len], phone) {
  const running = n === 6;
  const state = running ? `${lu('arrow-down', 13)}<span>62%</span>` : `${lu('check', 13)}<span>Saved</span>`;
  const meter = phone ? '' : `<span class="dk-meter${running ? ' is-run' : ''}"><i></i><i><b></b></i><i class="d"></i><i class="d"></i><i class="d"></i></span>`;
  return `<li${running ? ' class="is-run"' : ''}><span class="dk-n">${n}</span><span class="dk-t"><b>${esc(title)}</b><span>alac 24/96 · ${size} MB</span></span>${meter}<span class="dk-len">${len}</span><span class="dk-state">${state}</span></li>`;
}

const dkCells = `<span class="dk-cells">${repeat(12, (i) => `<i class="${i < 5 ? 'ok' : i < 7 ? 'run' : 'rest'}"></i>`)}</span>`;

export function desktopMock(label) {
  const wide = `<div class="dk dk-wide" aria-hidden="true" lang="en">
<div class="dk-side">${lu('panel-left', 16, 'dk-toggle')}
<ul class="dk-nav"><li class="on">${lu('house', 14)}<span>Home</span></li><li>${lu('list', 14)}<span>Queue</span><span class="dk-count">3</span></li><li>${lu('library', 14)}<span>Library</span></li></ul>
<div class="dk-now">${cover('tide', { size: 'small', cls: 'dk-now-cv' })}<span class="dk-now-t"><b>Low Tide Signals</b><span>1 of 11</span><span class="dk-mini"><i class="done"></i><i class="run"></i><i class="rest"></i>${repeat(5, '<i class="dot"></i>')}</span></span></div>
<div class="dk-conn"><span class="dk-tile">${lu('server', 16)}</span><span class="dk-conn-t"><b>nas:18570</b><span>Live</span></span></div>
</div>
<div class="dk-top">${lu('chevron-left', 14)}<span>Home</span><span class="dk-btn">Pause</span></div>
<div class="dk-head">${cover('glass', { cls: 'dk-cover' })}
<div class="dk-info"><p class="dk-eyebrow">ALBUM</p><p class="dk-title">Glass Weather</p><p class="dk-artist"><span>Mira Solenne</span><span class="dk-gilt">${GLASS_QUALITY}</span></p>
<div class="dk-figs"><span class="hero-ink"><small>SIZE</small><b>536.3<em> MB</em></b></span><span><small>SPEED</small><b>6.4<em> MB/s</em></b></span><span><small>LEFT</small><b>~3 min</b></span><span class="hero-ink"><small>TRACKS</small><b>5 / 12</b></span></div>
${dkCells}<p class="dk-prog"><span>5 / 12 finished</span><span>58%</span></p></div>
</div>
<div class="dk-body"><ol class="dk-tracks">${DK_ROWS.map((r) => dkRow(r, false)).join('')}</ol>
<div class="dk-aside"><p class="dk-eyebrow">ACTIVITY</p><p class="dk-act"><span>18:27:00</span><span>Queued</span></p><div class="dk-saved"><p class="dk-eyebrow">SAVED TO</p><p class="dk-path">${esc(DK_FOLDER)}</p></div></div>
</div>
</div>`;
  const phone = `<div class="dk dk-phone" aria-hidden="true" lang="en">
<div class="dkp-top">${lu('chevron-left', 14)}<span>Home</span><span class="dk-btn">Pause</span></div>
<div class="dkp-head">${cover('glass', { size: 'mid', cls: 'dkp-cover' })}<div class="dkp-info"><p class="dk-eyebrow">ALBUM</p><p class="dk-title">Glass Weather</p><p class="dk-artist">Mira Solenne</p><p><span class="dk-gilt">${GLASS_QUALITY}</span></p></div></div>
<div class="dkp-cells">${dkCells}<p class="dk-prog"><span>5 / 12 finished</span><span>58%</span></p></div>
<ol class="dk-tracks">${DK_ROWS.slice(1).map((r) => dkRow(r, true)).join('')}</ol>
</div>`;
  return frame('mock-desktop', label, wide + phone);
}

// ---- M2: the web console's Overview ----------------------------------------

// One card per running album; its genre, year and track count (one cell per
// track) come from the catalog, so they cannot drift from it. The percent
// counts the running track's part too.
export const LIVE = [
  { key: 'tidal', started: '21:16', saved: 7, pct: '74%', left: '~2 min left' },
  { key: 'tide', started: '21:18', saved: 4, pct: '41%', left: '~6 min left' },
  { key: 'summer', started: '21:19', saved: 1, pct: '16%', left: '~11 min left' },
].map((c) => {
  const a = album(c.key);
  return { ...c, facts: `${a.genre} · ${a.year} · Started ${c.started}`, cells: a.tracks.length };
});

function liveCard(c) {
  const a = album(c.key);
  return `<div class="cn-live lc-${c.key}">${cover(c.key, { size: 'mid', cls: 'cn-live-cv' })}<div class="cn-live-t"><p class="cn-eyebrow">${eq}<span>RUNNING</span></p><p class="cn-live-title">${esc(a.title)}</p><p class="cn-live-artist">${esc(ARTISTS[a.artist])}</p><p class="cn-live-facts">${esc(c.facts)}</p></div>
<div class="cn-live-foot"><p class="cn-cells">${repeat(c.cells, (i) => `<i${i < c.saved ? ' class="on"' : ''}></i>`)}<b>${c.pct}</b></p><p class="cn-legend"><span><i class="on"></i>${c.saved} saved</span><span><i></i>${c.cells - c.saved} remaining</span><span>${c.left}</span></p></div></div>`;
}

const queuedCard = `<div class="cn-live cn-queued"><span class="cn-ph">${lu('music', 32)}</span><div class="cn-live-t"><p class="cn-eyebrow">${lu('clock', 12)}<span>QUEUED</span></p><p class="cn-live-title cn-mono">us/album/920004</p><p class="cn-live-artist">Not resolved yet</p></div></div>`;

const attention = (phone) =>
  `<div class="cn-group cn-attn">${lu('circle-x', 20, 'cn-x')}<div class="cn-attn-t"><p><span class="cn-chip">Failed</span><span class="cn-mono">us/album/920099</span></p><p class="cn-sec">Apple Music has nothing at this link in the US storefront</p>${phone ? '<p class="cn-sec">10s ago</p>' : ''}</div>${phone ? '' : '<span class="cn-sec cn-ago">10s ago</span>'}</div>`;

const brandTile = `<span class="cn-brand-tile">${lu('music', 18)}</span>`;

export function consoleMock(label) {
  const wide = `<div class="cn cn-wide" aria-hidden="true" lang="en">
<div class="cn-side"><p class="cn-brand">${brandTile}<span>AMDL</span></p><p class="cn-new">${lu('plus', 16)}<span>New download</span></p>
<ul class="cn-nav"><li class="on">${lu('layout-grid', 18)}<span>Overview</span></li><li>${lu('activity', 18)}<span>Activity</span><span class="cn-badge">4</span></li><li>${lu('settings', 18)}<span>Settings</span></li></ul>
<p class="cn-logs">${lu('file-text', 18)}<span>Logs</span></p></div>
<div class="cn-main"><div class="cn-top"><i class="cn-dot"></i><span class="cn-search">${lu('search', 16)}<span>Search or paste a link…</span><kbd>Ctrl K</kbd></span><span class="cn-theme">${lu('monitor', 18)}</span></div>
<div class="cn-page"><p class="cn-h1">Overview</p><p class="cn-date">Nov 8</p>
<p class="cn-health">${lu('circle-check', 16)}<span>All systems healthy</span><span class="cn-link">Status</span></p>
<div class="cn-head"><p><b>Needs attention</b><span>Last 24 hours</span></p><span class="cn-link">All failed${lu('chevron-right', 16)}</span></div>
${attention(false)}
<div class="cn-head cn-head-q"><p><b>Live queue</b></p><span class="cn-link">Open queue${lu('chevron-right', 16)}</span></div>
<div class="cn-grid">${LIVE.map(liveCard).join('')}${queuedCard}</div>
</div></div>
</div>`;
  const tab = (icon, name, on) => `<span class="cnp-tab${on ? ' on' : ''}">${icon}<span>${name}</span></span>`;
  const phone = `<div class="cn cn-phone" aria-hidden="true" lang="en">
<div class="cnp-top">${brandTile}<i class="cn-dot"></i></div>
<div class="cnp-page"><p class="cn-h1">Overview</p><p class="cn-date">Nov 8</p>
<p class="cn-health">${lu('circle-check', 16)}<span>All systems healthy</span></p>
<div class="cn-head"><p><b>Needs attention</b></p></div>
${attention(true)}
<div class="cn-head"><p><b>Live queue</b></p></div>
${LIVE.slice(0, 2).map(liveCard).join('')}
</div>
<div class="cnp-tabs">${tab(lu('layout-grid', 20), 'Overview', true)}${tab(lu('activity', 20), 'Activity')}<span class="cnp-tab"><span class="cnp-new">${lu('plus', 18)}</span><span>New</span></span>${tab(lu('settings', 20), 'Settings')}</div>
</div>`;
  return frame('mock-console', label, wide + phone);
}

// ---- M3: the terminal UI ---------------------------------------------------

const sp = (n) => ' '.repeat(n);
const pad = (s, n) => s + sp(Math.max(0, n - [...s].length));
// A run of text in a class; the logo's blocks are drawn as boxes so the bars
// stay solid in any font.
function run([cls, s]) {
  if (cls === 'b1' || cls === 'b2' || cls === 'b3' || cls === 'b4') {
    if (/^[█▄]+$/.test(s)) return `<span class="t-${cls}">${[...s].map((ch) => (ch === '█' ? '<i class="blk"></i>' : '<i class="blk half"></i>')).join('')}</span>`;
  }
  if (!cls) return esc(s);
  return `<span class="${cls.split(' ').map((c) => `t-${c}`).join(' ')}">${esc(s)}</span>`;
}
const tline = (runs, sel = false) => (sel ? `<span class="t-sel">${runs.map(run).join('')}</span>` : runs.map(run).join(''));

const LOGO1 = [['', sp(3)], ['b2', '██'], ['', sp(4)], ['b4', '▄▄'], ['', sp(3)], ['bold', 'amdl'], ['dim', ' 2.0.0']];
const LOGO3 = [['b1', '██'], ['', ' '], ['b2', '██'], ['', ' '], ['b3', '██'], ['', ' '], ['b4', '██'], ['', sp(3)], ['dim', '/volume1/music · 1.2 TB free']];
const logo2 = (tail) => [['b1', '██'], ['', ' '], ['b2', '██'], ['', ' '], ['b3', '▄▄'], ['', ' '], ['b4', '██'], ['', sp(3)], ['dim', tail]];
const COUNTS = [['dim', '  128 saved · '], ['bad', '2 failed']];
// A search row's facts, from the catalog.
const albumFacts = (key) => {
  const a = album(key);
  return `${ARTISTS[a.artist]} · ${a.year} · ${a.tracks.length} tracks`;
};
const songFacts = (key, title) => {
  const a = album(key);
  return `${ARTISTS[a.artist]} · ${a.tracks.find(([t]) => t === title)[1]}`;
};
const GLASS_N = GLASS.tracks.length;
const ASK = [['b1', '> '], ['fg', 'mira solenne']];

const TUI_WIDE = [
  LOGO1,
  logo2('nas · nas.local:18570 · wrapper ready (us)'),
  LOGO3,
  [],
  COUNTS,
  [],
  ASK,
  [['acc', '◆ '], ['bold', 'Glass Weather'], ['dim', ' · Mira Solenne #1'], ['', sp(57)], ['dim', 'ALAC 24/96']],
  [['fg', `  3/${GLASS_N}  `], ['acc', '━━━━'], ['', ' '], ['skip', '━━━━━'], ['', ' '], ['acc', '━━━━'], ['', ' '], ['bad', '━━━━'], ['', ' '],
    ['soft', '╍╍╍╍╍'], ['', ' '], ['soft', '╍╍'], ['faint', '──'], ['', ' '], ['faint', '──── ───── ──── ──── ───── ────'], ['dim', '  46%  ~1 min']],
  [['dim', '  03 '], ['fg', pad('Slow Lightning', 24)], ['', '  '], ['acc', '━━━━━━ ━━━━━━━━━━━━━━━━━━━━'], ['faint', '──────────── '], ['faint', '─ ─ ─ ─ ─'], ['', '  '], ['dim', 'Download 62%']],
  [['dim', '  05 '], ['fg', pad('Halo Static', 24)], ['', '  '], ['acc', '━━━━━━ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ ╸ '], ['faint', '─ ─ ─ ─'], ['', '  '], ['dim', 'Decrypt 35%']],
  [['dim', '  06 '], ['fg', pad('Small Hours', 24)], ['', '  '], ['acc', '━━━━━━ ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━ '], ['bad', '━ '], ['faint', '─ ─ ─ ─'], ['', '  '], ['bad', '✗ decrypt failed']],
  [['b1', '◆ '], ['bold', 'Slow Lightning Radio'], ['dim', ' #2'], ['', sp(69)], ['dim', 'queued']],
  [['faint', `╭${'─'.repeat(98)}╮`]],
  [['faint', '│ '], ['b1', '> '], ['dim', pad('link, search or /', 95)], ['faint', '│']],
  [['faint', `╰${'─'.repeat(98)}╯`]],
  [['fg', '  search · mira solenne   '], ['bold u', 'all'], ['dim', '  album  song  playlist  artist']],
  'sel',
  [['', sp(4)], ['b1', '● '], ['dim', 'album    '], ['fg', pad('Tidal Rooms', 30)], ['dim', `  ${albumFacts('tidal')}`]],
  [['', sp(4)], ['dim', '○ '], ['dim', 'song     '], ['fg', pad('Paper Satellites', 30)], ['dim', `  ${songFacts('glass', 'Paper Satellites')}`]],
  [['', sp(4)], ['dim', '○ '], ['dim', 'playlist '], ['fg', pad('Slow Lightning Radio', 30)], ['dim', '  Mira Solenne']],
  [['', sp(4)], ['dim', '○ '], ['dim', 'artist   '], ['fg', pad('Mira Solenne', 30)]],
  [['ok', '  ●'], ['dim', ' 1 running · 1 queued'], ['', sp(30)], ['dim', 'enter downloads · space selects · tab filters']],
];
const SEL_WIDE = [['', '  '], ['b1', '▸ '], ['dim', '○ '], ['dim', 'album    '], ['bold', pad('Glass Weather', 30)], ['dim', pad(`  ${albumFacts('glass')}`, 41)], ['ok', '  ✓ in library']];

const TUI_NARROW = [
  LOGO1,
  logo2('nas · nas.local:18570 · wrapper…'),
  LOGO3,
  [],
  COUNTS,
  [],
  ASK,
  [['acc', '◆ '], ['bold', 'Glass Weather'], ['dim', ' #1']],
  [['fg', `  3/${GLASS_N}  `], ['acc', '━━━'], ['skip', '━━'], ['acc', '━━━'], ['bad', '━━━'], ['soft', '╍╍╍╍'], ['faint', '─'.repeat(18)], ['dim', '  46%']],
  [['b1', '◆ '], ['bold', 'Slow Lightning Radio'], ['dim', ' #2'], ['', sp(15)], ['dim', 'queued']],
  [['faint', `╭${'─'.repeat(44)}╮`]],
  [['faint', '│ '], ['b1', '> '], ['dim', pad('link, search or /', 41)], ['faint', '│']],
  [['faint', `╰${'─'.repeat(44)}╯`]],
  [['fg', '  search · mira solenne']],
  'sel',
  [['', sp(4)], ['b1', '● '], ['dim', 'album    '], ['fg', 'Tidal Rooms']],
  [['', sp(4)], ['dim', '○ '], ['dim', 'song     '], ['fg', 'Paper Satellites']],
  [['', sp(4)], ['dim', '○ '], ['dim', 'playlist '], ['fg', 'Slow Lightning Radio']],
  [['', sp(4)], ['dim', '○ '], ['dim', 'artist   '], ['fg', 'Mira Solenne']],
  [['ok', '  ●'], ['dim', ' 1 running · 1 queued'], ['', sp(6)], ['dim', 'enter downloads']],
];
const SEL_NARROW = [['', '  '], ['b1', '▸ '], ['dim', '○ '], ['dim', 'album    '], ['bold', pad('Glass Weather', 31)]];

const screen = (lines, sel) => lines.map((l) => (l === 'sel' ? tline(sel, true) : tline(l))).join('\n');

// Track titles the mocks show, for the demo-data test.
export const SHOWN_TRACKS = { glass: [...DK_ROWS.map((r) => r[1]), 'Slow Lightning', 'Halo Static', 'Small Hours', 'Paper Satellites'] };

export function terminalMock(label) {
  return frame(
    'mock-terminal',
    label,
    `<div class="tw" aria-hidden="true" lang="en"><div class="tw-bar"><i></i><i></i><i></i><span>~ — amdl</span></div><div class="tw-body"><pre class="tui tui-wide">${screen(TUI_WIDE, SEL_WIDE)}</pre><pre class="tui tui-narrow">${screen(TUI_NARROW, SEL_NARROW)}</pre></div></div>`,
  );
}

// ---- M4: an agent over MCP -------------------------------------------------

// One line of JSON, colored: keys, strings, numbers, punctuation.
function jsonLine(s) {
  return s.replace(/("(?:[^"\\]|\\.)*")(\s*:)?|(-?\b\d+(?:\.\d+)?\b)|(true|false|null)|([{}[\],:])|(…)/g, (m, str, colon, num, lit, punct, ell) => {
    if (str) return colon ? `<span class="j-k">${esc(str)}</span><span class="j-p">${colon}</span>` : `<span class="j-s">${esc(str)}</span>`;
    if (num || lit) return `<span class="j-n">${m}</span>`;
    if (punct) return `<span class="j-p">${punct}</span>`;
    return `<span class="j-p">${ell}</span>`;
  });
}

const TOOL_CALLS = [
  ['search_catalog', '{"term":"Nightjar Coast","types":["albums"],"limit":3}',
    `{"albums":{"items":[{"name":"Low Tide Signals","release_date":"${album('tide').date}","url":"…/920003"},{"name":"Salt & Static","release_date":"${album('salt').date}","url":"…/920004"}]}}`],
  ['start_download', `{"urls":["${albumUrl(album('tide'))}"],"codec_priority":["ec3"]}`,
    '{"accepted":[{"url":"…/920003","status":"accepted","job_id":"job_9b2c…","resource":{"kind":"album","id":"920003"}}],"rejected":[]}'],
  ['wait_for_job', '{"job_id":"job_9b2c…","timeout_s":300}',
    '{"done":true,"job":{"status":"completed","album_progress":{"total":11,"done":11,"failed":0,"percent":100},…}}'],
];

export function agentChat(chat, inline, label) {
  const calls = TOOL_CALLS.map(([tool, args, result]) => `<li class="ag-call" lang="en"><p class="ag-tool"><i></i>${tool}</p><p class="ag-args">${jsonLine(args)}</p><p class="ag-res"><span class="j-p">→ </span>${jsonLine(result)}</p></li>`).join('');
  return frame(
    'mock-agent',
    label,
    `<ol class="ag" aria-hidden="true"><li class="ag-you"><p class="ag-who">${esc(chat.you)}</p><p class="ag-bubble">${esc(chat.ask)}</p></li>${calls}<li class="ag-reply"><p class="ag-who">${esc(chat.agent)}</p><p>${inline(chat.reply)}</p></li></ol>`,
  );
}

// ---- the hero's shelf ------------------------------------------------------

// Five sleeves, bottoms on y 400 of a 1120 x 440 picture, overlapping by 36
// and centered, in z order; the class sh-<n> carries the entrance delay
// (distance from the center). A sleeve left of the center shows its left
// part and one right of it its right part, so each sits where its type and
// main shape show.
const SHELF = [
  ['orchard', 190, 157, 1, 2],
  ['summer', 190, 963, 1, 2],
  ['tidal', 230, 331, 2, 1],
  ['tide', 230, 789, 2, 1],
  ['glass', 300, 560, 3, 0],
];
const SHELF_PHONE = [
  ['tidal', 150, 100, 1],
  ['tide', 150, 260, 1],
  ['glass', 200, 180, 0],
];

function sleeve(id, key, size, cx, bottom, delay) {
  const x = cx - size / 2;
  const y = bottom - size;
  return `<g class="sl sh-${delay}"><g filter="url(#shelf-shadow)"><g clip-path="url(#${id})"><use href="#cv-${key}" x="${x}" y="${y}" width="${size}" height="${size}"/></g><path d="M${x + 1.5} ${y + 0.5}H${x + size - 1.5}" stroke="#ffffff" stroke-opacity=".22" stroke-width="1"/><path d="M${x + 1.5} ${bottom - 0.5}H${x + size - 1.5}" stroke="#000000" stroke-opacity=".18" stroke-width="1"/></g></g>`;
}

// The filters the shelf uses live in the cover sprite, which is never hidden.
export const SHELF_DEFS =
  '<filter id="shelf-shadow" x="-30%" y="-20%" width="160%" height="160%" color-interpolation-filters="sRGB"><feDropShadow class="d1" dx="0" dy="2" stdDeviation="2" flood-color="#172723" flood-opacity=".16"/><feDropShadow class="d2" dx="0" dy="16" stdDeviation="12" flood-color="#172723" flood-opacity=".16"/></filter>' +
  '<filter id="shelf-floor" x="-10%" y="-200%" width="120%" height="500%"><feGaussianBlur stdDeviation="6"/></filter>';

export function shelf(label) {
  const clips = (list, p) => list.map(([key, size, cx], i) => `<clipPath id="${p}${i}"><rect x="${cx - size / 2}" y="${(p === 'shw' ? 400 : 260) - size}" width="${size}" height="${size}" rx="3"/></clipPath>`).join('');
  const wide = `<svg class="shelf shelf-wide" viewBox="0 0 1120 440" aria-hidden="true" focusable="false"><defs>${clips(SHELF, 'shw')}</defs><ellipse class="shelf-floor" cx="560" cy="404" rx="520" ry="10" fill="#172723" fill-opacity=".1" filter="url(#shelf-floor)"/><path class="shelf-line" d="M0 401.5H1120" stroke-width="1"/>${SHELF.map(([key, size, cx, , d], i) => sleeve(`shw${i}`, key, size, cx, 400, d)).join('')}</svg>`;
  const phone = `<svg class="shelf shelf-phone" viewBox="0 44 360 236" aria-hidden="true" focusable="false"><defs>${clips(SHELF_PHONE, 'shp')}</defs><ellipse class="shelf-floor" cx="180" cy="264" rx="170" ry="7" fill="#172723" fill-opacity=".1" filter="url(#shelf-floor)"/><path class="shelf-line" d="M0 261.5H360" stroke-width="1"/>${SHELF_PHONE.map(([key, size, cx, d], i) => sleeve(`shp${i}`, key, size, cx, 260, d)).join('')}</svg>`;
  const chip = `<div class="file-chip" aria-hidden="true"><span class="file-chip-icon">${sized(ICON.note, 18)}</span><span class="file-chip-t"><b>${esc(GLASS_FILE)}</b><span>${GLASS_QUALITY}</span></span></div>`;
  return `<figure class="shelf-fig" role="img" aria-label="${esc(label)}">${wide}${phone}${chip}</figure>`;
}

export { ALBUMS };
