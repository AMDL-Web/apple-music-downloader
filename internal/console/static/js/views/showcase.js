// Showcase: the latest completed release as a hero in Apple's own artwork
// colors, then a grid of completed albums, playlists or songs. Motion covers
// play on hover or focus where the browser plays HLS natively.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { jobTitle, jobSubtitle, artPalette, paletteStyle, artworkUrl, typeLabel, count, jobFacts } from '../lib/format.js';
import { isAlreadyInLibrary } from '../lib/status.js';
import { upsertJob, removeJob, appendPage, jobMatches } from '../lib/store.js';
import { buildHash, showcaseType, showcaseQuery, SHOWCASE_TYPES } from '../lib/route.js';
import { useApp, Artwork, Icon, Segmented, Empty, ErrorNote, Skeleton, Button, Facts } from '../ui/components.js';
import { useReducedMotion } from '../ui/hooks.js';

const PAGE_SIZE = 24;
const TYPE_LABELS = { album: 'Albums', playlist: 'Playlists', song: 'Songs' };

let hlsSupport = null;

/** Whether this browser plays HLS in a <video> without a library (Safari, iOS). */
function canPlayHls() {
  if (hlsSupport === null) {
    const v = document.createElement('video');
    hlsSupport = Boolean(v.canPlayType && v.canPlayType('application/vnd.apple.mpegurl'));
  }
  return hlsSupport;
}

export function ShowcasePage({ route }) {
  const app = useApp();
  const type = showcaseType(route.params);
  const [state, setState] = useState({ jobs: [], total: 0, loading: true, error: null, more: false });
  const [fresh, setFresh] = useState(() => new Set());
  const [playing, setPlaying] = useState(null);
  const reduced = useReducedMotion();
  const motionOk = !reduced && canPlayHls();
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
      if (motionOk && job.motion_artwork_url) setPlaying(job.id);
    },
    leave: (job) => setPlaying((p) => (p === job.id ? null : p)),
  };

  const [hero, ...rest] = state.jobs;

  return html`<div class="page page-showcase">
    <div class="toolbar">
      <${Segmented}
        label="Release type"
        options=${SHOWCASE_TYPES.map((t) => ({ value: t, label: TYPE_LABELS[t] }))}
        value=${type}
        onChange=${setType}
      />
      ${state.total > 0 ? html`<span class="toolbar-note">${count(state.total)} completed</span>` : null}
    </div>
    ${state.loading
      ? html`<div class="hero hero-skel" aria-hidden="true"><${Skeleton} kind="hero" /></div>
          <ul class="gallery" aria-busy="true">${Array.from({ length: 8 }, (_, i) => html`<li key=${i} class="tile-skel"><${Skeleton} kind="tile" /><${Skeleton} kind="line" /><${Skeleton} kind="text" /></li>`)}</ul>`
      : state.error
        ? html`<div class="card"><div class="card-body"><${ErrorNote} title="Couldn't load completed downloads" error=${state.error} onRetry=${() => load(0)} /></div></div>`
        : !hero
          ? html`<div class="card"><div class="card-body"><${Empty} icon="disc" title=${`Completed ${TYPE_LABELS[type].toLowerCase()} will appear here.`}>Jobs still in progress are listed in <a class="link" href=${buildHash('queue')}>the Queue</a>.</${Empty}></div></div>`
          : [
              html`<${Hero} key="hero" job=${hero} href=${href(hero)} playing=${playing === hero.id} hover=${hover} />`,
              rest.length
                ? html`<ul key="grid" class="gallery" aria-label=${TYPE_LABELS[type]}>
                    ${rest.map(
                      (job, i) => html`<li key=${job.id} class=${fresh.has(job.id) ? 'is-new' : undefined}>
                        <${Tile} job=${job} index=${i + 2} href=${href(job)} playing=${playing === job.id} hover=${hover} />
                      </li>`,
                    )}
                  </ul>`
                : null,
              state.jobs.length < state.total
                ? html`<div key="more" class="load-more">
                    <${Button} onClick=${() => load(state.jobs.length)} disabled=${state.more}>${state.more ? 'Loading…' : 'Load more'}</${Button}>
                  </div>`
                : null,
            ]}
  </div>`;
}

