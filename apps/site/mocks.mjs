// The page's hand-made pictures: the hero's waveform, the products' screens
// and the small examples beside the specs. Each screen is a simplified copy of
// a real one, drawn in HTML and CSS over made-up music:
//
//   desktop  apps/desktop/internal/golden/testdata/{queue,library,album}-*.png
//   console  the console's New download, Overview, Logs and Settings › Hooks
//   tui      apps/amdl/internal/tui/testdata/frame-100.ansi and tui/theme.go
//
// The apps are in English, so their words are too, in both languages of the
// page; content/<lang>.json holds what the page says about them. None of this
// runs app code. site.js gives a few of them their one button.
import { esc } from './blocks.mjs';

// The made-up catalog, from internal/demo/catalog.go. site.css draws each
// cover as that file's cover() does: a diagonal gradient with a disc (.cv-*).
export const ALBUMS = {
  glass: { title: 'Glass Weather', artist: 'Mira Solenne', year: 2025, id: '920001', tracks: 12 },
  tidal: { title: 'Tidal Rooms', artist: 'Mira Solenne', year: 2022, id: '920002', tracks: 10 },
  tide: { title: 'Low Tide Signals', artist: 'Nightjar Coast', year: 2026, id: '920003', tracks: 11 },
  salt: { title: 'Salt & Static', artist: 'Nightjar Coast', year: 2023, id: '920004', tracks: 9 },
  orchard: { title: 'Orchard Lights', artist: 'The Velvet Orchard', year: 2024, id: '920005', tracks: 8 },
  shore: { title: '海边的耳机', artist: '青柠电台', year: 2023, id: '920008', tracks: 9 },
};
const GLASS_TRACKS = ['Glass Weather', 'Paper Satellites', 'Slow Lightning', 'Northbound', 'Halo Static', 'Small Hours', 'Weathervane'];
export const albumUrl = (k) => `https://music.apple.com/us/album/${ALBUMS[k].title.toLowerCase().replace(/[^a-z0-9]+/g, '-')}/${ALBUMS[k].id}`;
// Where the default library_path_format puts a track of Glass Weather.
export const glassPath = (i) => `library/Mira Solenne/Glass Weather/${String(i + 1).padStart(2, '0')}. ${GLASS_TRACKS[i]}.m4a`;
const GLASS_DIR = '/volume1/music/library/Mira Solenne/Glass Weather/';

const cover = (key, cls = '') => `<span class="cv cv-${key}${cls ? ` ${cls}` : ''}"></span>`;
const times = (n, fn) => Array.from({ length: n }, (_, i) => fn(i)).join('');

// The apps' icons, drawn after Lucide as both apps use them.
const ic = (body) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${body}</svg>`;
const I = {
  panel: ic('<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>'),
  home: ic('<path d="M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6h-6v6H4a1 1 0 0 1-1-1z"/>'),
  list: ic('<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>'),
  books: ic('<path d="M4 4h4v16H4zM10 4h4v16h-4z"/><path d="m16 4.5 3.5-.9 2.5 15.6-3.5.9z"/>'),
  server: ic('<rect x="3" y="4" width="18" height="7" rx="2"/><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M7 7.5h.01M7 16.5h.01"/>'),
  search: ic('<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>'),
  back: ic('<path d="m15 18-6-6 6-6"/>'),
  check: ic('<path d="M20 6 9 17l-5-5"/>'),
  clock: ic('<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>'),
  down: ic('<path d="M12 5v14M19 12l-7 7-7-7"/>'),
  dl: ic('<path d="M12 4v11M7 10l5 5 5-5M5 20h14"/>'),
  more: ic('<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>'),
  sync: ic('<path d="M21 12a9 9 0 0 1-15 6.7L3 16"/><path d="M3 12a9 9 0 0 1 15-6.7L21 8"/><path d="M21 3v5h-5M3 21v-5h5"/>'),
  grid: ic('<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>'),
  pulse: ic('<path d="M3 12h4l3-8 4 16 3-8h4"/>'),
  sliders: ic('<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>'),
  file: ic('<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5M9 13h6M9 17h6M9 9h1"/>'),
  note: ic('<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>'),
  plus: ic('<path d="M12 5v14M5 12h14"/>'),
  monitor: ic('<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>'),
  x: ic('<path d="M18 6 6 18M6 6l12 12"/>'),
  bolt: ic('<path d="M13 2 4 14h7l-1 8 9-12h-7z"/>'),
  chevDown: ic('<path d="m6 9 6 6 6-6"/>'),
  chevRight: ic('<path d="m9 6 6 6-6 6"/>'),
  xCircle: ic('<circle cx="12" cy="12" r="9"/><path d="m15 9-6 6M9 9l6 6"/>'),
  okCircle: ic('<circle cx="12" cy="12" r="9"/><path d="m8.5 12.5 2.5 2.5 4.5-5"/>'),
  pause: ic('<path d="M9 5v14M15 5v14"/>'),
  minus: ic('<circle cx="12" cy="12" r="9"/><path d="M8 12h8"/>'),
  lock: ic('<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>'),
  refresh: ic('<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>'),
  bars: '<svg viewBox="0 0 12 12" aria-hidden="true" focusable="false"><rect x="1" y="7" width="2.5" height="4" rx="1"/><rect x="4.75" y="3" width="2.5" height="8" rx="1"/><rect x="8.5" y="8" width="2.5" height="3" rx="1"/></svg>',
};

// ---- hero -------------------------------------------------------------------

// One track's waveform as bars, cut into the four stages. The heights are a
// fixed sum of sines, so every build draws the same picture.
export function waveform() {
  const N = 112;
  const W = 8;
  const groups = [[], [], [], []];
  for (let i = 0; i < N; i++) {
    const env = 0.35 + 0.65 * Math.sin((Math.PI * (i + 4)) / (N + 8)) ** 0.6;
    const v = Math.abs(0.55 * Math.sin(i * 0.47) + 0.3 * Math.sin(i * 0.13 + 1.7) + 0.15 * Math.sin(i * 1.31));
    const h = Math.max(6, Math.round(env * (14 + 92 * v)));
    groups[Math.floor((i * 4) / N)].push(`<rect x="${i * W + 1}" y="${60 - h / 2}" width="${W - 3}" height="${h}" rx="2"/>`);
  }
  return `<svg class="wave" viewBox="0 0 ${N * W} 120" preserveAspectRatio="none" aria-hidden="true" focusable="false">${groups
    .map((g, i) => `<g class="st-${i + 1}">${g.join('')}</g>`)
    .join('')}</svg>`;
}

export function heroStrip(h) {
  return `<figure class="strip" role="img" aria-label="${esc(h.strip)}">
