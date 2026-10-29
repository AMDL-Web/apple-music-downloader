// Shell chrome: primary navigation (sidebar, tab bar and its More sheet),
// the New download entry point, the connection indicator, the theme control
// and the page banners.

import { html } from '../ui/preact.js';
import { buildHash } from '../lib/route.js';
import { connectionView } from '../lib/status.js';
import { clockTime, count } from '../lib/format.js';
import { Icon, Button, Notice } from '../ui/components.js';
import { Dialog } from '../ui/dialogs.js';
import { useClock } from '../ui/hooks.js';
import { THEMES } from '../lib/prefs.js';

export const NAV = [
  { page: 'overview', label: 'Overview', icon: 'grid', key: 'o' },
  { page: 'queue', label: 'Queue', icon: 'list', key: 'q' },
  { page: 'showcase', label: 'Showcase', icon: 'disc', key: 's' },
  { page: 'logs', label: 'Logs', icon: 'file-text', key: 'l' },
  { page: 'system', label: 'System', icon: 'server', key: 'y' },
  { page: 'settings', label: 'Settings', icon: 'settings', key: ',' },
];

/** The primary action: its own button above the nav, `n` or `g n`. */
export const NEW_DOWNLOAD = { page: 'new', label: 'New download', icon: 'plus', key: 'n' };

/** Pages the phone tab bar reaches through its More sheet. */
export const MORE_PAGES = ['logs', 'system', 'settings'];

export function Sidebar({ route, counts }) {
  const active = counts ? counts.queued + counts.running : 0;
  return html`<nav class="sidebar" aria-label="Primary">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true"><${Icon} name="music" /></span>
      <span class="brand-text">
        <span class="brand-name">AMDL</span>
        <span class="brand-sub">Apple Music Downloader</span>
      </span>
    </div>
    <a
      class="btn btn-primary sidebar-new"
      href=${buildHash('new')}
      aria-current=${route.page === 'new' ? 'page' : undefined}
      aria-label=${NEW_DOWNLOAD.label}
      aria-keyshortcuts="n"
      title=${`${NEW_DOWNLOAD.label} (n)`}
    >
      <${Icon} name="plus" />
      <span class="sidebar-new-label">${NEW_DOWNLOAD.label}</span>
    </a>
    <ul class="nav">
      ${NAV.map(
        (n) => html`<li key=${n.page}>
          <a class="nav-item" href=${buildHash(n.page)} aria-current=${route.page === n.page ? 'page' : undefined} title=${n.label}>
            <${Icon} name=${n.icon} size="nav" />
            <span class="nav-label">${n.label}</span>
            ${n.page === 'queue' && active > 0 ? html`<span class="nav-badge" aria-label=${`${active} active`}>${count(active)}</span>` : null}
          </a>
        </li>`,
      )}
    </ul>
  </nav>`;
}

const navItem = (page) => NAV.find((n) => n.page === page);

/**
 * The phone tab bar: Overview, Queue, New download (center, primary),
 * Showcase and More, a button that opens MoreSheet. More is marked current
 * while one of MORE_PAGES is shown.
 */
export function TabBar({ route, moreOpen, onMore }) {
  const tab = (n) => html`<a key=${n.page} class="tab" data-page=${n.page} href=${buildHash(n.page)} aria-current=${route.page === n.page ? 'page' : undefined}>
    <${Icon} name=${n.icon} size="nav" />
    <span class="tab-label">${n.label}</span>
  </a>`;
  return html`<nav class="tabbar" aria-label="Primary">
    ${tab(navItem('overview'))}
    ${tab(navItem('queue'))}
    <a class="tab tab-new" data-page="new" href=${buildHash('new')} aria-current=${route.page === 'new' ? 'page' : undefined} aria-label=${NEW_DOWNLOAD.label}>
      <span class="tab-new-mark"><${Icon} name="plus" size="nav" /></span>
      <span class="tab-label">New</span>
    </a>
    ${tab(navItem('showcase'))}
    <button
      type="button"
      class="tab tab-more"
      aria-haspopup="dialog"
      aria-expanded=${String(Boolean(moreOpen))}
      aria-current=${MORE_PAGES.includes(route.page) ? 'page' : undefined}
      onClick=${onMore}
    >
      <${Icon} name="more" size="nav" />
      <span class="tab-label">More</span>
    </button>
  </nav>`;
}

/**
 * The tab bar's More sheet: a bottom sheet listing MORE_PAGES, the current
 * one marked. Choosing a page closes it; so do Escape, the scrim and Close.
 */
