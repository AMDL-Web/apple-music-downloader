// Shell chrome: primary navigation (sidebar and tab bar), the connection
// indicator, the theme control and the page banners.

import { html } from '../ui/preact.js';
import { docsUrl } from '../api.js';
import { buildHash } from '../lib/route.js';
import { connectionView } from '../lib/status.js';
import { clockTime, count } from '../lib/format.js';
import { Icon, Button, Notice } from '../ui/components.js';
import { useClock } from '../ui/hooks.js';

export const NAV = [
  { page: 'overview', label: 'Overview', icon: 'grid', key: 'o' },
  { page: 'queue', label: 'Queue', icon: 'list', key: 'q' },
  { page: 'showcase', label: 'Showcase', icon: 'disc', key: 's' },
  { page: 'logs', label: 'Logs', icon: 'file-text', key: 'l' },
  { page: 'system', label: 'System', icon: 'server', key: 'y' },
];

export const THEMES = ['system', 'light', 'dark'];

export function Sidebar({ route, counts }) {
  const active = counts ? counts.queued + counts.running : 0;
  return html`<nav class="sidebar" aria-label="Primary">
    <div class="brand">
      <span class="brand-mark" aria-hidden="true"><${Icon} name="music" /></span>
      <span class="brand-name">AMDL</span>
    </div>
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
    <div class="sidebar-foot">
      <a class="nav-item" href=${docsUrl()} target="_blank" rel="noopener noreferrer" title="API docs">
        <${Icon} name="book" size="nav" />
        <span class="nav-label">API docs</span>
        <${Icon} name="external" size="sm" />
      </a>
    </div>
  </nav>`;
}

export function TabBar({ route }) {
  return html`<nav class="tabbar" aria-label="Primary">
    ${NAV.map(
      (n) => html`<a key=${n.page} class="tab" href=${buildHash(n.page)} aria-current=${route.page === n.page ? 'page' : undefined}>
        <${Icon} name=${n.icon} size="nav" />
        <span class="tab-label">${n.label}</span>
      </a>`,
    )}
  </nav>`;
}

export function ConnectionIndicator({ conn, onRetry }) {
  const waiting = conn.state === 'reconnecting' || conn.state === 'offline';
  const now = useClock(waiting ? 1000 : 0);
  const view = connectionView(conn.state);
  const secs = waiting && conn.retryAt ? Math.max(0, Math.ceil((conn.retryAt - now) / 1000)) : null;
  return html`<span class="conn" data-role=${view.role} title="Live updates">
    <span class="conn-dot" aria-hidden="true"></span>
    <span class="conn-label">${view.label}${secs !== null && conn.state === 'reconnecting' ? ` · ${secs}s` : ''}</span>
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