<div class="strip-wave">${waveform()}<span class="playhead" aria-hidden="true"></span></div>
<ol class="stages" aria-hidden="true">${h.stages.map((s, i) => `<li class="st-${i + 1}"><span>0${i + 1}</span>${esc(s)}</li>`).join('')}</ol>
<div class="strip-file" aria-hidden="true">${I.note}<span><b>01. Glass Weather.m4a</b><small>ALAC · 24-bit/96 kHz</small></span></div>
</figure>`;
}

// ---- desktop app ------------------------------------------------------------

// The desktop's progress bar: tracks done, then the ones in progress striped,
// the rest, and a dot per track to come (up to five) at the end.
const dkBar = (done, run, total, cls = '') =>
  `<span class="dk-bar${cls}"><i class="f${Math.max(done, 0.6)}"></i><i class="dk-run f${Math.max(run, 0.01)}"></i><i class="dk-rest f${Math.max(total - done - run, 1)}"></i><b>${times(Math.min(5, total - done), () => '<i></i>')}</b></span>`;

function dkSide(page) {
  const nav = [
    ['home', I.home, 'Home', ''],
    ['queue', I.list, 'Queue', '<em>3</em>'],
    ['library', I.books, 'Library', ''],
  ]
    .map(([p, icon, name, extra]) => `<span${p === page ? ' class="on"' : ''}>${icon}${name}${extra}</span>`)
    .join('');
  return `<div class="dk-side" aria-hidden="true"><span class="dk-toggle">${I.panel}</span><div class="dk-nav">${nav}</div>
<div class="dk-now">${cover('tide')}<span><b>Low Tide Signals</b><small>1 of 11</small>${dkBar(1, 1, 11, ' dk-bar-mini')}</span></div>
<div class="dk-conn"><span class="dk-conn-ic">${I.server}</span><span><b>nas:18570</b><small>Live</small></span></div></div>`;
}

const dkFrame = (page, label, main, interactive = false) =>
  `<div class="dk dk-${page}" ${interactive ? 'role="group"' : 'role="img"'} aria-label="${esc(label)}">${dkSide(page === 'album' ? 'home' : page)}<div class="dk-main"${interactive ? '' : ' aria-hidden="true"'}>${main}</div></div>`;

function dkQueue(label) {
  const row = (k, sub, done, run, pct, chip, cls = '') =>
    `<li${cls ? ` class="${cls}"` : ''}>${cover(k)}<span class="dk-rt"><b>${esc(ALBUMS[k].title)}</b><small>${esc(ALBUMS[k].artist)} · Album</small></span><span class="dk-rp">${dkBar(done, run, ALBUMS[k].tracks)}<small><span>${sub}</span><span>${pct}</span></small></span>${chip}<span class="dk-dots">${I.more}</span></li>`;
  const dl = `<span class="dk-chip dk-chip-dl">${I.down}Downloading</span>`;
  const main = `<div class="dk-hero" aria-hidden="true">
