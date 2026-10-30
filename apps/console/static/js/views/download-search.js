// New download: catalog search, for a text box that holds words and no link.
// The hits come in three titled sections, each with Show more: Albums and
// Artists as tile walls (download-tiles.js), Songs as rows on a grouped
// surface (a 48 px cover, the title with E and quality tags, one meta
// line, a note when the item is in the library or the queue, Download). An
// artist opens its albums in their place with Back and Download all.
// ArrowDown from the text box enters the hits, the arrows move between
// them, Return downloads or opens, Escape leaves. While a new term loads,
// the last hits stay (dimmed) and the room never shrinks during a search,
// so nothing under it jumps as results come and go. The state is in
// download-search-state.js, the logic pure in lib/search.js; the page
// (download.js) submits what Download sends.

import { html, useState, useRef, useLayoutEffect } from '../ui/preact.js';
import { artPalette, paletteStyle, artWidths, shortDate } from '../lib/format.js';
import { hitView, hitKey, shownHits, nextStop, SEARCH_GROUPS, SEARCH_FIRST, SEARCH_MAX_TERM } from '../lib/search.js';
import { shortError } from '../lib/lookup.js';
import { Card, Button, Icon, Artwork, Skeleton, Notice } from '../ui/components.js';
import { AlbumTile, ArtistTile, SkeletonTiles } from './download-tiles.js';

const HIT_ART = artWidths(48);
const ARTIST_ART = artWidths(120);

// Placeholders per group while the first answer loads: a typical answer,
// so the page is about its final height when the hits arrive.
const SKELETON_ROWS = { songs: SEARCH_FIRST, albums: SEARCH_FIRST, artists: 2 };

/** The note for an earlier job, as on a link row: in the queue now, or in the library since when, with Open. */
function PriorNote({ prior, href }) {
  const active = prior.state === 'active';
  const when = active ? '' : shortDate(prior.job.finished_at);
  return html`<span class="new-link-prior new-hit-prior" data-state=${prior.state}>
    <${Icon} name=${active ? 'clock' : 'circle-check'} size="sm" />
    <span>${active ? 'In the queue now' : 'In your library'}${when ? ` · ${when}` : ''} · <a class="link new-link-open" href=${href} title="Open that job" tabindex="-1">Open</a></span>
  </span>`;
}

/**
 * A song: a row on the Songs surface. Download is its one focus stop
 * (data-hit-primary); a song in the queue offers none to press.
 */
function HitRow({ hit, row }) {
  const v = hitView(hit);
  const palette = artPalette(hit);
  const prior = row.priorOf(hit);
  const busy = row.busy.has(hit.url);
  const queued = prior && prior.state === 'active';
  const cls = ['new-hit', palette ? 'has-palette' : ''].filter(Boolean).join(' ');
  const meta = v.facts.map((f) => f.text).join(' · ');
  return html`<li class=${cls} data-kind=${hit.type} data-id=${hit.id} data-prior=${prior ? prior.state : undefined} style=${paletteStyle(palette)}>
    <${Artwork} url=${hit.artwork_url} bg=${palette && palette.bg} px=${100} widths=${HIT_ART} sizes="48px" className="art-48 new-hit-art" />
    <div class="new-hit-main">
      <span class="new-hit-head">
        <span class="new-hit-title" dir="auto" title=${v.title}>${v.title}</span>
        ${v.explicit ? html`<span class="new-link-explicit" title="Explicit" aria-label="Explicit">E</span>` : null}
        ${v.quality.map((q) => html`<span key=${q} class="chip new-hit-quality">${q}</span>`)}
      </span>
      <span class="new-hit-sub" title=${meta}>
        ${v.facts.map((f) => html`<span key=${f.key} class="new-hit-fact" data-key=${f.key} dir=${f.auto ? 'auto' : undefined}>${f.text}</span>`)}
      </span>
      ${prior ? html`<${PriorNote} prior=${prior} href=${row.jobHref(prior.job.id)} />` : null}
    </div>
    <${Button}
      size="sm"
      variant="ghost"
      icon="download"
      className="new-hit-download"
      data-hit-primary
      disabled=${busy || queued}
      aria-busy=${busy ? 'true' : undefined}
      aria-label=${queued ? `${v.title} is in the queue` : `Download ${v.title}`}
      title=${queued ? 'Already in the queue' : prior ? 'In your library; tracks on disk are skipped unless Overwrite is on' : 'Download'}
      onClick=${() => row.onDownload(hit)}
    />
  </li>`;
}

