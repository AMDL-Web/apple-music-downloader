// The page's hand-made pictures: the hero's waveform, the three product views
// (Search, Queue, Library) and the small examples beside the specs. All of
// them are plain HTML and CSS over made-up music, drawn after the console's
// and the desktop app's screens and named as they name things; none runs app
// code. site.js gives each product view its one button.
import { ICON, esc } from './blocks.mjs';

// The made-up catalog, from internal/demo/catalog.go: names and the two
// colors of each cover, which site.css draws as a color field (.cv-*).
export const ALBUMS = {
  glass: { title: 'Glass Weather', artist: 'Mira Solenne' },
  tidal: { title: 'Tidal Rooms', artist: 'Mira Solenne' },
  tide: { title: 'Low Tide Signals', artist: 'Nightjar Coast' },
  orchard: { title: 'Orchard Lights', artist: 'The Velvet Orchard' },
  bloom: { title: 'Late Bloom', artist: 'The Velvet Orchard' },
};
const GLASS_TRACKS = ['Glass Weather', 'Paper Satellites', 'Slow Lightning', 'Northbound', 'Halo Static'];

const cover = (key, cls = '') => `<span class="cover cv-${key}${cls ? ` ${cls}` : ''}" aria-hidden="true"></span>`;
const fill = (template, values) => template.replace(/\{(\w+)\}/g, (m, k) => (k in values ? String(values[k]) : m));
// The path the default library_path_format gives a track of Glass Weather.
export const glassPath = (i) => `library/Mira Solenne/Glass Weather/${String(i + 1).padStart(2, '0')}. ${GLASS_TRACKS[i]}.m4a`;
const sr = (text) => `<span class="sr">${esc(text)}</span>`;

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
<div class="strip-file" aria-hidden="true">${ICON.note}<span><b>01. Glass Weather.m4a</b><small>ALAC · 24-bit/96 kHz</small></span></div>
</figure>`;
}

// ---- the product views ------------------------------------------------------

// A frame drawn after the console's: a rail of page icons, the page's title
// and its content. `page` picks the rail's current icon.
function frame(page, label, title, head, body) {
  const rail = [
    ['search', ICON.plus],
    ['queue', ICON.queue],
    ['library', ICON.library],
    ['settings', ICON.gear],
  ]
    .map(([p, icon]) => `<span${p === page ? ' class="on"' : ''}>${icon}</span>`)
    .join('');
  return `<div class="app app-${page}" role="group" aria-label="${esc(label)}"><div class="app-rail" aria-hidden="true"><span class="app-logo"><i></i><i></i><i></i><i></i></span>${rail}</div><div class="app-page"><div class="app-head"><p class="app-title">${esc(title)}</p>${head}</div>${body}</div></div>`;
}

const tags = (list) => `<span class="fmt">${list.map((t) => `<i>${esc(t)}</i>`).join('')}</span>`;
const dl = (s, what) => `<button class="dl" type="button" data-dl aria-label="${esc(`${s.download}: ${what}`)}">${ICON.download}<span>${esc(s.download)}</span></button>`;

function searchView(s) {
  const [hires, lossless, atmos] = s.tags;
  const group = (name, rows, cls = '') => `<section class="hits ${cls}"><p class="hits-name">${esc(name)}</p><ul>${rows.join('')}</ul></section>`;
  const albums = [
    ['glass', s.albumMeta[0], [hires, atmos]],
    ['tidal', s.albumMeta[1], [lossless]],
  ].map(
    ([k, meta, t]) =>
      `<li class="hit">${cover(k)}<span class="hit-text"><b>${esc(ALBUMS[k].title)}</b><span>${esc(ALBUMS[k].artist)} · ${esc(meta)}</span>${tags(t)}</span>${dl(s, ALBUMS[k].title)}</li>`,
  );
  const songs = [
    ['Paper Satellites', '3:41', [hires]],
    ['Northbound', '4:05', [hires, atmos]],
  ].map(
    ([t, len, f]) =>
      `<li class="hit hit-song">${cover('glass', 'cover-sm')}<span class="hit-text"><b>${esc(t)}</b><span>${esc(ALBUMS.glass.title)} · ${len}</span>${tags(f)}</span>${dl(s, t)}</li>`,
  );
  const artist = `<li class="hit">${cover('glass', 'cover-round')}<span class="hit-text"><b>Mira Solenne</b><span>${esc(s.artistMeta)}</span></span></li>`;
  const playlist = `<li class="hit">${cover('tidal', 'cover-sm')}<span class="hit-text"><b>${esc(s.playlist)}</b><span>${esc(s.playlistMeta)}</span></span>${dl(s, s.playlist)}</li>`;
  const head = `<span class="app-chip">${esc(s.preset)}</span>`;
  const body = `<div class="compose">${ICON.search}<span class="compose-q">${esc(s.query)}</span><span class="compose-x" aria-hidden="true">${ICON.cross}</span></div>