<div class="dk-art">${cover('tide', 'dk-cover')}<span class="dk-next">${cover('orchard')}${cover('shore')}</span></div>
<div class="dk-id"><p class="dk-title">Low Tide Signals</p><p class="dk-artist">Nightjar Coast <span class="dk-tag dk-gilt">ALAC 24/48</span><span class="dk-tag">11 tracks</span></p>${dkBar(1, 2, 11, ' dk-bar-hero')}<p class="dk-sub">1 of 11 tracks · downloading 2</p></div>
<div class="dk-big"><p class="dk-pct">33<small>%</small></p></div>
</div>
<div class="dk-btns"><button type="button" class="dk-btn" data-pause data-alt="Resume">Pause</button><span class="dk-btn dk-btn-solid" aria-hidden="true">Details</span></div>
<div class="dk-qhead" aria-hidden="true"><p class="dk-h">Queue</p><span class="dk-seg"><i class="on">All</i><i>Active · 3</i><i>Done · 2</i><i class="bad">Failed · 1</i></span></div>
<p class="dk-group" aria-hidden="true">Up next <em>1</em></p>
<ul class="dk-rows" aria-hidden="true">
${row('tide', '1/11 · 33%', 1, 1, 'ALAC 24/48', dl, 'run')}
${row('glass', '5/12 · 58%', 5, 2, 'ALAC 24/48', dl, 'run')}
${row('salt', '0/9', 0, 0, '', `<span class="dk-chip">${I.clock}Queued</span>`)}
</ul>
<p class="dk-group dk-earlier" aria-hidden="true"><span>Earlier</span><span>Newest first</span></p>`;
  return dkFrame('queue', label, main, true);
}

function dkLibrary(label) {
  const tile = (k, mark) =>
    `<li>${cover(k)}${mark ? `<span class="dk-mark dk-mark-${mark}">${mark === 'ok' ? I.check : I.down}</span>` : ''}<b>${esc(ALBUMS[k].title)}</b><small>${esc(ALBUMS[k].artist)}</small></li>`;
  const main = `<div class="dk-lhead"><p class="dk-h">Library</p><span class="dk-synced">Synced just now</span><span class="dk-btn dk-btn-line">${I.sync}Sync now</span></div>
<span class="dk-seg"><i class="on">Albums</i><i>Songs</i><i>Playlists</i></span>
<ul class="dk-grid">${tile('glass', 'dl')}${tile('orchard', 'ok')}${tile('tide', 'dl')}${tile('shore', 'ok')}${tile('salt')}</ul>`;
  return dkFrame('library', label, main);
}

function dkAlbum(label) {
  const sizes = ['67.2', '73.7', '80.1', '86.6', '58.3', '61.9', '70.4'];
  const lens = ['3:52', '4:14', '4:36', '4:59', '3:21', '3:44', '4:08'];
  const rows = GLASS_TRACKS.map((t, i) => {
    const saved = i < 5;
    return `<li${saved ? '' : ' class="wait"'}><span class="dk-n">${i + 1}</span><span class="dk-rt"><b>${esc(t)}</b><small>alac 24/48 · ${sizes[i]} MB</small></span><span class="dk-mb"><i></i><i></i><b><i></i><i></i><i></i></b></span><span class="dk-len">${lens[i]}</span><span class="dk-st">${saved ? `${I.check}Saved` : `${I.clock}Waiting`}</span></li>`;
  }).join('');
  const main = `<div class="dk-top"><span class="dk-crumb">${I.back}Home</span><span class="dk-btn">Pause</span></div>