/** A failed read: what failed, the server's reason without the catalog prefix it repeats, Retry. */
function FailNote({ what, error, onRetry }) {
  const why = shortError(String((error && error.message) || '').replace(/^read Apple Music catalog:\s*/i, ''));
  return html`<div class="new-search-fail" role="alert">
    <${Notice} role="danger" icon="circle-x" action=${html`<${Button} size="sm" icon="rotate" className="new-search-retry" onClick=${onRetry}>Retry</${Button}>`}>
      <span class="new-search-fail-text"><strong>${what}.</strong> ${why}</span>
    </${Notice}>
  </div>`;
}

function SkeletonHits({ n }) {
  return html`<ul class="new-hits" aria-hidden="true">
    ${Array.from(
      { length: n },
      (_, i) => html`<li key=${i} class="new-hit is-skel">
        <span class="art art-48 new-hit-art"><${Skeleton} kind="tile" /></span>
        <span class="new-hit-main">
          <span class="new-hit-head"><span class="skel skel-lh new-hit-skel-title"></span></span>
          <span class="new-hit-sub"><span class="skel skel-lh skel-short"></span></span>
        </span>
      </li>`,
    )}
  </ul>`;
}

/** Show more in a section's head (red text, as Overview's actions), or its failure with Retry. */
function MoreAction({ state, onMore }) {
  if (state === 'error') {
    return html`<span class="new-search-more-fail status-text" data-role="warning"><${Icon} name="triangle-alert" size="sm" /><span>Couldn't load more</span></span>
      <${Button} variant="ghost" onClick=${onMore}>Retry</${Button}>`;
  }
  return html`<${Button} variant="ghost" className="new-search-more-btn" disabled=${state === 'loading'} aria-busy=${state === 'loading' ? 'true' : undefined} onClick=${onMore}
    >${state === 'loading' ? 'Loading…' : 'Show more'}</${Button}>`;
}

/** Show more under an artist's albums, its loading state, or its failure with Retry. */
function MoreRow({ state, label, onMore }) {
  if (state === 'error') {
    return html`<div class="new-search-more">
      <span class="status-text" data-role="warning"><${Icon} name="triangle-alert" size="sm" /><span>Couldn't load more</span></span>
      <${Button} size="sm" icon="rotate" onClick=${onMore}>Retry</${Button}>
    </div>`;
  }
  return html`<div class="new-search-more">
    <${Button} size="sm" icon="chevron-down" className="new-search-more-btn" disabled=${state === 'loading'} aria-busy=${state === 'loading' ? 'true' : undefined} onClick=${onMore}
      >${state === 'loading' ? 'Loading…' : label}</${Button}>
  </div>`;
}

// Hits are keyed by position: a new term's hits take the places the last
// term's had, in place, instead of the old ones moving to new places.
function Groups({ data, loading, more, row, onMore }) {
  return SEARCH_GROUPS.map((g) => {
    const group = data && data[g.id];
    if (!loading && !(group && group.items.length)) return null;
    const songs = g.id === 'songs';
    const kind = g.id === 'artists' ? 'artist' : 'album';
    const Tile = kind === 'artist' ? ArtistTile : AlbumTile;
    const label = `new-search-${g.id}`;
    let body;
    if (loading) {
      body = songs ? html`<${SkeletonHits} n=${SKELETON_ROWS[g.id]} />` : html`<${SkeletonTiles} n=${SKELETON_ROWS[g.id]} kind=${kind} />`;
    } else if (songs) {
      body = html`<ul class="new-hits" aria-labelledby=${label}>
        ${group.items.map((hit, i) => html`<${HitRow} key=${i} hit=${hit} row=${row} />`)}
      </ul>`;
    } else {
      body = html`<ul class="wall new-wall" data-kind=${kind} aria-labelledby=${label}>
        ${group.items.map((hit, i) => html`<${Tile} key=${i} hit=${hit} row=${row} />`)}
      </ul>`;
    }
    return html`<${Card}
      key=${g.id}
      section=${`hits-${g.id}`}
      className=${songs ? 'card-flush new-search-group' : 'new-search-group'}
      title=${g.label}
      labelId=${label}
      actions=${!loading && group.next_offset ? html`<${MoreAction} state=${more[g.id]} onMore=${() => onMore(g.id)} />` : null}
    >${body}</${Card}>`;
  });
}

