// Overview, Recently completed: the newest release as a banner, its motion
// cover playing, then a wall of the ones before it, two rows deep, one tile
// per release (repeat downloads of a link collapse to the newest). A tile
// opens the job drawer over the Overview; See all is the Queue's Completed
// tab. Tiles are keyed by place, so a new completion changes what each tile
// shows and moves none of them.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { count, jobTitle, jobSubtitle, artPalette, paletteStyle, typeLabel, jobFacts, artWidths } from '../lib/format.js';
import { uniqueByTarget } from '../lib/albums.js';
import { buildHash } from '../lib/route.js';
import { useApp, Card, Artwork, Empty, ErrorNote, LinkButton } from '../ui/components.js';
import { useColumns, useReducedMotion } from '../ui/hooks.js';
import { MotionCover } from '../ui/motion.js';
import { applyFeedToList } from './queue.js';

const RECENT_FILTER = { statuses: ['completed'] };
// Read more than are shown: repeat downloads of one album collapse to one tile.
const RECENT_FETCH = 48;
const WALL_ROWS = 2;
const TILE_MIN = 100;
const TILE_GAP = 16;
// The banner's cover is 112 px (88 on a phone); a tile 100-170 px.
const BANNER_ART = artWidths(88, 112);
const TILE_ART = artWidths(120, 170);

export function RecentSection() {
  const app = useApp();
  const ref = useRef(null);
  const cols = useColumns(ref, TILE_MIN, TILE_GAP);
  const reduced = useReducedMotion();
  const [state, setState] = useState({ jobs: [], loading: true, error: null });
  const [playing, setPlaying] = useState(null);
  const seq = useRef(0);
  const load = () => {
    const mine = ++seq.current;
    return listDownloads({ status: 'completed', sort: 'updated_at', order: 'desc', limit: RECENT_FETCH }).then(
      (body) => {
        if (mine === seq.current) setState({ jobs: body.downloads, loading: false, error: null });
      },
      (error) => {
        if (mine === seq.current) setState((s) => ({ ...s, loading: false, error }));
      },
    );
  };
  useEffect(() => {
    load();
  }, [app.epoch]);
  useEffect(
    () => app.subscribeFeed((batch) => setState((s) => ({ ...s, jobs: applyFeedToList(s.jobs, batch, RECENT_FILTER, { limit: RECENT_FETCH }) }))),
    [],
  );

  const slots = Math.max(1, cols) * WALL_ROWS;
  const completed = app.counts ? app.counts.completed : null;
  const albums = uniqueByTarget(state.jobs);
  const hover = {
    enter: (job) => !reduced && job.motion_artwork_url && setPlaying(job.id),
    leave: (job) => setPlaying((p) => (p === job.id ? null : p)),
  };

  let body;
  if (state.loading) {
    // As many tiles as the completed count allows, so the wall keeps its rows.
    const n = completed === null ? slots : Math.min(slots, Math.max(0, completed - 1));
    body = completed === 0
      ? html`<${Empty} icon="disc" title="Completed downloads will appear here" />`
      : [
          html`<div key="latest" class="latest is-skel" aria-hidden="true">
            <span class="art art-square skel"></span>
            <span class="latest-text">
              <span class="latest-kicker"><span class="skel skel-text"> </span></span>
              <span class="latest-title"><span class="skel skel-text"> </span></span>
              <span class="latest-artist"><span class="skel skel-text"> </span></span>
              <span class="latest-facts"><span class="skel skel-text"> </span></span>
            </span>
          </div>`,
          n > 0 ? html`<div key="wall" class="wall" aria-hidden="true">${Array.from({ length: n }, (_, i) => html`<${SkeletonTile} key=${i} />`)}</div>` : null,
        ];
  } else if (state.error) {
    body = html`<${ErrorNote} title="Couldn't load completed jobs" error=${state.error} onRetry=${load} />`;
  } else if (albums.length === 0) {
    body = html`<${Empty} icon="disc" title="Completed downloads will appear here" />`;
  } else {
    const wall = albums.slice(1, 1 + slots);
    body = [
      html`<${Latest} key="latest" job=${albums[0]} />`,
      wall.length
        ? html`<ul key="wall" class="wall" aria-label="Earlier downloads">
            ${wall.map((job, i) => html`<li key=${i}><${Tile} job=${job} playing=${playing === job.id} hover=${hover} /></li>`)}
          </ul>`
        : null,
    ];
  }

  return html`<${Card}
    section="recent"
    title="Recently completed"
    labelId="recent-title"
    note=${completed === null ? ' ' : `${count(completed)} completed`}
    actions=${html`<${LinkButton} variant="ghost" size="sm" href=${buildHash('queue', { tab: 'completed' })}>See all</${LinkButton}>`}
  >
    <div class="recent" ref=${ref} style=${{ '--cols': String(Math.max(1, cols)) }}>${body}</div>
  </${Card}>`;
}

/** The newest release: cover (its motion cover playing), kind, title, artist and one line of facts. */
function Latest({ job }) {
  const palette = artPalette(job);
  const f = jobFacts(job);
  const facts = [f.genre, f.year, f.tracks, f.finished && `Finished ${f.finished}`].filter(Boolean).join(' · ');
  return html`<a class=${palette ? 'latest has-palette' : 'latest'} href=${buildHash('overview', {}, job.id)} style=${paletteStyle(palette)}>
    <${Artwork} url=${job.artwork_url} bg=${palette && palette.bg} px=${224} widths=${BANNER_ART} sizes="(max-width: 639px) 88px, 112px" className="art-square" eager>
      ${job.motion_artwork_url ? html`<${MotionCover} job=${job} size=${112} />` : null}
    </${Artwork}>
    <span class="latest-text">
      <span class="latest-kicker">Latest · ${typeLabel(job.type)}</span>
      <span class="latest-title" dir="auto" title=${jobTitle(job)}>${jobTitle(job)}</span>
      <span class="latest-artist" dir="auto">${jobSubtitle(job) || ' '}</span>
      <span class="latest-facts" title=${facts}>${facts || ' '}</span>
    </span>
  </a>`;
}

/** A wall tile: the cover (its motion cover on hover or focus), then title and artist, one line each. */
function Tile({ job, playing, hover }) {
  const title = jobTitle(job);
  return html`<a
    class="wall-tile"
    href=${buildHash('overview', {}, job.id)}
    onMouseEnter=${() => hover.enter(job)}
    onMouseLeave=${() => hover.leave(job)}
    onFocus=${() => hover.enter(job)}
    onBlur=${() => hover.leave(job)}
  >
    <${Artwork} url=${job.artwork_url} px=${340} widths=${TILE_ART} sizes="(max-width: 639px) 33vw, 170px" className="art-square">
      ${playing && job.motion_artwork_url ? html`<${MotionCover} job=${job} size=${170} />` : null}
    </${Artwork}>
    <span class="wall-title" dir="auto" title=${title}>${title}</span>
    <span class="wall-sub" dir="auto">${jobSubtitle(job) || typeLabel(job.type)}</span>
  </a>`;
}

function SkeletonTile() {
  return html`<span class="wall-tile is-skel">
    <span class="art art-square skel"></span>
    <span class="wall-title"><span class="skel skel-text"> </span></span>
    <span class="wall-sub"><span class="skel skel-text"> </span></span>
  </span>`;
}