<div class="hits-grid" data-queued="${esc(s.queued)}"><div>${group(s.groups.albums, albums)}${group(s.groups.songs, songs)}</div><div>${group(s.groups.artists, [artist])}${group(s.groups.playlists, [playlist])}</div></div>`;
  return frame('search', s.label, s.page, head, body);
}

function queueView(q) {
  const counts = ['', '3', '', '1'];
  const head = `<span class="app-tabs">${q.tabs.map((t, i) => `<span${i === 0 ? ' class="on"' : ''}>${esc(t)}${counts[i] ? ` <em>${counts[i]}</em>` : ''}</span>`).join('')}</span>`;
  const pill = (kind, text, icon) => `<span class="pill pill-${kind}">${icon}<span>${esc(text)}</span></span>`;
  const row = (cls, key, sub, right, extra = '') =>
    `<li class="q-row ${cls}">${extra}${cover(key)}<span class="q-text"><b>${esc(ALBUMS[key].title)}</b><span>${sub}</span></span>${right}</li>`;
  const grip = `<span class="grip" title="${esc(q.drag)}" aria-hidden="true">${ICON.grip}</span>`;
  const run = `<li class="q-row is-run" data-run data-stages="${esc(JSON.stringify(q.stages))}" data-track="${esc(q.track)}">${cover('glass')}<span class="q-text"><b>${esc(ALBUMS.glass.title)}</b><span>${esc(ALBUMS.glass.artist)} · <span data-track-text>${esc(fill(q.track, { n: 8, total: 12 }))}</span></span><span class="q-bar" aria-hidden="true"><i data-bar class="w-60"></i></span></span><span class="pill pill-run" data-stage="decrypting">${ICON.download}${ICON.lock}${ICON.film}${ICON.tag}<span data-stage-text>${esc(q.stages.decrypting)}</span></span></li>`;
  const body = `<ul class="q">
${run}
${row('is-wait', 'tidal', esc(ALBUMS.tidal.artist), pill('wait', q.queued, ICON.clock), grip)}
${row('is-wait', 'orchard', esc(ALBUMS.orchard.artist), pill('wait', q.queued, ICON.clock), grip)}
<li class="q-hint" aria-hidden="true">${ICON.grip}${esc(q.drag)}</li>
${row('is-done', 'bloom', `${esc(ALBUMS.bloom.artist)} · 10/10`, pill('ok', q.done, ICON.check))}
${row('is-fail', 'tide', `${esc(ALBUMS.tide.artist)} · <span data-fail-text>${esc(q.retryIn)}</span>`, `<span class="q-end">${pill('bad', q.failed, ICON.cross)}<button class="q-retry" type="button" data-retry data-queued="${esc(q.queued)}">${ICON.retry}<span>${esc(q.retry)}</span></button></span>`)}
</ul>`;
  return frame('queue', q.label, q.page, head, body);
}

function libraryView(l) {
  const st = l.states;
  // Per track: the ALAC slot's state, then the Dolby Atmos slot's.
  const slots = [
    ['done', 'done'],
    ['done', 'done'],
    ['done', 'done'],
    ['missing', 'done'],
    ['done', 'waiting'],
  ];
  const icon = { done: ICON.check, missing: ICON.cross, waiting: ICON.clock };
  const cell = (s, i, q) =>
    `<td class="slot is-${s}"${i === 3 && q === 0 ? ' data-missing' : ''}>${icon[s]}<span class="slot-word">${esc(st[s])}</span></td>`;
  const rows = GLASS_TRACKS.map(
    (t, i) => `<tr><td class="rec-n">${i + 1}</td><th scope="row">${esc(t)}</th>${slots[i].map((s, q) => cell(s, i, q)).join('')}</tr>`,
  ).join('');
  const list = [
    ['glass', l.partial, 'part', true],
    ['tidal', l.complete, 'ok'],
    ['tide', st.missing, 'bad'],
  ]
    .map(([k, state, kind, on]) => `<li${on ? ' class="on"' : ''}>${cover(k, 'cover-sm')}<span><b>${esc(ALBUMS[k].title)}</b><span class="badge badge-${kind}"${on ? ' data-badge' : ''}>${esc(state)}</span></span></li>`)
    .join('');
  const [primary, ...rest] = l.actions;
  const body = `<div class="lib">