function ArtistView({ artist, hit, row, onBack, onMore, onRetry, onDownloadAll }) {
  const shown = artist.artist || hit;
  const palette = shown ? artPalette(shown) : null;
  const name = shown ? shown.name : '';
  return html`<div class="new-artist">
    <div class=${palette ? 'new-artist-head has-palette' : 'new-artist-head'} style=${paletteStyle(palette)}>
      <${Button} variant="ghost" size="sm" icon="chevron-left" className="new-artist-back" data-hit-primary onClick=${onBack}>Results</${Button}>
      ${shown
        ? html`<${Artwork} url=${shown.artwork_url} bg=${palette && palette.bg} px=${240} widths=${ARTIST_ART} sizes="(max-width: 639px) 88px, 120px" className="new-artist-art" />`
        : html`<span class="art new-artist-art"><${Skeleton} kind="tile" /></span>`}
      <div class="new-artist-main">
        ${name ? html`<h2 class="new-artist-name" dir="auto">${name}</h2>` : html`<span class="skel skel-lh new-hit-skel-title"></span>`}
        <span class="new-artist-sub" dir=${shown && shown.genre ? 'auto' : undefined}>${shown && shown.genre ? shown.genre : 'Artist'}</span>
      </div>
      <${Button} icon="download" className="new-artist-all" disabled=${!shown} onClick=${() => onDownloadAll(shown)}>Download all</${Button}>
    </div>
    ${artist.status === 'error'
      ? html`<${FailNote} what="Couldn't load the albums" error=${artist.error} onRetry=${onRetry} />`
      : artist.status === 'ready'
        ? html`<ul class="wall new-wall" data-kind="album" aria-label=${`Albums by ${name}`}>
            ${artist.albums.items.map((a) => html`<${AlbumTile} key=${hitKey(a)} hit=${a} row=${row} />`)}
          </ul>
          ${artist.albums.items.length === 0 ? html`<p class="new-search-note">Apple Music lists no albums for this artist here.</p>` : null}
          ${artist.albums.next_offset ? html`<${MoreRow} state=${artist.more} label="Show more albums" onMore=${onMore} />` : null}`
        : html`<${SkeletonTiles} n=${SEARCH_FIRST} kind="album" />`}
  </div>`;
}

/**
 * Props: s (useSearch), term, tooLong, inputRef (the text box arrows
 * return to), openHit (the artist hit an artist view was opened from, for
 * its head before the answer), focusArtist (focus its Back on open), row: {busy (urls being sent), jobHref(id),
 * onDownload(hit), onOpenArtist(hit), priorOf(hit)}, onBack, onDownloadAll(artistHit).
 */