<div class="dk-ahead">${cover('glass', 'dk-cover')}<div class="dk-aid"><p class="dk-kicker">ALBUM</p><p class="dk-atitle">Glass Weather</p><p class="dk-artist">Mira Solenne <span class="dk-tag dk-gilt">ALAC 24-bit · 48 kHz</span></p>
<dl class="dk-figs"><div><dt>SIZE</dt><dd>453.0 <small>MB</small></dd></div><div><dt>SPEED</dt><dd>—</dd></div><div><dt>LEFT</dt><dd>—</dd></div><div><dt>TRACKS</dt><dd>5 / 12</dd></div></dl>
<span class="dk-steps">${times(12, (i) => `<i${i < 5 ? ' class="ok"' : i < 7 ? ' class="run"' : ''}></i>`)}</span><p class="dk-sub"><span>5 / 12 finished</span><span>58%</span></p></div></div>
<div class="dk-body"><ul class="dk-tracks">${rows}</ul><div class="dk-aside"><p class="dk-kicker">ACTIVITY</p><p class="dk-log"><span>18:27:00</span>Queued</p><div class="dk-saved"><p class="dk-kicker">SAVED TO</p><p>${esc(GLASS_DIR)}</p></div></div></div>`;
  return dkFrame('album', label, main);
}

export const desktopViews = (d) => ({ queue: dkQueue(d.labels.queue), library: dkLibrary(d.labels.library), album: dkAlbum(d.labels.album) });

// ---- web console --------------------------------------------------------------

function cnFrame(page, label, main, { search = true, interactive = false } = {}) {
  const nav = [
    ['overview', I.grid, 'Overview', ''],
    ['activity', I.pulse, 'Activity', '<em>3</em>'],
    ['settings', I.sliders, 'Settings', ''],
  ]
    .map(([p, icon, name, extra]) => `<span${p === page ? ' class="on"' : ''}>${icon}${name}${extra}</span>`)
    .join('');
  const side = `<div class="cn-side" aria-hidden="true"><p class="cn-brand"><span class="cn-mark">${I.note}</span>AMDL</p><span class="cn-newbtn">${I.plus}New download</span><div class="cn-nav">${nav}</div><div class="cn-nav cn-foot"><span${page === 'logs' ? ' class="on"' : ''}>${I.file}Logs</span></div></div>`;
  const top = `<div class="cn-top" aria-hidden="true"><span class="cn-live"></span>${search ? `<span class="cn-find">${I.search}<span>Search or paste a link…</span><kbd>Ctrl K</kbd></span>` : ''}<span class="cn-theme">${I.monitor}</span></div>`;
  return `<div class="cn cn-${page}" ${interactive ? 'role="group"' : 'role="img"'} aria-label="${esc(label)}">${side}<div class="cn-main">${top}<div class="cn-page"${interactive ? '' : ' aria-hidden="true"'}>${main}</div></div></div>`;
}

function cnNew(label, queued) {
  // Search results as the real page lays them out: album tiles with their
  // quality and a round download button, and songs in three columns whose
  // second line says when a song is already queued or in the library.
  const tile = (k, tag, meta, state = '') =>
    `<li${state ? ` class="is-${state}"` : ''}>${cover(k)}${state === 'q' ? `<span class="cn-tq">${I.clock}</span>` : ''}<span class="cn-q">${tag}</span><span class="cn-tdl">${I.dl}</span><b>${esc(ALBUMS[k].title)}</b><small>${esc(ALBUMS[k].artist)} · ${meta}</small></li>`;
  const states = `<small class="cn-state cn-state-q">${I.clock}In the queue now · <u>Open</u></small><small class="cn-state cn-state-lib">${I.okCircle}In your library · <u>Open</u></small>`;
  const song = (t, k, len, state = '') => {
    const action =
      state === 'lib'
        ? `<span class="cn-dl" aria-hidden="true">${I.refresh}</span>`
        : state === 'q'
          ? `<span class="cn-dl is-done" aria-hidden="true">${I.dl}</span>`
          : `<button type="button" class="cn-dl" data-dl data-done="${esc(queued)}" aria-label="Download ${esc(t)}">${I.dl}</button>`;
    return `<li${state ? ` class="is-${state}"` : ''}>${cover(k)}<span class="cn-st"><b>${esc(t)}</b><small>${esc(ALBUMS[k].artist)} · ${esc(ALBUMS[k].title)} · ${len}</small>${states}</span>${action}</li>`;
  };
  const main = `<h3 class="cn-h1" aria-hidden="true">New download</h3>
<div class="cn-glow" aria-hidden="true"><p class="cn-bigsearch">${I.search}<span>mira solenne</span><i></i>${I.x}</p><span class="cn-select">Quality: Settings ${I.chevDown}</span></div>
<p class="cn-h2" aria-hidden="true">Albums<a>Show more</a></p>
<ul class="cn-tiles" aria-hidden="true">${tile('glass', 'Hi-Res Lossless', '2025 · 12 songs', 'q')}${tile('tidal', 'Lossless', '2022 · 10 songs')}</ul>
<p class="cn-h2" aria-hidden="true">Songs<a>Show more</a></p>
<ul class="cn-songs">${song('Paper Satellites', 'glass', '4:14', 'q')}${song('Seaglass', 'tidal', '4:02')}${song('Low Ceilings', 'tidal', '3:58', 'lib')}${song('Slow Lightning', 'glass', '4:36', 'q')}${song('Hollow Bells', 'tidal', '3:47')}${song('Harbor Song', 'tidal', '4:21')}</ul>`;
  return cnFrame('new', label, main, { search: false, interactive: true });
}

function cnOverview(label) {
  const card = (k, done, pct, started) => {
    const a = ALBUMS[k];
    const caps = times(a.tracks, (i) => `<i${i < done ? ' class="on"' : ''}></i>`);
    return `<li class="cn-live-card cv-${k}"><span class="cn-card-art">${cover(k)}</span><span class="cn-card-id"><small class="cn-run">${I.bars}RUNNING</small><b>${esc(a.title)}</b><span class="cn-card-artist">${esc(a.artist)}</span><small>${a.year} · Started ${started}</small></span><span class="cn-caps">${caps}</span><span class="cn-card-pct">${pct}%</span><small class="cn-card-foot">${done ? `<span><i class="on"></i>${done} saved</span>` : ''}<span><i></i>${a.tracks - done} remaining</span></small></li>`;
  };
  const main = `<h3 class="cn-h1">Overview</h3><p class="cn-date">Nov 8</p>