<ul class="lib-list" aria-hidden="true">${list}</ul>
<div class="lib-rec" data-record data-complete="${esc(l.complete)}" data-done="${esc(st.done)}">
<div class="rec-head">${cover('glass', 'cover-lg')}<div class="rec-id"><b>${esc(ALBUMS.glass.title)}</b><span>${esc(ALBUMS.glass.artist)}</span><span class="rec-meta">${esc(l.meta)}</span><span class="rec-q">${l.qualities.map((q) => `<i>${esc(q)}</i>`).join('')}<span class="badge badge-part" data-badge>${esc(l.partial)}</span></span></div></div>
<div class="rec-actions"><button class="act act-primary" type="button" data-fix>${ICON.download}<span>${esc(primary)}</span></button>${rest.map((a) => `<span class="act">${esc(a)}</span>`).join('')}</div>
<table class="rec-tracks"><thead><tr><th scope="col">#</th><th scope="col">${esc(l.trackCol)}</th>${l.qualities.map((q) => `<th scope="col">${esc(q)}</th>`).join('')}</tr></thead><tbody>${rows}</tbody></table>
<p class="rec-path"><span>${esc(l.savedTo)}</span><code data-path data-fixed="${esc(glassPath(3))}">${esc(glassPath(0))}</code></p>
</div>
</div>`;
  return frame('library', l.label, l.page, '', body);
}

export const productViews = (p) => ({ search: searchView(p.search), queue: queueView(p.queue), library: libraryView(p.library) });

// ---- the specs' examples ----------------------------------------------------

export function specDemo(key, s) {
  switch (key) {
    case 'formats':
      return `<div class="chain"><code><span class="c-k">quality_priority</span>: [alac, ec3, aac]</code><ol aria-hidden="true"><li>alac</li><li>ec3</li><li>aac</li><li class="floor">aac-lc</li></ol></div>`;
    case 'records': {
      const cells = Array.from({ length: 12 }, (_, i) => (i === 7 ? 'wait' : i === 10 ? 'bad' : 'ok'));
      const mark = { ok: ICON.check, wait: ICON.clock, bad: ICON.cross };
      return `<ol class="cells" role="img" aria-label="${esc(s.records.label)}">${cells.map((c, i) => `<li class="is-${c}"><span>${i + 1}</span>${mark[c]}</li>`).join('')}</ol>`;
    }
    case 'library':
      return `<div class="tpl"><code>library/<b>{AlbumArtist}</b>/<b>{AlbumName}</b>/<b>{SongNumber:02d}</b>. <b>{SongName}</b></code><code class="tpl-out">${esc(glassPath(0))}</code></div>`;
    case 'accounts':
      return `<div class="regions"><code>music.apple.com/<b>jp</b>/album/…</code><span aria-hidden="true"><i>us</i><i class="on">jp</i><i>cn</i></span></div>`;
    case 'lyrics':
      return `<code class="lrc">[00:41.20] <span>&lt;00:41.20&gt;</span>Paper <span>&lt;00:41.86&gt;</span><b>satellites</b></code>`;
    case 'automation':
      return `<code class="yaml"><span class="c-k">events</span>: [job_finished]<br><span class="c-k">url</span>: <span class="c-s">https://media.example.org/refresh</span></code>`;
    case 'agents':
      return `<span class="tools"><code>search_catalog</code><code>start_download</code><code>wait_for_job</code></span>`;
    default:
      throw new Error(`no example for spec ${key}`);
  }
}