export function SearchCard({ s, term, tooLong, inputRef, openHit, focusArtist, row, onBack, onDownloadAll }) {
  const { search, artist } = s;
  const viewing = artist.status !== 'idle';
  // Skeletons only while there is nothing to show; otherwise the last
  // answer stays, dimmed, through the debounce and the request.
  const haveHits = Boolean(search.data);
  const loading = !viewing && !haveHits && (s.pending || search.status === 'loading');
  const stale = !viewing && haveHits && (s.pending || search.stale);
  const empty = !viewing && !loading && search.status === 'ready' && search.data && shownHits(search.data).length === 0;

  // The room the card takes never shrinks while a term is in the box:
  // results that come back shorter, an error or "nothing found" leave
  // canvas below the card instead of pulling up everything under it.
  const body = useRef(null);
  const room = useRef(null);
  const [floor, setFloor] = useState(0);
  useLayoutEffect(() => {
    if (!term) {
      if (floor) setFloor(0);
      return;
    }
    const h = room.current ? Math.ceil(room.current.firstElementChild.getBoundingClientRect().height) : 0;
    if (h > floor) setFloor(h);
  });

  // Focus follows the view: an artist this page opened focuses its Back;
  // back in the results, the row of the artist that was open takes focus
  // when the view it had is gone.
  const was = useRef({ viewing, id: '' });
  useLayoutEffect(() => {
    const prev = was.current;
    was.current = { viewing, id: viewing ? artist.id : prev.id };
    if (!body.current || viewing === prev.viewing) return;
    const lost = !document.activeElement || document.activeElement === document.body || body.current.contains(document.activeElement);
    const el = viewing
      ? focusArtist && body.current.querySelector('.new-artist-back')
      : lost && prev.id && body.current.querySelector(`.new-tile[data-id="${prev.id}"] .new-tile-open`);
    if (el) el.focus();
  });

  // The hits are one Tab stop: the one last focused (else the first) keeps
  // tabindex 0 and the arrows move between the rest.
  const stopsOf = () => (body.current ? [...body.current.querySelectorAll('[data-hit-primary]')].filter((el) => !el.disabled && el.offsetParent !== null) : []);
  const roving = useRef(null);
  useLayoutEffect(() => {
    const stops = stopsOf();
    const keep = stops.includes(roving.current) ? roving.current : stops[0];
    for (const el of stops) el.tabIndex = el === keep ? 0 : -1;
  });
  const onFocusIn = (e) => {
    const stops = stopsOf();
    if (!stops.includes(e.target)) return;
    roving.current = e.target;
    for (const el of stops) el.tabIndex = el === e.target ? 0 : -1;
  };

  const onKeyDown = (e) => {
    if (e.altKey || e.ctrlKey || e.metaKey || !body.current) return;
    const stops = stopsOf();
    const at = stops.indexOf(document.activeElement);
    // The whole tile or row comes into view, not just its button.
    const go = (el) => {
      e.preventDefault();
      el.focus({ preventScroll: true });
      (el.closest('li') || el).scrollIntoView({ block: 'nearest', inline: 'nearest' });
    };
    const toInput = () => {
      e.preventDefault();
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      el.setSelectionRange(el.value.length, el.value.length);
    };
    if (e.key === 'Escape') {
      if (viewing) {
        e.preventDefault();
        onBack();
      } else toInput();
      return;
    }
    if (at < 0 || !['ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
    // Where each hit (its tile or row, not its button) is drawn, and which
    // section it is in.
    const boxes = stops.map((el) => {
      const r = (el.closest('li') || el).getBoundingClientRect();
      const group = el.closest('[data-section], .new-artist-head, .new-artist');
      return { top: r.top, bottom: r.bottom, left: r.left, right: r.right, group };
    });
    const to = nextStop(boxes, at, e.key);
    if (to === -1) toInput();
    else if (to !== at) go(stops[to]);
    else if (e.key.startsWith('Arrow')) e.preventDefault();
  };

  let content;
  if (tooLong) {
    content = html`<p class="new-search-note">Search terms are at most ${SEARCH_MAX_TERM} characters.</p>`;
  } else if (viewing) {
    content = html`<${ArtistView}
      artist=${artist}
      hit=${openHit}
      row=${row}
      onBack=${onBack}
      onMore=${s.artistMore}
      onRetry=${s.artistRetry}
      onDownloadAll=${onDownloadAll}
    />`;
  } else if (!loading && search.status === 'error') {
    content = html`<${FailNote} what="Couldn't search Apple Music" error=${search.error} onRetry=${s.retry} />`;
  } else if (empty) {
    content = html`<p class="new-search-note new-search-empty">
      <${Icon} name="search" size="sm" />
      <span>Nothing in Apple Music matches “<span dir="auto">${search.term}</span>”</span>
    </p>`;
  } else {
    content = html`<${Groups} data=${search.data} loading=${loading} more=${search.more} row=${row} onMore=${s.more} />`;
  }

  return html`<div class="new-search-room" ref=${room} style=${floor ? { minHeight: `${floor}px` } : undefined}>
    <section
      class="new-search"
      ref=${body}
      aria-labelledby="new-search-title"
      data-state=${viewing ? 'artist' : loading ? 'loading' : stale ? 'stale' : search.status}
      aria-busy=${loading || stale ? 'true' : undefined}
      onKeyDown=${onKeyDown}
      onfocusin=${onFocusIn}
    >
      <h2 id="new-search-title" class="sr-only">Search results</h2>
      ${content}
    </section>
  </div>`;
}
