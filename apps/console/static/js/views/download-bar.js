// New download: the dock that floats at the bottom of the viewport while
// the box holds links (what will be sent, what the quality asks for,
// Download, and under it what went wrong with the last submit), its
// height for the room the page keeps under 640 px, and the Paste button of
// the text box on a touch screen.

import { html, useState, useEffect, useRef, useCallback } from '../ui/preact.js';
import { scanLinks } from '../lib/links.js';
import { useApp, Button, Notice } from '../ui/components.js';
import { useMedia } from '../ui/hooks.js';
import { isMac } from './palette.js';

const SUBMIT_KEYS = isMac() ? '⌘↵' : 'Ctrl ↵';

/**
 * Props: on (the box holds links: the capsule shows), count (the count
 * line; #new-status announces it), asks (what the chosen quality asks for),
 * label (the Download text), disabled, busy, title, onSubmit, failure (the
 * last submit's error), refusal (the server's invalid_overrides message)
 * and onCustom (opens Custom… from it), barRef. The notes come under the
 * capsule, so nothing above them moves.
 */
export function Dock({ on, count, asks, label, disabled, busy, title, onSubmit, failure, refusal, onCustom, barRef }) {
  // "Download 3 albums": on a narrow phone the button says "Download" and
  // the count beside it says how many.
  const [verb, what] = /^Download /.test(label) ? ['Download', label.slice(8)] : [label, ''];
  return html`<div class="new-dock-wrap" ref=${barRef} data-on=${on ? '' : undefined}>
    <div class="new-dock">
      <div class="new-dock-text">
        <p class="new-dock-count" aria-hidden="true">${count}</p>
        ${asks ? html`<p class="new-dock-asks" title=${asks}>${asks}</p>` : null}
      </div>
      <${Button}
        variant="primary"
        icon="download"
        className="new-dock-go"
        disabled=${disabled}
        aria-busy=${busy ? 'true' : undefined}
        aria-keyshortcuts="Meta+Enter Control+Enter"
        title=${title}
        onClick=${onSubmit}
      >${verb}${what ? html`<span class="new-dock-what">${what}</span>` : null}<span class="new-dock-key" aria-hidden="true">${SUBMIT_KEYS}</span></${Button}>
    </div>
    <div class="new-notes">
      ${failure ? html`<${Notice} role="danger" icon="circle-x">${failure}</${Notice}>` : null}
      ${refusal
        ? html`<${Notice} role="danger" icon="circle-x" action=${html`<${Button} size="sm" onClick=${onCustom}>Custom…</${Button}>`}>
            <strong>The server refused these quality options.</strong> ${refusal}
          </${Notice}>`
        : null}
    </div>
  </div>`;
}

/**
 * [ref, height]: a callback ref and the rendered height of the element it
 * holds, kept current; 0 while nothing is mounted there.
 */
export function useBarHeight() {
  const [h, setH] = useState(0);
  const ro = useRef(null);
  const ref = useCallback((el) => {
    if (ro.current) ro.current.disconnect();
    ro.current = null;
    if (!el) {
      setH(0);
      return;
    }
    if (typeof ResizeObserver !== 'function') return;
    ro.current = new ResizeObserver(() => setH(Math.ceil(el.getBoundingClientRect().height)));
    ro.current.observe(el);
  }, []);
  useEffect(() => () => ro.current && ro.current.disconnect(), []);
  return [ref, h];
}

/**
 * Paste in the text box's corner where there is no keyboard to paste with
 * (a coarse pointer) and the browser offers clipboard reading; it hides for
 * good once the browser refuses. onLinks(urls) gets the Apple Music links
 * found on the clipboard.
 */
export function PasteButton({ storefront, onLinks }) {
  const app = useApp();
  const coarse = useMedia('(pointer: coarse)');
  const [off, setOff] = useState(false);
  if (!coarse || off || !(navigator.clipboard && navigator.clipboard.readText)) return null;
  const paste = async () => {
    let clip = '';
    try {
      clip = await navigator.clipboard.readText();
    } catch {
      setOff(true);
      app.toast({ message: 'The browser did not allow reading the clipboard. Long-press the box and paste.', role: 'warning' });
      return;
    }
    const urls = scanLinks(clip, storefront)
      .filter((e) => e.link)
      .map((e) => e.url);
    if (urls.length) onLinks(urls);
    else app.toast({ message: 'No Apple Music link on the clipboard', role: 'warning' });
  };
  return html`<${Button} size="sm" className="new-paste" onClick=${paste}>Paste</${Button}>`;
}