export function MoreSheet({ route, onClose }) {
  const items = MORE_PAGES.map(navItem);
  const focus = MORE_PAGES.includes(route.page) ? route.page : MORE_PAGES[0];
  return html`<${Dialog} name="more" className="sheet sheet-more" placement="bottom" labelledBy="more-title" onClose=${onClose}>
    <div class="sheet-head">
      <h2 id="more-title" class="card-title">More</h2>
      <${Button} variant="ghost" icon="x" aria-label="Close" onClick=${onClose} />
    </div>
    <ul class="more-list">
      ${items.map((n) => {
        const current = route.page === n.page;
        return html`<li key=${n.page}>
          <a
            class="more-item"
            data-page=${n.page}
            href=${buildHash(n.page)}
            aria-current=${current ? 'page' : undefined}
            data-autofocus=${n.page === focus ? '' : undefined}
            onClick=${onClose}
          >
            <${Icon} name=${n.icon} size="nav" />
            <span class="more-label">${n.label}</span>
            ${current ? html`<span class="more-current" aria-hidden="true">Current</span>` : null}
            <${Icon} name="chevron-right" size="sm" />
          </a>
        </li>`;
      })}
    </ul>
  </${Dialog}>`;
}

export function ConnectionIndicator({ conn, onRetry }) {
  const waiting = conn.state === 'reconnecting' || conn.state === 'offline';
  const now = useClock(waiting ? 1000 : 0);
  const view = connectionView(conn.state);
  const secs = waiting && conn.retryAt ? Math.max(0, Math.ceil((conn.retryAt - now) / 1000)) : null;
  // The number is the wait before the next attempt, so it says so.
  const label = conn.state === 'reconnecting' && secs ? `Retrying in ${secs} s` : view.label;
  return html`<span class="conn" data-role=${view.role} data-state=${conn.state} title=${`Live updates: ${label}`}>
    <span class="conn-dot" aria-hidden="true"></span>
    <span class="conn-label">${label}</span>
    ${conn.state === 'offline' && onRetry ? html`<${Button} size="sm" variant="ghost" onClick=${onRetry}>Retry</${Button}>` : null}
  </span>`;
}

const THEME_OPTIONS = [
  { value: 'system', label: 'System theme', icon: 'monitor' },
  { value: 'light', label: 'Light theme', icon: 'sun' },
  { value: 'dark', label: 'Dark theme', icon: 'moon' },
];

export function ThemeControl({ theme, setTheme }) {
  const i = THEMES.indexOf(theme);
  const current = THEME_OPTIONS[i < 0 ? 0 : i];
  const next = THEMES[(i + 1) % THEMES.length];
  return html`<div class="theme">
    <div class="seg seg-icons" role="group" aria-label="Theme">
      ${THEME_OPTIONS.map(
        (o) => html`<button key=${o.value} type="button" class="seg-btn" aria-pressed=${String(o.value === theme)} aria-label=${o.label} title=${o.label} onClick=${() => setTheme(o.value)}>
          <${Icon} name=${o.icon} />
        </button>`,
      )}
    </div>
    <button type="button" class="btn btn-ghost btn-icon theme-cycle" aria-label=${`${current.label}; switch to ${next}`} onClick=${() => setTheme(next)}>
      <${Icon} name=${current.icon} />
    </button>
  </div>`;
}

export function Banners({ simulate, conn, onRetry }) {
  const down = conn.state === 'reconnecting' || conn.state === 'offline';
  const now = useClock(down ? 1000 : 0);
  const downFor = down && conn.downSince ? now - conn.downSince : 0;
  return html`<div class="banners">
    ${simulate
      ? html`<${Notice} role="warning" icon="triangle-alert">Simulate mode: transfers and decryption are simulated and no files are written.</${Notice}>`
      : null}
    ${conn.state === 'offline'
      ? html`<${Notice}
          role="danger"
          icon="circle-x"
          action=${html`<${Button} size="sm" icon="rotate" onClick=${onRetry}>Retry now</${Button}>`}
        >Disconnected${conn.downSince ? ` since ${clockTime(new Date(conn.downSince), false)}` : ''}. The numbers shown may be stale.</${Notice}>`
      : downFor >= 10000
        ? html`<${Notice} role="warning" icon="triangle-alert">Live updates interrupted. Reconnecting…</${Notice}>`
        : null}
  </div>`;
}
