// Shell: hash router, the downloads feed shared by every page, connection
// state, theme, keyboard shortcuts, toasts and live announcements.

import { html, useState, useEffect, useRef, useCallback, useMemo } from './ui/preact.js';
import { listDownloads, getSystem, getConfig, feedStreamUrl, docsUrl, parseData } from './api.js';
import { parseHash, buildHash } from './lib/route.js';
import { applyFeedMessage, EMPTY_COUNTS } from './lib/store.js';
import { StreamClient, createCoalescer, backoffDelay } from './lib/stream.js';
import { connectionView } from './lib/status.js';
import { jobTitle, typeLabel, clockTime, count } from './lib/format.js';
import { AppContext, Icon, Button, Notice } from './ui/components.js';
import { useClock } from './ui/hooks.js';
import { OverviewPage } from './views/overview.js';
import { QueuePage } from './views/queue.js';
import { ShowcasePage } from './views/showcase.js';
import { LogsPage } from './views/logs.js';
import { SystemPage } from './views/system.js';
import { JobDrawer } from './views/drawer.js';
import { CommandPalette, ShortcutSheet } from './views/palette.js';

const NAV = [
  { page: 'overview', label: 'Overview', icon: 'grid', key: 'o' },
  { page: 'queue', label: 'Queue', icon: 'list', key: 'q' },
  { page: 'showcase', label: 'Showcase', icon: 'disc', key: 's' },
  { page: 'logs', label: 'Logs', icon: 'file-text', key: 'l' },
  { page: 'system', label: 'System', icon: 'server', key: 'y' },
];
const TITLES = { overview: 'Overview', queue: 'Queue', showcase: 'Showcase', logs: 'Logs', system: 'System', notfound: 'Not found' };

const THEME_KEY = 'amdl.console.theme';
const THEMES = ['system', 'light', 'dark'];

/** Reads the stored theme; storage can be unavailable (private mode, blocked site data). */
export function loadTheme() {
  try {
    const v = window.localStorage.getItem(THEME_KEY);
    return THEMES.includes(v) ? v : 'system';
  } catch {
    return 'system';
  }
}

/** Applies a theme to <html>: "system" follows prefers-color-scheme. */
export function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'light' || theme === 'dark') root.setAttribute('data-theme', theme);
  else root.removeAttribute('data-theme');
}

function saveTheme(theme) {
  try {
    window.localStorage.setItem(THEME_KEY, theme);
  } catch {
    // Storage blocked: the choice lasts for this page view only.
  }
}

const ANNOUNCE_GAP_MS = 5000;
const TOAST_MS = 5000;

const isTyping = (el) =>
  el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);

