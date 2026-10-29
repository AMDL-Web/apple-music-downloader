// Shared presentational components. Status is always icon + label + color.

import { html, useState, useEffect, createContext, useContext } from './preact.js';
import { artworkUrl, artworkSrcset, relTime, absTime } from '../lib/format.js';
import { jobProgress, progressCells, progressLegend, stageSegments, stageText } from '../lib/status.js';
import { useNow } from './hooks.js';
import { iconHref } from './icons.js';

/** App-wide services: navigate, toast, announce, feed subscription, counts, connection, system info. */
export const AppContext = createContext(null);

export const useApp = () => useContext(AppContext);

export function Icon({ name, size }) {
  const cls = size ? `icon icon-${size}` : 'icon';
  return html`<svg class=${cls} aria-hidden="true" focusable="false"><use href=${iconHref(name)} /></svg>`;
}

/** A status badge: icon (or the running pulse) + label on the role's tint. */
export function Badge({ view, title }) {
  if (!view) return null;
  return html`<span class="badge" data-role=${view.role} title=${title || view.title || view.detail || undefined}>
    ${view.pulse ? html`<span class="pulse" aria-hidden="true"></span>` : html`<${Icon} name=${view.icon} size="sm" />`}
    <span class="badge-label">${view.label}</span>
  </span>`;
}

/** A status line without a tint: icon + label, for dense rows and health lists. */
export function StatusText({ view }) {
  if (!view) return null;
  return html`<span class="status-text" data-role=${view.role}>
    ${view.pulse ? html`<span class="pulse" aria-hidden="true"></span>` : html`<${Icon} name=${view.icon} size="sm" />`}
    <span>${view.label}</span>
  </span>`;
}

export function Chip({ children, title, mono }) {
  return html`<span class=${mono ? 'chip chip-mono' : 'chip'} title=${title}>${children}</span>`;
}

/**
 * Job progress, never a percentage. Up to CELL_MAX tracks it is one cell per
 * track (completed, already on disk, failed, cancelled, remaining); above that
 * a segmented bar. Either way the counter text follows, then an opt-in hook
 * the base hides: .jprog-legend (a count per cell state), hidden from screen
 * readers because it repeats aria-valuetext.
 */
export function JobProgress({ job }) {
  const p = jobProgress(job);
  const cells = progressCells(job);
  const legend = progressLegend(job);
  const text = [
    html`<span key="text" class="jprog-text">${p.text}</span>`,
    legend
      ? html`<span key="legend" class="jprog-legend" aria-hidden="true">
          ${legend.map(
            (k) => html`<span key=${k.state} class="jprog-key" data-state=${k.state} data-role=${k.role || undefined} data-zero=${k.n === 0 ? '' : undefined}>
              <span class="jprog-key-n">${k.n}</span> <span class="jprog-key-label">${k.label}</span>
            </span>`,
          )}
        </span>`
      : null,
  ];
  if (cells) {
    return html`<div class="jprog" data-mode="cells" data-working=${p.working ? '' : undefined}>
      <div
        class="pcells"
        role="progressbar"
        aria-label="Tracks"
        aria-valuemin="0"
        aria-valuemax=${p.total}
        aria-valuenow=${p.done}
        aria-valuetext=${p.aria}
        style=${{ '--n': String(cells.length) }}
      >
        ${cells.map((c, i) => html`<span key=${i} class="pcell" data-state=${c.state} data-role=${c.role || undefined}></span>`)}
      </div>
      ${text}
    </div>`;
  }
  const seg = (role, n) =>
    n > 0 ? html`<span key=${role} class="pbar-seg" data-role=${role} style=${{ '--w': String(n / p.total) }}></span>` : null;
  return html`<div class="jprog" data-mode="bar" data-working=${p.working ? '' : undefined}>
    <div
      class=${p.indeterminate ? 'pbar is-indeterminate' : 'pbar'}
      role="progressbar"
      aria-label="Tracks"
      aria-valuemin="0"
      aria-valuemax=${p.total}
      aria-valuenow=${p.indeterminate ? undefined : p.done}
      aria-valuetext=${p.aria}
    >
      ${p.total > 0 ? [seg('success', p.completed), seg('skipped', p.skipped), seg('danger', p.failed), seg('neutral', p.cancelled)] : null}
    </div>
    ${text}
  </div>`;
}