<p class="cn-health">${I.okCircle}All systems healthy <u>Status</u></p>
<p class="cn-h2">Needs attention<small>Last 24 hours</small><a>All failed ›</a></p>
<div class="cn-attn"><p><span class="cn-fail">${I.xCircle}Failed</span><code>us/album/920099</code><small>10s ago</small></p><p>Apple Music has nothing at this link in the US storefront</p></div>
<p class="cn-h2">Live queue<a>Open queue ›</a></p>
<ul class="cn-cards">${card('glass', 7, 58, '21:16')}${card('tide', 1, 13, '21:18')}${card('tidal', 3, 30, '21:19')}<li class="cn-live-card cn-queued"><span class="cn-card-art"><span class="cn-ph">${I.note}</span></span><span class="cn-card-id"><small class="cn-run">${I.clock}QUEUED</small><b>us/album/920004</b><span class="cn-card-artist">Not resolved yet</span></span><span class="cn-caps cn-caps-empty"></span></li></ul>`;
  return cnFrame('overview', label, main);
}

function cnLogs(label) {
  const rows = [
    ['21:15:21.127', 'inf', 'main', 'logging initialized', 'level=info', []],
    ['21:15:21.182', 'inf', 'main', 'amdl backend listening', 'addr=:18080 console=http://nas.local:18080/console/', []],
    ['21:15:21.183', 'wrn', 'accounts', 'account discovery failed; the stored accounts stay', "read the wrapper's accounts: wrapper /status: …", []],
    ['21:16:20.012', 'inf', 'api', 'job queued', 'job_type=album resource_id=920001 storefront=us', ['Glass Weather', 'req 84d198d7']],
    ['21:16:20.013', 'inf', 'jobs', 'job started', 'job_type=album storefront=us', ['Glass Weather']],
    ['21:18:02.540', 'inf', 'api', 'job queued', 'job_type=album resource_id=920003 storefront=us', ['Low Tide Signals', 'req 1c07e2aa']],
    ['21:18:02.541', 'inf', 'jobs', 'job started', 'job_type=album storefront=us', ['Low Tide Signals']],
    ['21:19:40.212', 'wrn', 'jobs', 'operation attempt failed', 'Apple Music has nothing at this link in the US storefront', ['us/album/920099']],
    ['21:19:40.213', 'err', 'jobs', 'job finished', 'Apple Music has nothing at this link in the US storefront', ['us/album/920099']],
    ['21:19:40.214', 'inf', 'jobs', 'job started', 'job_type=album storefront=us', ['Tidal Rooms']],
  ]
    .map(
      ([t, lvl, comp, msg, kv, chips]) =>
        `<li class="lv-${lvl}"><span class="cn-t">${t}</span><span class="cn-lvl"><i></i>${lvl.toUpperCase()}</span><span class="cn-comp">${comp}</span><span class="cn-msg">${esc(msg)} <span>${esc(kv)}</span></span><span class="cn-chips">${chips.map((c) => `<i>${esc(c)}</i>`).join('')}</span></li>`,
    )
    .join('');
  const main = `<h3 class="cn-h1">Logs<span class="cn-filters">${I.sliders}</span></h3>
<p class="cn-logbar"><span class="cn-chip cn-chip-line"><i></i>Debug</span><span class="cn-chip">${I.okCircle}Info</span><span class="cn-chip cn-chip-warn">${I.okCircle}Warn</span><span class="cn-chip cn-chip-err">${I.okCircle}Error</span><span class="cn-select">All components ${I.chevDown}</span><span class="cn-field">${I.search}Search logs</span><span class="cn-on"><i></i>Live updates on</span><span class="cn-chip cn-chip-line cn-pause">${I.pause}Pause</span><span class="cn-switch cn-switch-static"><i></i></span><span class="cn-wrap">Wrap lines</span></p>
<ul class="cn-loglist">${rows}</ul>`;
  return cnFrame('logs', label, main);
}

export const consoleViews = (c) => ({ new: cnNew(c.labels.new, c.queued), overview: cnOverview(c.labels.overview), logs: cnLogs(c.labels.logs) });

// The console's Settings › Hooks before any hook exists: the switch, and the
// presets internal/hookpreset offers, in its groups.
export function hooksPanel(label) {
  const group = (name, items) => `<p class="cn-hg">${name}</p><span class="cn-presets">${items.map((i) => `<i>${i}</i>`).join('')}</span>`;
  return `<div class="cn cn-card-only" role="group" aria-label="${esc(label)}"><div class="cn-hooks">