export function App() {
  const [route, setRoute] = useState(() => parseHash(window.location.hash));
  const [feed, setFeed] = useState({ counts: null, cursor: null });
  const [conn, setConn] = useState({ state: 'connecting', retryAt: null, downSince: null });
  const [epoch, setEpoch] = useState(0);
  const [simulate, setSimulate] = useState(false);
  const [theme, setThemeState] = useState(loadTheme);
  const [toasts, setToasts] = useState([]);
  const [liveText, setLiveText] = useState('');
  const [overlay, setOverlay] = useState(null);
  const listeners = useRef(new Set());
  const retryRef = useRef(() => {});
  const lastStatus = useRef(new Map());
  const announceQueue = useRef([]);
  const announceTimer = useRef(null);
  const lastAnnounce = useRef(0);
  const opener = useRef(null);
  const mainRef = useRef(null);
  const h1Ref = useRef(null);

  const navigate = useCallback((hash, opts = {}) => {
    if (opts.replace) {
      window.history.replaceState(null, '', hash);
      setRoute(parseHash(hash));
    } else if (window.location.hash !== hash) {
      window.location.hash = hash;
    }
  }, []);

  useEffect(() => {
    const on = () => setRoute(parseHash(window.location.hash));
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);

  const setTheme = useCallback((t) => {
    setThemeState(t);
    saveTheme(t);
  }, []);
  useEffect(() => applyTheme(theme), [theme]);

  const toast = useCallback((t) => {
    const id = `${Date.now()}-${Math.random()}`;
    const role = t.role || 'neutral';
    setToasts((list) => [...list.slice(-3), { id, message: t.message, role }]);
    if (!t.sticky && role !== 'danger') setTimeout(() => setToasts((list) => list.filter((x) => x.id !== id)), TOAST_MS);
  }, []);
  const dismissToast = (id) => setToasts((list) => list.filter((x) => x.id !== id));

  const flushAnnouncements = useCallback(() => {
    announceTimer.current = null;
    const items = announceQueue.current;
    announceQueue.current = [];
    if (items.length === 0) return;
    lastAnnounce.current = Date.now();
    setLiveText(items.length > 3 ? `${items.slice(0, 3).join('. ')}. And ${items.length - 3} more.` : `${items.join('. ')}.`);
  }, []);
  const announce = useCallback(
    (text) => {
      announceQueue.current.push(text);
      if (announceTimer.current !== null) return;
      const wait = Math.max(0, lastAnnounce.current + ANNOUNCE_GAP_MS - Date.now());
      announceTimer.current = setTimeout(flushAnnouncements, wait);
    },
    [flushAnnouncements],
  );

  const subscribeFeed = useCallback((fn) => {
    listeners.current.add(fn);
    return () => listeners.current.delete(fn);
  }, []);

  // The downloads feed: one snapshot for counts and the cursor, then the
  // stream from that cursor. Every page subscribes to its batches.
  useEffect(() => {
    let stopped = false;
    let client = null;
    let bootTimer = null;
    let attempt = 0;
    let downSince = null;
    const noteTransitions = (batch) => {
      for (const msg of batch) {
        const job = msg.job;
        if (!job) continue;
        const prev = lastStatus.current.get(job.id);
        lastStatus.current.set(job.id, job.status);
        if (prev && prev !== job.status && (job.status === 'completed' || job.status === 'failed')) {
          announce(`${typeLabel(job.type)} ${jobTitle(job)} ${job.status}`);
        }
      }
      if (lastStatus.current.size > 1000) {
        const drop = lastStatus.current.size - 1000;
        let i = 0;
        for (const k of lastStatus.current.keys()) {
          if (i++ >= drop) break;
          lastStatus.current.delete(k);
        }
      }
    };
    const coalescer = createCoalescer((batch) => {
      setFeed((f) => batch.reduce(applyFeedMessage, f));
      for (const fn of listeners.current) fn(batch);
      noteTransitions(batch);
    }, 100);
    const refreshCounts = () =>
      listDownloads({ limit: 1 }).then(
        (body) => setFeed((f) => ({ ...f, counts: { ...EMPTY_COUNTS, ...body.status_counts } })),
        () => {},
      );
    const boot = async () => {
      bootTimer = null;
      try {
        const body = await listDownloads({ limit: 1 });
        if (stopped) return;
        for (const j of body.downloads || []) lastStatus.current.set(j.id, j.status);
        setFeed({ counts: { ...EMPTY_COUNTS, ...body.status_counts }, cursor: body.event_cursor ?? null });
        if (downSince !== null) setEpoch((e) => e + 1);
        client = new StreamClient({
          url: feedStreamUrl(),
          cursor: body.event_cursor ?? null,
          onEvent: (ev) => {
            if (ev.type !== 'download_upserted' && ev.type !== 'download_deleted') return;
            const msg = parseData(ev);
            if (msg) coalescer.push(msg);
          },
          onState: (s) => setConn(s),
          onResync: () => {
            refreshCounts();
            setEpoch((e) => e + 1);
          },
          onEnd: ({ status }) => setConn({ state: 'offline', retryAt: null, downSince: Date.now(), endStatus: status }),
        });
        retryRef.current = () => client.retryNow();
        client.start();
      } catch {
        if (stopped) return;
        if (downSince === null) downSince = Date.now();
        const delay = backoffDelay(attempt++);
        const state = Date.now() - downSince >= 60000 ? 'offline' : 'reconnecting';
        setConn({ state, retryAt: Date.now() + delay, downSince });
        bootTimer = setTimeout(boot, delay);
        retryRef.current = () => {
          if (bootTimer !== null) {
            clearTimeout(bootTimer);
            boot();
          }
        };
      }
    };
    boot();
    return () => {
      stopped = true;
      if (bootTimer !== null) clearTimeout(bootTimer);
      coalescer.cancel();
      if (client) client.stop();
    };
  }, []);

  // Simulate mode comes from /system; a backend without it still reports it in /config.
  useEffect(() => {
    let cancelled = false;
    getSystem().then(
      (s) => !cancelled && setSimulate(Boolean(s.simulate)),
      (err) => {
        if (cancelled || err.status !== 404) return;
        getConfig().then(
          (c) => !cancelled && setSimulate(Boolean(c && c.config && c.config.simulate && c.config.simulate.enabled)),
          () => {},
        );
      },
    );
    return () => {
      cancelled = true;
    };
  }, [epoch]);

  const counts = feed.counts;
  const running = counts ? counts.running : 0;
  useEffect(() => {
    const page = TITLES[route.page] || 'AMDL';
    document.title = running > 0 ? `${count(running)} running · AMDL` : `${page} · AMDL`;
  }, [running, route.page]);

  // Focus: a new page focuses its heading; the drawer returns focus to the row that opened it.
  const prevPage = useRef(route.page);
  useEffect(() => {
    if (prevPage.current !== route.page) {
      window.scrollTo(0, 0);
      if (!route.jobId && h1Ref.current) h1Ref.current.focus({ preventScroll: true });
    }
    prevPage.current = route.page;
  }, [route.page]);
  const prevJob = useRef(route.jobId);
  useEffect(() => {
    if (route.jobId && !prevJob.current) opener.current = document.activeElement;
    if (!route.jobId && prevJob.current) {
      const el = opener.current;
      opener.current = null;
      if (el && el.isConnected && typeof el.focus === 'function') el.focus({ preventScroll: true });
      else if (mainRef.current) mainRef.current.focus({ preventScroll: true });
    }
    prevJob.current = route.jobId;
    document.documentElement.classList.toggle('has-overlay', Boolean(route.jobId));
  }, [route.jobId]);

  const closeDrawer = useCallback(() => navigate(buildHash(route.page, route.params)), [route.page, route.params, navigate]);

  // Keyboard shortcuts.
  const gPending = useRef(0);
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        setOverlay((o) => (o === 'palette' ? null : 'palette'));
        return;
      }
      if (e.key === 'Escape') {
        if (overlay) {
          e.preventDefault();
          setOverlay(null);
        } else if (route.jobId) {
          e.preventDefault();
          closeDrawer();
        }
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target) || overlay) return;
      if (gPending.current && Date.now() - gPending.current < 1200) {
        gPending.current = 0;
        const target = NAV.find((n) => n.key === e.key);
        if (target) {
          e.preventDefault();
          navigate(buildHash(target.page));
        }
        return;
      }
      if (e.key === 'g') {
        gPending.current = Date.now();
        return;
      }
      if (e.key === '?') {
        e.preventDefault();
        setOverlay('shortcuts');
        return;
      }
      if (e.key === '/') {
        const field = document.querySelector('main [data-filter]');
        if (field) {
          e.preventDefault();
          field.focus();
          field.select();
        }
        return;
      }
      if (e.key === 'j' || e.key === 'k') {
        const scope = document.querySelector('.drawer') || document.querySelector('main');
        if (!scope) return;
        const rows = Array.from(scope.querySelectorAll('[data-row]')).filter((r) => r.offsetParent !== null);
        if (rows.length === 0) return;
        e.preventDefault();
        const at = rows.findIndex((r) => r === document.activeElement || r.contains(document.activeElement));
        const next = at < 0 ? 0 : Math.max(0, Math.min(rows.length - 1, at + (e.key === 'j' ? 1 : -1)));
        const row = rows[next];
        const target = row.matches('a, button') ? row : row.querySelector('a, button') || row;
        target.focus();
        row.scrollIntoView({ block: 'nearest' });
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [overlay, route.jobId, closeDrawer, navigate]);

  const ctx = useMemo(
    () => ({
      navigate,
      toast,
      announce,
      subscribeFeed,
      counts,
      conn,
      epoch,
      simulate,
      retryFeed: () => retryRef.current(),
    }),
    [navigate, toast, announce, subscribeFeed, counts, conn, epoch, simulate],
  );

  const title = TITLES[route.page] || 'AMDL';
  let page;
  switch (route.page) {
    case 'overview':
      page = html`<${OverviewPage} />`;
      break;
    case 'queue':
      page = html`<${QueuePage} route=${route} />`;
      break;
    case 'showcase':
      page = html`<${ShowcasePage} route=${route} />`;
      break;
    case 'logs':
      page = html`<${LogsPage} key=${'logs'} route=${route} />`;
      break;
    case 'system':
      page = html`<${SystemPage} />`;
      break;
    default:
      page = html`<div class="card"><p>This page does not exist. <a class="link" href=${buildHash('overview')}>Go to the overview</a>.</p></div>`;
  }

  return html`<${AppContext.Provider} value=${ctx}>
    <div class="shell">
      <button type="button" class="skip-link" onClick=${() => mainRef.current && mainRef.current.focus()}>Skip to content</button>
      <${Sidebar} route=${route} counts=${counts} />
      <div class="main-col">
        <header class="topbar">
          <h1 class="page-title" tabindex="-1" ref=${h1Ref}>${title}</h1>
          <div class="topbar-right">
            <${ConnectionIndicator} conn=${conn} onRetry=${() => retryRef.current()} />
            <button type="button" class="search-trigger" onClick=${() => setOverlay('palette')} aria-label="Search and commands" aria-keyshortcuts="Meta+K Control+K">
              <${Icon} name="search" />
              <span class="search-trigger-text">Search jobs…</span>
              <kbd class="search-trigger-kbd">⌘K</kbd>
            </button>
            <${ThemeControl} theme=${theme} setTheme=${setTheme} />
          </div>
        </header>
        <${Banners} simulate=${simulate} conn=${conn} onRetry=${() => retryRef.current()} />
        <main id="main" class="main" tabindex="-1" ref=${mainRef}>${page}</main>
      </div>
      <${TabBar} route=${route} />
    </div>
    ${route.jobId ? html`<${JobDrawer} jobId=${route.jobId} onClose=${closeDrawer} />` : null}
    ${overlay === 'palette' ? html`<${CommandPalette} onClose=${() => setOverlay(null)} navigate=${navigate} setTheme=${setTheme} />` : null}
    ${overlay === 'shortcuts' ? html`<${ShortcutSheet} onClose=${() => setOverlay(null)} />` : null}
    <div class="toasts" role="status" aria-live="polite">
      ${toasts.map(
        (t) => html`<div key=${t.id} class="toast" data-role=${t.role}>
          <${Icon} name=${t.role === 'danger' ? 'circle-x' : t.role === 'success' ? 'circle-check' : t.role === 'warning' ? 'triangle-alert' : 'info'} />
          <span class="toast-msg">${t.message}</span>
          <${Button} variant="ghost" size="sm" icon="x" aria-label="Dismiss" onClick=${() => dismissToast(t.id)} />
        </div>`,
      )}
    </div>
    <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">${liveText}</div>
  </${AppContext.Provider}>`;
}

function Sidebar({ route, counts }) {
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

function TabBar({ route }) {
  return html`<nav class="tabbar" aria-label="Primary">
    ${NAV.map(
      (n) => html`<a key=${n.page} class="tab" href=${buildHash(n.page)} aria-current=${route.page === n.page ? 'page' : undefined}>
        <${Icon} name=${n.icon} size="nav" />
        <span class="tab-label">${n.label}</span>
      </a>`,
    )}
  </nav>`;
}

function ConnectionIndicator({ conn, onRetry }) {
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

function ThemeControl({ theme, setTheme }) {
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

function Banners({ simulate, conn, onRetry }) {
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