/** Seven pipeline stage segments for one item; download and decrypt fill to their fraction. */
export function StageTrack({ item }) {
  const segs = stageSegments(item);
  return html`<div class="stages" role="img" aria-label=${stageText(segs)}>
    ${segs.map(
      (s) => html`<span key=${s.key} class="stage" data-state=${s.state} title=${s.state === 'unverified' ? `${s.label}: not verified` : s.label}>
        <span class="stage-fill" style=${{ '--f': String(s.fraction) }}></span>
      </span>`,
    )}
  </div>`;
}

/**
 * Apple artwork with its background color behind it while loading and on
 * failure (an expired pre-signed URL, for example), where a music glyph shows.
 */
export function Artwork({ url, bg, px, widths, sizes, className, eager }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [url]);
  const src = artworkUrl(url, px);
  const srcset = widths ? artworkSrcset(url, widths) : '';
  return html`<div class=${className ? `art ${className}` : 'art'} style=${bg ? { '--art-bg': bg } : undefined}>
    ${src && !failed
      ? html`<img
          src=${src}
          srcset=${srcset || undefined}
          sizes=${srcset ? sizes : undefined}
          loading=${eager ? 'eager' : 'lazy'}
          decoding="async"
          alt=""
          onError=${() => setFailed(true)}
        />`
      : html`<span class="art-glyph"><${Icon} name="music" /></span>`}
  </div>`;
}

/** Relative time with the absolute time as a tooltip. */
export function RelTime({ value }) {
  const now = useNow();
  if (!value) return html`<span class="muted">—</span>`;
  return html`<time datetime=${value} title=${absTime(value)}>${relTime(value, now)}</time>`;
}

export function Button({ variant = 'secondary', size, icon, children, className, ...rest }) {
  const cls = ['btn', `btn-${variant}`, size ? `btn-${size}` : '', !children ? 'btn-icon' : '', className || ''].filter(Boolean).join(' ');
  return html`<button type="button" class=${cls} ...${rest}>${icon ? html`<${Icon} name=${icon} />` : null}${children ? html`<span>${children}</span>` : null}</button>`;
}

export function LinkButton({ variant = 'secondary', size, icon, children, className, external, ...rest }) {
  const cls = ['btn', `btn-${variant}`, size ? `btn-${size}` : '', className || ''].filter(Boolean).join(' ');
  const ext = external ? { target: '_blank', rel: 'noopener noreferrer' } : {};
  return html`<a class=${cls} ...${ext} ...${rest}>${icon ? html`<${Icon} name=${icon} />` : null}<span>${children}</span>${external ? html`<${Icon} name="external" size="sm" />` : null}</a>`;
}

/**
 * A titled section: .card > .card-head (.card-title, .card-note,
 * .card-actions) + .card-body. `section` names it for the skin (data-section).
 */
export function Card({ title, actions, children, className, labelId, note, section }) {
  return html`<section class=${className ? `card ${className}` : 'card'} data-section=${section} aria-labelledby=${labelId}>
    ${title
      ? html`<header class="card-head">
          <h2 class="card-title" id=${labelId}>${title}</h2>
          ${note ? html`<span class="card-note">${note}</span>` : null}
          ${actions ? html`<div class="card-actions">${actions}</div>` : null}
        </header>`
      : null}
    <div class="card-body">${children}</div>
  </section>`;
}

const HEALTH_ICONS = { Backend: 'server', Wrapper: 'key', 'Library sync': 'rotate', Hooks: 'layers' };

/**
 * Health rows: .health-row[data-role] > .health-icon, .health-name,
 * .health-status (the badge), .health-detail.
 */
export function HealthList({ rows, wrapDetail }) {
  return html`<ul class="health-list">
    ${rows.map(
      ([name, view]) => html`<li key=${name} class="health-row" data-role=${view.role}>
        <span class="health-icon" aria-hidden="true"><${Icon} name=${HEALTH_ICONS[name] || 'info'} /></span>
        <span class="health-name">${name}</span>
        <span class="health-status"><${Badge} view=${view} /></span>
        ${view.detail ? html`<span class=${wrapDetail ? 'health-detail wrap-any' : 'health-detail'} title=${view.title || view.detail}>${view.detail}</span>` : null}
      </li>`,
    )}
  </ul>`;
}

