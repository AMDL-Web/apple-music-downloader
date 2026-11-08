// A job's motion cover as a muted loop over its still cover. The console
// reads Apple's motion playlists itself (lib/motion.js picks the MP4 to
// play); that is its one request outside /api/v1, allowed by the CSP's
// connect-src. Anything that fails leaves the still cover alone.

import { html, useState, useEffect, useRef } from './preact.js';
import { artPalette } from '../lib/format.js';
import { motionVariants, pickMotionVariant, motionFile } from '../lib/motion.js';
import { useReducedMotion } from './hooks.js';

// Parsed playlists for this page's lifetime, keyed by URL: url -> Promise.
const playlists = new Map();

function playlist(url) {
  if (!playlists.has(url)) {
    const p = fetch(url, { credentials: 'omit' }).then((r) => {
      if (!r.ok) throw new Error(`motion playlist: HTTP ${r.status}`);
      return r.text();
    });
    // A failed read may succeed later (the network came back): forget it.
    p.catch(() => playlists.delete(url));
    playlists.set(url, p);
  }
  return playlists.get(url);
}

/** The MP4 that plays `master` sharply in a box `px` device pixels wide; '' when there is none. */
async function motionSource(master, px) {
  const variant = pickMotionVariant(motionVariants(await playlist(master), master), px);
  if (!variant) return '';
  return motionFile(await playlist(variant.url), variant.url);
}

/** Whether this browser plays HLS without a library (Safari). */
let hlsSupport = null;
function canPlayHls() {
  if (hlsSupport === null) {
    const v = document.createElement('video');
    hlsSupport = Boolean(v.canPlayType && v.canPlayType('application/vnd.apple.mpegurl'));
  }
  return hlsSupport;
}

/**
 * The motion cover of `job` over the cover it shares a box with (the parent
 * positions it), sized for a box `size` CSS px wide. It plays while `play`
 * is set, motion is allowed and it is on screen; it shows only once a frame
 * is ready, and on any failure it goes away and the still stays.
 */
export function MotionCover({ job, size, play = true }) {
  const url = job && job.motion_artwork_url;
  const reduced = useReducedMotion();
  const [src, setSrc] = useState('');
  const [state, setState] = useState('idle'); // idle | ready | failed
  const ref = useRef(null);
  const active = Boolean(url) && play && !reduced;

  useEffect(() => {
    setSrc('');
    setState('idle');
    if (!active) return undefined;
    let live = true;
    const px = Math.ceil(size * (window.devicePixelRatio || 1));
    motionSource(url, px).then(
      (mp4) => {
        if (!live) return;
        if (mp4) setSrc(mp4);
        else if (canPlayHls()) setSrc(url);
        else setState('failed');
      },
      () => live && (canPlayHls() ? setSrc(url) : setState('failed')),
    );
    return () => {
      live = false;
    };
  }, [url, size, active]);

  // Off screen it pauses; back on screen it carries on.
  useEffect(() => {
    const v = ref.current;
    if (!v || typeof IntersectionObserver !== 'function') return undefined;
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) v.play().catch(() => {});
        else v.pause();
      }
    });
    io.observe(v);
    return () => io.disconnect();
  }, [src]);

  if (!active || !src || state === 'failed') return null;
  const p = artPalette(job, true);
  const onError = () => {
    // The MP4 failed: Safari still has the playlist itself; others keep the still.
    if (src !== url && canPlayHls()) setSrc(url);
    else setState('failed');
  };
  return html`<video
    ref=${ref}
    class=${state === 'ready' ? 'motion is-ready' : 'motion'}
    style=${p ? { '--art-bg': p.bg } : undefined}
    src=${src}
    muted
    autoplay
    loop
    playsinline
    disablepictureinpicture
    preload="auto"
    aria-hidden="true"
    onPlaying=${() => setState('ready')}
    onError=${onError}
  ></video>`;
}
