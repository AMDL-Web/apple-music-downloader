// Showcase: a grid of completed albums, playlists or songs, one tile per
// release (jobs of the same link target collapse to the newest), under a
// compact banner for the latest one (desktop only; on a phone it is the first
// tile). Motion covers play on hover or focus (ui/motion.js).

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { jobTitle, jobSubtitle, artPalette, paletteStyle, typeLabel, jobFacts, artWidths } from '../lib/format.js';
import { uniqueByTarget } from '../lib/albums.js';
import { upsertJob, removeJob, appendPage, jobMatches } from '../lib/store.js';
import { buildHash, showcaseType, showcaseQuery, SHOWCASE_TYPES } from '../lib/route.js';
import { useApp, Artwork, Segmented, Empty, ErrorNote, Skeleton, Button, LinkButton, Facts } from '../ui/components.js';
import { useReducedMotion } from '../ui/hooks.js';
import { MotionCover } from '../ui/motion.js';

const PAGE_SIZE = 24;
const TYPE_LABELS = { album: 'Albums', playlist: 'Playlists', song: 'Songs' };

// The banner's cover is at most 160 px wide (the banner is hidden on a
// phone); a tile is half a phone screen, or about 240 px on a desktop.
const HERO_ART = artWidths(140, 160);
const TILE_ART = artWidths(190, 240);