/** Muted looping motion cover over the still, in the motion asset's own colors. */
function MotionCover({ job }) {
  const p = artPalette(job, true);
  return html`<video
    class="motion"
    style=${p ? { '--art-bg': p.bg } : undefined}
    src=${job.motion_artwork_url}
    poster=${artworkUrl(job.artwork_url, 600) || undefined}
    muted
    autoplay
    loop
    playsinline
    disablepictureinpicture
    aria-hidden="true"
  ></video>`;
}

/** Two-digit catalogue number of a release in the current list ("01" is the hero). */
const catalogNo = (n) => String(n).padStart(2, '0');

function Hero({ job, href, playing, hover }) {
  const motion = playing && job.motion_artwork_url;
  const palette = artPalette(job, Boolean(motion)) || artPalette(job);
  const f = jobFacts(job);
  const facts = [
    ['genre', 'Genre', f.genre],
    ['year', 'Released', f.year],
    ['tracks', 'Tracks', f.tracks],
    ['finished', 'Finished', f.finished],
    ['took', 'Took', f.took],
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
      <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${480} widths=${[300, 600, 1200]} sizes="(max-width: 639px) 100vw, 50vw" className="art-square" eager />
      ${motion ? html`<${MotionCover} job=${job} />` : null}
    </div>
    <div class="hero-text">
      <span class="hero-kicker">Latest · ${typeLabel(job.type)}<span class="hero-no"> · No. ${catalogNo(1)}</span></span>
      <span class="hero-title" dir="auto">${jobTitle(job)}</span>
      <span class="hero-artist" dir="auto">${jobSubtitle(job)}</span>
      ${facts.length ? html`<${Facts} className="hero-facts" label="Release" rows=${facts} />` : null}
      ${isAlreadyInLibrary(job) ? html`<span class="tag-library"><${Icon} name="archive" size="sm" />Already in library</span>` : null}
    </div>
  </a>`;
}

/**
 * A gallery tile in the release's own Apple palette (--art-bg, --art-t1..t3).
 * .tile-index and .tile-facts are hooks a skin may show or rearrange.
 */
function Tile({ job, index, href, playing, hover }) {
  const palette = artPalette(job);
  const title = jobTitle(job);
  const f = jobFacts(job);
  return html`<a
    class=${palette ? 'tile has-palette' : 'tile'}
    href=${href}
    style=${paletteStyle(palette)}
    onMouseEnter=${() => hover.enter(job)}
    onMouseLeave=${() => hover.leave(job)}
    onFocus=${() => hover.enter(job)}
    onBlur=${() => hover.leave(job)}
  >
    <span class="tile-head" aria-hidden="true"><span class="tile-index">No. ${catalogNo(index)}</span><span class="tile-year">${f.year}</span></span>
    <div class="tile-art">
      <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${300} widths=${[300, 600]} sizes="(max-width: 639px) 50vw, 240px" className="art-square" />
      ${playing && job.motion_artwork_url ? html`<${MotionCover} job=${job} />` : null}
      ${job.motion_artwork_url ? html`<span class="tile-motion" title="Has a motion cover"><${Icon} name="play" size="sm" /><span class="sr-only">Motion cover</span></span>` : null}
    </div>
    <span class="tile-title" dir="auto" title=${title}>${title}</span>
    <span class="tile-sub" dir="auto">${jobSubtitle(job) || typeLabel(job.type)}</span>
    <span class="tile-facts">
      ${f.genre ? html`<span class="tile-fact" data-key="genre" dir="auto">${f.genre}</span>` : null}
      ${f.year ? html`<span class="tile-fact" data-key="year">${f.year}</span>` : null}
      ${f.tracks ? html`<span class="tile-fact" data-key="tracks">${f.tracks}</span>` : null}
      ${f.finished ? html`<span class="tile-fact" data-key="finished">${f.finished}</span>` : null}
    </span>
    ${isAlreadyInLibrary(job) ? html`<span class="tag-library"><${Icon} name="archive" size="sm" />Already in library</span>` : null}
  </a>`;
}