/**
 * Label/value facts: dl.facts > .fact[data-key][data-role] > dt.fact-label +
 * dd.fact-value. A row is [key, label, value] or [key, label, value, role].
 */
export function Facts({ rows, className, label }) {
  return html`<dl class=${className ? `facts ${className}` : 'facts'} aria-label=${label}>
    ${rows.map(
      ([key, k, v, role]) =>
        html`<div key=${key} class="fact" data-key=${key} data-role=${role || undefined}><dt class="fact-label">${k}</dt><dd class="fact-value">${v}</dd></div>`,
    )}
  </dl>`;
}

export function Empty({ icon = 'info', title, children, action }) {
  return html`<div class="empty">
    <span class="empty-icon"><${Icon} name=${icon} /></span>
    <p class="empty-title">${title}</p>
    ${children ? html`<p class="empty-body">${children}</p>` : null}
    ${action || null}
  </div>`;
}

export function ErrorNote({ title = 'Something went wrong', error, onRetry }) {
  return html`<div class="error-note" role="alert">
    <span class="status-text" data-role="danger"><${Icon} name="circle-x" size="sm" /><span>${title}</span></span>
    ${error && error.message ? html`<p class="error-msg">${error.message}</p>` : null}
    ${onRetry ? html`<${Button} size="sm" icon="rotate" onClick=${onRetry}>Retry</${Button}>` : null}
  </div>`;
}

/** A quiet note for a widget whose endpoint this backend does not have. */
export function Unavailable({ what }) {
  return html`<p class="quiet-note"><${Icon} name="info" size="sm" /> ${what} is not available from this backend.</p>`;
}

export function Notice({ role = 'neutral', icon = 'info', children, action }) {
  return html`<div class="notice" data-role=${role}>
    <${Icon} name=${icon} />
    <div class="notice-body">${children}</div>
    ${action || null}
  </div>`;
}

export function Skeleton({ kind = 'text' }) {
  return html`<span class=${`skel skel-${kind}`} aria-hidden="true"></span>`;
}

/** Repeats a skeleton row `n` times inside a busy container. */
export function SkeletonRows({ n = 6, kind = 'row' }) {
  return html`<div class="skel-list" aria-busy="true" aria-label="Loading">
    ${Array.from({ length: n }, (_, i) => html`<${Skeleton} key=${i} kind=${kind} />`)}
  </div>`;
}

/** A group of toggle buttons; exactly one value is pressed. */
export function Segmented({ label, options, value, onChange, size }) {
  return html`<div class=${size ? `seg seg-${size}` : 'seg'} role="group" aria-label=${label}>
    ${options.map(
      (o) => html`<button
        key=${o.value}
        type="button"
        class="seg-btn"
        aria-pressed=${String(o.value === value)}
        title=${o.title || undefined}
        aria-label=${o.iconOnly ? o.label : undefined}
        onClick=${() => onChange(o.value)}
      >
        ${o.icon ? html`<${Icon} name=${o.icon} />` : null}
        ${o.iconOnly ? null : html`<span>${o.label}</span>`}
        ${o.count !== undefined ? html`<span class="seg-count">${o.count}</span>` : null}
      </button>`,
    )}
  </div>`;
}

export function CopyButton({ text, label = 'Copy' }) {
  const app = useApp();
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      app.toast({ message: 'Copied to clipboard' });
    } catch {
      app.toast({ message: 'Copy failed: the browser blocked clipboard access', role: 'danger' });
    }
  };
  return html`<${Button} variant="ghost" size="sm" icon="copy" aria-label=${label} title=${label} onClick=${copy} />`;
}

/** Key/value list; values may be any node. */
export function KV({ rows }) {
  return html`<dl class="kv">
    ${rows.filter(Boolean).map(
      ([k, v]) => html`<div class="kv-row" key=${k}><dt>${k}</dt><dd>${v === undefined || v === null || v === '' ? html`<span class="muted">—</span>` : v}</dd></div>`,
    )}
  </dl>`;
}