export function ShowcasePage({ route }) {
  const app = useApp();
  const type = showcaseType(route.params);
  const [state, setState] = useState({ jobs: [], total: 0, loading: true, error: null, more: false });
  const [fresh, setFresh] = useState(() => new Set());
  const [playing, setPlaying] = useState(null);
  const reduced = useReducedMotion();
  const seq = useRef(0);

  const load = (offset) => {
    const mine = ++seq.current;
    if (offset === 0) setState((s) => ({ ...s, loading: true, error: null }));
    else setState((s) => ({ ...s, more: true }));
    listDownloads(showcaseQuery(type, offset, PAGE_SIZE)).then(
      (body) => {
        if (mine !== seq.current) return;
        setState((s) => ({
          jobs: offset === 0 ? body.downloads : appendPage(s.jobs, body.downloads),
          total: body.total,
          loading: false,
          error: null,
          more: false,
        }));
      },
      (error) => {
        if (mine === seq.current) setState((s) => ({ ...s, loading: false, more: false, error }));
      },
    );
  };

  useEffect(() => {
    setFresh(new Set());
    load(0);
  }, [type, app.epoch]);

  const jobsRef = useRef(state.jobs);
  jobsRef.current = state.jobs;
  useEffect(() => {
    const filter = { statuses: ['completed'], types: [type] };
    return app.subscribeFeed((batch) => {
      const held = new Set(jobsRef.current.map((j) => j.id));
      const added = batch.filter((m) => m.job && !held.has(m.job.id) && jobMatches(m.job, filter)).map((m) => m.job.id);
      setState((s) => {
        let jobs = s.jobs;
        for (const msg of batch) {
          if (msg.type === 'download_deleted') jobs = removeJob(jobs, msg.job_id);
          else if (msg.job) jobs = upsertJob(jobs, msg.job, filter);
        }
        return { ...s, jobs, total: Math.max(0, s.total + jobs.length - s.jobs.length) };
      });
      if (added.length) setFresh((f) => new Set([...f, ...added]));
    });
  }, [type]);

  const setType = (t) => app.navigate(buildHash('showcase', { type: t === 'album' ? '' : t }, route.jobId), { replace: true });
  const href = (job) => buildHash('showcase', route.params, job.id);
  const hover = {
    enter: (job) => {
      if (!reduced && job.motion_artwork_url) setPlaying(job.id);
    },
    leave: (job) => setPlaying((p) => (p === job.id ? null : p)),
  };

  // The newest release leads: a banner on desktop, the first tile on a phone.
  const albums = uniqueByTarget(state.jobs);
  const hero = albums[0];

  return html`<div class="page page-showcase">
    <div class="toolbar">
      <${Segmented}
        label="Release type"
        options=${SHOWCASE_TYPES.map((t) => ({ value: t, label: TYPE_LABELS[t] }))}
        value=${type}
        onChange=${setType}
      />
    </div>
    ${state.loading
      ? html`<div class="hero hero-skel" aria-hidden="true"><${Skeleton} kind="hero" /></div>
          <ul class="gallery" aria-busy="true">${Array.from({ length: 8 }, (_, i) => html`<li key=${i} class="tile-skel"><${Skeleton} kind="tile" /><${Skeleton} kind="line" /><${Skeleton} kind="text" /></li>`)}</ul>`
      : state.error
        ? html`<div class="card"><div class="card-body"><${ErrorNote} title="Couldn't load completed downloads" error=${state.error} onRetry=${() => load(0)} /></div></div>`
        : !hero
          ? html`<div class="card"><div class="card-body"><${Empty}
              icon="disc"
              title=${`Completed ${TYPE_LABELS[type].toLowerCase()} will appear here.`}
              action=${html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`}
            >Jobs still in progress are listed in <a class="link" href=${buildHash('queue')}>the Queue</a>.</${Empty}></div></div>`
          : [
              html`<${Hero} key="hero" job=${hero} href=${href(hero)} playing=${playing === hero.id} hover=${hover} />`,
              html`<ul key="grid" class="gallery" aria-label=${TYPE_LABELS[type]}>
                ${albums.map(
                  (job, i) => html`<li key=${job.id} class=${[fresh.has(job.id) ? 'is-new' : '', i === 0 ? 'is-latest' : ''].filter(Boolean).join(' ') || undefined}>
                    <${Tile} job=${job} href=${href(job)} playing=${playing === job.id} hover=${hover} />
                  </li>`,
                )}
              </ul>`,
              state.jobs.length < state.total
                ? html`<div key="more" class="load-more">
                    <${Button} onClick=${() => load(state.jobs.length)} disabled=${state.more}>${state.more ? 'Loading…' : 'Load more'}</${Button}>
                  </div>`
                : null,
            ]}
  </div>`;
}

function Hero({ job, href, playing, hover }) {
  const motion = playing && job.motion_artwork_url;
  const palette = artPalette(job, Boolean(motion)) || artPalette(job);
  const f = jobFacts(job);
  const facts = [
    ['genre', 'Genre', f.genre],
    ['year', 'Released', f.year],
    ['tracks', 'Tracks', f.tracks],
    ['finished', 'Finished', f.finished],
  ].filter((r) => r[2]);
  return html`<a
    class=${palette ? 'hero has-palette' : 'hero'}
    href=${href}
    style=${paletteStyle(palette)}
    onMouseEnter=${() => hover.enter(job)}
    onMouseLeave=${() => hover.leave(job)}
    onFocus=${() => hover.enter(job)}
    onBlur=${() => hover.leave(job)}
  >
    <div class="hero-art">
      <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${320} widths=${HERO_ART} sizes="160px" className="art-square" eager />
      ${motion ? html`<${MotionCover} job=${job} size=${160} />` : null}
    </div>
    <div class="hero-text">
      <span class="hero-kicker">Latest · ${typeLabel(job.type)}</span>
      <span class="hero-title" dir="auto">${jobTitle(job)}</span>
      <span class="hero-artist" dir="auto">${jobSubtitle(job)}</span>
      ${facts.length ? html`<${Facts} className="hero-facts" label="Release" rows=${facts} />` : null}
    </div>
  </a>`;
}

/** A gallery tile: the cover, then title and artist, on the page's own surface. */
function Tile({ job, href, playing, hover }) {
  const title = jobTitle(job);
  return html`<a
    class="tile"
    href=${href}
    onMouseEnter=${() => hover.enter(job)}
    onMouseLeave=${() => hover.leave(job)}
    onFocus=${() => hover.enter(job)}
    onBlur=${() => hover.leave(job)}
  >
    <div class="tile-art">
      <${Artwork} url=${job.artwork_url} px=${480} widths=${TILE_ART} sizes="(max-width: 639px) 50vw, 240px" className="art-square" />
      ${playing && job.motion_artwork_url ? html`<${MotionCover} job=${job} size=${240} />` : null}
      ${job.motion_artwork_url ? html`<span class="tile-motion">Motion cover</span>` : null}
    </div>
    <span class="tile-title" dir="auto" title=${title}>${title}</span>
    <span class="tile-sub" dir="auto">${jobSubtitle(job) || typeLabel(job.type)}</span>
  </a>`;
}
