// New download: the submit bar under the detected links (the count line,
// the quality controls, Download, and what went wrong with the last
// submit), its height for the room the page keeps under 640 px, and the
// Paste button of the text box on a touch screen.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { scanLinks } from '../lib/links.js';
import { useApp, Button, Notice } from '../ui/components.js';
import { useMedia } from '../ui/hooks.js';

/**
 * Props: count (the announced count line), controls (the quality
 * controls), label (the Download text), disabled, busy, title, onSubmit,
 * failure (the last submit's error), refusal (the server's invalid_overrides
 * message) and onCustom (opens Custom… from it), cfgError with
 * onRetryCfg (GET config failed: downloads use Settings as they are),
 * barRef.
 */
export function SubmitBar({ count, controls, label, disabled, busy, title, onSubmit, failure, refusal, onCustom, cfgError, onRetryCfg, barRef }) {
  return html`<div class="new-submit" ref=${barRef}>
    <p class="new-count" role="status" aria-live="polite">${count}</p>
    <div class="new-submit-row">
      ${controls}
      <${Button}
        variant="primary"
        icon="download"
        className="new-submit-btn"
        disabled=${disabled}
        aria-busy=${busy ? 'true' : undefined}
        title=${title}
        onClick=${onSubmit}
      >${label}</${Button}>
    </div>
    ${failure ? html`<${Notice} role="danger" icon="circle-x">${failure}</${Notice}>` : null}
    ${refusal
      ? html`<${Notice} role="danger" icon="circle-x" action=${html`<${Button} size="sm" onClick=${onCustom}>Custom…</${Button}>`}>
          <strong>The server refused these quality options.</strong> ${refusal}
        </${Notice}>`
      : null}
    ${cfgError
      ? html`<p class="new-cfg-note">
          <span>Couldn't load your Settings; downloads use them as they are.</span>
          <button type="button" class="link" onClick=${onRetryCfg}>Retry</button>
        </p>`
      : null}
  </div>`;
}

/** [ref, height]: the rendered height of the element in ref, kept current. */
export function useBarHeight() {
  const ref = useRef(null);
  const [h, setH] = useState(0);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver !== 'function') return undefined;
    const ro = new ResizeObserver(() => setH(Math.ceil(el.getBoundingClientRect().height)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
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