<p class="cn-hhead" aria-hidden="true"><span class="cn-hic">${I.bolt}</span><b>Hooks</b><span class="cn-pill" data-pill data-on="On">${I.minus}<span>Off</span></span><span class="cn-ref">${I.refresh}</span></p>
<p class="cn-fig" aria-hidden="true">No hooks<i></i></p>
<p class="cn-row cn-row-file" aria-hidden="true"><span>Hooks file</span><code>hooks.yaml</code><span class="cn-pill">${I.lock}At startup</span></p>
<p class="cn-row"><span id="run-hooks">Run hooks</span><button type="button" class="cn-switch" role="switch" aria-checked="false" aria-labelledby="run-hooks" data-switch><i></i></button></p>
<div aria-hidden="true">${group('Notify', ['ntfy', 'Bark', 'Telegram'])}${group('Refresh a library', ['Plex', 'Jellyfin', 'Emby'])}${group('Other', ['Custom'])}</div>
</div></div>`;
}

// ---- terminal UI ---------------------------------------------------------------

// A line of the TUI is runs of text, each in one of its theme's colors
// (tui/theme.go): fg, dim, faint, acc (the card's album color), soft (that
// color faded, a bar still in progress), skip, ok, bad, b1-b4 (the logo's
// bars) and bold. Runs are [class, text].
// The logo's blocks are drawn as boxes, so its bars stay solid in any font.
const blocks = (text) => [...text].map((ch) => (ch === '█' ? '<i class="blk"></i>' : ch === '▄' ? '<i class="blk half"></i>' : esc(ch))).join('');
const run = (cls, text) => (cls ? `<span class="${cls}">${/^b[1-4]$/.test(cls) ? blocks(text) : esc(text)}</span>` : esc(text));
const line = (...runs) => `<span class="tl">${runs.map(([c, t]) => run(c, t)).join('')}</span>`;
const pad = (s, n) => s + ' '.repeat(Math.max(0, n - [...s].length));

export function tuiScreen(label) {
  const albumBar = [
    ['acc', '━━━━'], ['', ' '], ['skip', '━━━━━'], ['', ' '], ['acc', '━━━━'], ['', ' '], ['bad', '━━━━'], ['', ' '],
    ['soft', '╍╍╍╍╍'], ['', ' '], ['soft', '╍╍'], ['faint', '──'], ['', ' '], ['faint', '──── ───── ──── ──── ───── ────'],
  ];
  const track = (n, title, bar, state, stateCls = 'dim') => line(['dim', `  ${n} `], ['fg', pad(title, 24)], ['', '  '], ...bar, ['', '  '], [stateCls, state]);
  const tail = (k) => [['faint', '─ '.repeat(k).trimEnd()]];
  const full = `━━━━━━ ${'━'.repeat(32)} `;
  const hit = (sel, mark, type, name, meta, extra = '') => {
    const s = sel ? ' sel' : '';
    return line(
      ['', '  '],
      [`b1${s}`, sel ? '▸ ' : '  '],
      [(mark === '●' ? 'b1' : 'dim') + s, `${mark} `],
      [`dim${s}`, pad(type, 9)],
      [(sel ? 'bold' : 'fg') + s, pad(name, 30)],
      [`dim${s}`, pad(`  ${meta}`, 41)],
      [`ok${s}`, extra],
    );
  };
  const box = '─'.repeat(98);
  const lines = [
    line(['', '   '], ['b2', '██'], ['', '    '], ['b4', '▄▄'], ['', '   '], ['bold', 'amdl'], ['dim', ' 2.0.0']),
    line(['b1', '██'], ['', ' '], ['b2', '██'], ['', ' '], ['b3', '▄▄'], ['', ' '], ['b4', '██'], ['', '   '], ['dim', 'nas · nas.local:18570 · wrapper ready (us)']),
    line(['b1', '██'], ['', ' '], ['b2', '██'], ['', ' '], ['b3', '██'], ['', ' '], ['b4', '██'], ['', '   '], ['dim', '/volume1/music · 1.2 TB free']),
    line(),
    line(['dim', '  128 saved · '], ['bad', '2 failed']),
    line(),
    line(['b1', '> '], ['fg', 'mira solenne']),
    line(['acc', '◆ '], ['bold', 'Glass Weather'], ['dim', ' · Mira Solenne #1'], ['', ' '.repeat(57)], ['dim', 'ALAC 24/96']),
    line(['fg', '  3/12  '], ...albumBar, ['dim', '  46%  ~1 min']),
    track('03', 'Slow Lightning', [['acc', `━━━━━━ ${'━'.repeat(20)}`], ['faint', `${'─'.repeat(12)} `], ...tail(5)], 'Download 62%'),
    track('05', 'Halo Static', [['acc', `${full}╸ `], ...tail(4)], 'Decrypt 35%'),
    track('06', 'Small Hours', [['acc', full], ['bad', '━ '], ...tail(4)], '✗ decrypt failed', 'bad'),
    line(['b1', '◆ '], ['bold', 'Mira Solenne Essentials'], ['dim', ' #2'], ['', ' '.repeat(66)], ['dim', 'queued']),
    line(['faint', `╭${box}╮`]),
    line(['faint', '│ '], ['b1', '> '], ['dim', pad('link, search or /', 95)], ['faint', '│']),
    line(['faint', `╰${box}╯`]),
    line(['fg', '  search · mira solenne   '], ['bold u', 'all'], ['dim', '  album  song  playlist  artist']),
    hit(true, '○', 'album', 'Glass Weather', 'Mira Solenne · 2025 · 12 tracks', '  ✓ in library'),
    hit(false, '●', 'album', 'Tidal Rooms', 'Mira Solenne · 2022 · 10 tracks'),
    hit(false, '○', 'song', 'Paper Satellites', 'Mira Solenne · 3:41'),
    hit(false, '○', 'playlist', 'Mira Solenne Essentials', 'Apple Music Pop'),
    hit(false, '○', 'artist', 'Mira Solenne', ''),
    line(['ok', '  ●'], ['dim', ' 1 running · 1 queued'], ['', ' '.repeat(30)], ['dim', 'enter downloads · space selects · tab filters']),
  ];
  // A phone gets the TUI's own 46-column layout (frame-46.ansi), not a
  // shrunken 100-column one.
  const nbox = '─'.repeat(44);
  const nhit = (sel, mark, type, name) => {
    const s = sel ? ' sel' : '';
    return line(['', '  '], [`b1${s}`, sel ? '▸ ' : '  '], [(mark === '●' ? 'b1' : 'dim') + s, `${mark} `], [`dim${s}`, pad(type, 9)], [(sel ? 'bold' : 'fg') + s, pad(name, 31)]);
  };
  const narrow = [
    lines[0],
    line(['b1', '██'], ['', ' '], ['b2', '██'], ['', ' '], ['b3', '▄▄'], ['', ' '], ['b4', '██'], ['', '   '], ['dim', 'nas · nas.local:18570 · wrapper…']),
    lines[2],
    line(),
    lines[4],
    line(),
    lines[6],
    line(['acc', '◆ '], ['bold', 'Glass Weather'], ['dim', ' #1']),
    line(['fg', '  3/12  '], ['acc', '━━━'], ['skip', '━━'], ['acc', '━━━'], ['bad', '━━━'], ['soft', '╍╍╍╍'], ['faint', '─'.repeat(18)], ['dim', '  46%']),
    line(['b1', '◆ '], ['bold', 'Mira Solenne Essentials'], ['dim', ' #2'], ['', ' '.repeat(12)], ['dim', 'queued']),
    line(['faint', `╭${nbox}╮`]),
    line(['faint', '│ '], ['b1', '> '], ['dim', pad('link, search or /', 41)], ['faint', '│']),
    line(['faint', `╰${nbox}╯`]),
    line(['fg', '  search · mira solenne']),
    nhit(true, '○', 'album', 'Glass Weather'),
    nhit(false, '●', 'album', 'Tidal Rooms'),
    nhit(false, '○', 'song', 'Paper Satellites'),
    nhit(false, '○', 'playlist', 'Mira Solenne Essentials'),
    nhit(false, '○', 'artist', 'Mira Solenne'),
    line(['ok', '  ●'], ['dim', ' 1 running · 1 queued'], ['', ' '.repeat(6)], ['dim', 'enter downloads']),
  ];
  return term('tui', label, '~ — amdl', `<pre class="tui tui-wide">${lines.join('')}</pre><pre class="tui tui-narrow">${narrow.join('')}</pre>`);
}

// A terminal window around a screen: its title bar and a body that scrolls
// sideways on a screen too narrow for its columns.
const term = (kind, label, title, body) =>
  `<div class="term term-${kind}" role="img" aria-label="${esc(label)}"><p class="term-bar" aria-hidden="true"><i></i><i></i><i></i><span>${esc(title)}</span></p><div class="term-body" aria-hidden="true">${body}</div></div>`;

// ---- command line ------------------------------------------------------------------

// A session of real commands (docs/cli.md): a search piped into jq, a download
// streamed as its events (each track's phase is its stage), and a job list.
export function cliSession(label) {
  const p = (cmd) => `<span class="tl"><span class="b1">$ </span><span class="fg">${esc(cmd)}</span></span>`;
  const o = (text, cls = 'dim') => `<span class="tl"><span class="${cls}">${esc(text)}</span></span>`;
  const ev = (type, phase) =>
    `<span class="tl"><span class="dim">{</span><span class="k">"type"</span><span class="dim">:</span><span class="s">"${type}"</span>${phase ? `<span class="dim">,</span><span class="k">"phase"</span><span class="dim">:</span><span class="s ph-${phase}">"${phase}"</span>` : ''}<span class="dim">,…}</span></span>`;
  const url = albumUrl('glass');
  const lines = [
    p(`amdl search "mira solenne" --types albums --limit 2 | jq -r '.albums.items[].url'`),
    o(url, 'fg'),
    o(albumUrl('tidal'), 'fg'),
    p(`amdl download ${url} --watch`),
    o('-> nas (http://nas.local:18080)'),
    ev('item_progress', 'downloading'),
    ev('item_progress', 'decrypting'),
    ev('item_progress', 'remuxing'),
    ev('item_progress', 'tagging'),
    ev('item_completed'),
    o('…'),
    `<span class="tl"><span class="dim">{</span><span class="k">"type"</span><span class="dim">:</span><span class="s">"job_snapshot"</span><span class="dim">,</span><span class="k">"job"</span><span class="dim">:{</span><span class="k">"status"</span><span class="dim">:</span><span class="s">"completed"</span><span class="dim">,…}}</span></span>`,
    p('amdl jobs list --status failed --fields id,title,status'),
    o('{'),
    `<span class="tl"><span class="k">  "downloads"</span><span class="dim">: [</span></span>`,
    `<span class="tl"><span class="dim">    { </span><span class="k">"id"</span><span class="dim">: </span><span class="s">"job_4d1e…"</span><span class="dim">, </span><span class="k">"title"</span><span class="dim">: </span><span class="s">"Low Tide Signals"</span><span class="dim">, </span><span class="k">"status"</span><span class="dim">: </span><span class="s">"failed"</span><span class="dim"> }</span></span>`,
    o('  ], …'),
    o('}'),
  ];
  return term('cli', label, '~ — zsh', `<pre class="tui">${lines.join('')}</pre>`);
}

// ---- MCP ------------------------------------------------------------------------

// Every tool amdl mcp serves (internal/capability#All), grouped as the page
// shows them; a test checks the list against that file.
export const MCP_TOOLS = {
  catalog: ['search_catalog', 'list_artist_albums', 'get_album', 'get_playlist', 'lookup_link', 'probe_quality'],
  jobs: ['start_download', 'list_jobs', 'get_job', 'wait_for_job', 'cancel_job', 'retry_job', 'move_job', 'delete_job'],
  library: ['list_library', 'list_library_playlists', 'get_resource', 'get_library_playlist', 'get_library_summary', 'download_resource', 'reset_resource', 'pause_resource', 'resume_resource', 'delete_resource', 'check_library', 'get_library_check'],
  apple: ['list_apple_library', 'list_apple_library_playlist_tracks', 'sync_apple_library', 'get_apple_library_status'],
  system: ['get_status', 'get_logs', 'get_config', 'set_config', 'list_accounts', 'update_account', 'delete_account'],
};

// A JSON value as one highlighted line: keys, strings and numbers.
const js = (value) =>
  esc(JSON.stringify(value).replace(/"…"/g, '…'))
    .replace(/(&quot;(?:[^&]|&(?!quot;))*?&quot;)(:)?|(\b\d+\b)/g, (m, str, colon, num) => (str ? (colon ? `<span class="k">${str}</span>:` : `<span class="s">${str}</span>`) : `<span class="n">${num}</span>`))
    .replace(/[{}[\],]/g, (c) => `<span class="dim">${c}</span>`);

export function agentChat(m, inline) {
  const url = albumUrl('glass');
  const call = (tool, args, result) =>
    `<li class="mc-call"><p class="mc-tool"><span class="mc-dot"></span><b>${tool}</b></p><code class="mc-args">${js(args)}</code><code class="mc-out"><span class="dim">→ </span>${js(result)}</code></li>`;
  return `<div class="chat" role="img" aria-label="${esc(m.label)}"><ol aria-hidden="true">
<li class="mc-you"><p class="mc-who">${esc(m.you)}</p><p>${esc(m.ask)}</p></li>
${call('search_catalog', { term: 'Mira Solenne', types: ['albums'], limit: 3 }, { albums: { items: [{ name: 'Glass Weather', release_date: '2025-…', url: `…/${ALBUMS.glass.id}` }, { name: 'Tidal Rooms', release_date: '2022-…', url: `…/${ALBUMS.tidal.id}` }] } })}
${call('start_download', { urls: [url], codec_priority: ['ec3'] }, { accepted: [{ status: 'accepted', job_id: 'job_9b2c…', resource: { kind: 'album', id: ALBUMS.glass.id } }], rejected: [] })}
${call('wait_for_job', { job_id: 'job_9b2c…', timeout_s: 300 }, { done: true, job: { status: 'completed', album_progress: { total: 12, done: 12, failed: 0, percent: 100 }, tracks: [{ index: 1, quality: 'Atmos', output_path: `${GLASS_DIR}01. Glass Weather.m4a` }, '…'] } })}
<li class="mc-agent"><p class="mc-who">${esc(m.agent)}</p><p>${inline(m.reply)}</p></li>
</ol></div>`;
}

export const mcpConfig = () =>
  `{ "mcpServers": { "amdl": { "command": "/Users/me/.local/bin/amdl", "args": ["mcp", "--server", "nas"] } } }`;

// ---- the specs' examples ----------------------------------------------------

export function specDemo(key, s) {
  switch (key) {
    case 'formats':
      return `<div class="chain"><code><span class="c-k">quality_priority</span>: [alac, ec3, aac]</code><ol aria-hidden="true"><li>alac</li><li>ec3</li><li>aac</li><li class="floor">aac-lc</li></ol></div>`;
    case 'records': {
      const cells = Array.from({ length: 12 }, (_, i) => (i === 7 ? 'wait' : i === 10 ? 'bad' : 'ok'));
      const mark = { ok: I.check, wait: I.clock, bad: I.x };
      return `<ol class="cells" role="img" aria-label="${esc(s.records.label)}">${cells.map((c, i) => `<li class="is-${c}"><span>${i + 1}</span>${mark[c]}</li>`).join('')}</ol>`;
    }
    case 'library':
      return `<div class="tpl"><code>library/<b>{AlbumArtist}</b>/<b>{AlbumName}</b>/<b>{SongNumber:02d}</b>. <b>{SongName}</b></code><code class="tpl-out">${esc(glassPath(0))}</code></div>`;
    case 'accounts':
      return `<div class="regions"><code>music.apple.com/<b>jp</b>/album/…</code><span aria-hidden="true"><i>us</i><i class="on">jp</i><i>cn</i></span></div>`;
    case 'wrapper':
      return `<div class="regions"><code>$ amdl wrapper install</code><span aria-hidden="true"><i class="on">native</i><i>qemu</i></span></div>`;
    case 'lyrics':
      return `<code class="lrc">[00:41.20] <span>&lt;00:41.20&gt;</span>Paper <span>&lt;00:41.86&gt;</span><b>satellites</b></code>`;
    case 'sync':
      return `<code class="yaml"><span class="c-k">library_sync</span>:<br>  <span class="c-k">enabled</span>: true<br>  <span class="c-k">interval_minutes</span>: 15</code>`;
    default:
      throw new Error(`no example for spec ${key}`);
  }
}
