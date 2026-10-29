// Shell: the downloads feed shared by every page, connection state, theme,
// keyboard shortcuts, the global link paste, toasts and live announcements.
// The hash router and its leave guard are in ui/router.js.

import { html, useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo } from './ui/preact.js';
import { listDownloads, getSystem, getConfig, feedStreamUrl, parseData } from './api.js';
import { buildHash, newDownloadHash, newDownloadUrls } from './lib/route.js';
import { extractLinks } from './lib/links.js';
import { THEME_KEY, SKIN_KEY, normalizeTheme, normalizeSkin, readPref, writePref } from './lib/prefs.js';
import { applyFeedMessage, EMPTY_COUNTS } from './lib/store.js';
import { StreamClient, createCoalescer, backoffDelay } from './lib/stream.js';
import { jobTitle, typeLabel, count, pageSummary } from './lib/format.js';
import { isTerminal } from './lib/status.js';
import { BURST_MS, endsWatch, finishedToast } from './lib/watch.js';
import { AppContext, Icon, Button, LinkButton } from './ui/components.js';
import { isDialogOpen } from './ui/dialogs.js';
import { loadSprite, spriteLoaded } from './ui/icons.js';
import { useHashRouter } from './ui/router.js';
import { OverviewPage } from './views/overview.js';
import { QueuePage } from './views/queue.js';
import { ShowcasePage } from './views/showcase.js';
import { LogsPage } from './views/logs.js';
import { SystemPage } from './views/system.js';
import { DownloadPage } from './views/download.js';
import { SettingsPage } from './views/settings.js';
import { JobDrawer } from './views/drawer.js';
import { CommandPalette, ShortcutSheet } from './views/palette.js';
import { NAV, NEW_DOWNLOAD, Sidebar, TabBar, MoreSheet, ConnectionIndicator, ThemeControl, SkinControl, Banners } from './views/shell.js';

const TITLES = {
  overview: 'Overview',
  queue: 'Queue',
  showcase: 'Showcase',
  logs: 'Logs',
  system: 'System',
  new: 'New download',
  settings: 'Settings',
  notfound: 'Not found',
};

// Pages a g-chord reaches: the nav plus New download.
const CHORDS = [...NAV, NEW_DOWNLOAD];

const storage = () => {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
};

/** Reads the stored theme; storage can be unavailable (private mode, blocked site data). */
export const loadTheme = () => readPref(storage(), THEME_KEY, normalizeTheme);

/** Applies a theme to <html>: "system" follows prefers-color-scheme. */
export function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'light' || theme === 'dark') root.setAttribute('data-theme', theme);
  else root.removeAttribute('data-theme');
}

/** Reads the stored skin; the default when storage is unavailable or the value unknown. */
export const loadSkin = () => readPref(storage(), SKIN_KEY, normalizeSkin);

/** Applies a skin to <html data-skin>, which scopes css/skins/<skin>.css. */
export function applySkin(skin) {
  document.documentElement.setAttribute('data-skin', normalizeSkin(skin));
}

const ANNOUNCE_GAP_MS = 5000;
// How long a toast stays: errors longer, a toast with an action (Open) a
// little longer; a sticky one until dismissed or the page changes. Every
// timer pauses while the pointer or focus is on the toasts.
const TOAST_MS = 5000;
const TOAST_ACTION_MS = 8000;
const TOAST_DANGER_MS = 10000;

const isTyping = (el) =>
  el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);

/**
 * Escape in a field outside a dialog: a search field with text is cleared
 * first (an input event, so the page's own state follows); otherwise the
 * field gives up focus, so j/k and the other single keys work again. Other
 * fields (the New download box, settings) are never cleared.
 */
function leaveField(e) {
  const el = e.target;
  if (el.tagName === 'INPUT' && (el.type === 'search' || el.hasAttribute('data-filter')) && el.value !== '') {
    e.preventDefault();
    const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
    setter.call(el, '');
    el.dispatchEvent(new Event('input', { bubbles: true }));
    return;
  }
  e.preventDefault();
  el.blur();
}

export function App() {
  const { route, navigate, resumeLeave, setLeaveGuard } = useHashRouter();
  const [feed, setFeed] = useState({ counts: null, cursor: null });
  const [conn, setConn] = useState({ state: 'connecting', retryAt: null, downSince: null });
  const [epoch, setEpoch] = useState(0);
  const [simulate, setSimulate] = useState(false);
  const [defaultStorefront, setDefaultStorefront] = useState('');
  const [theme, setThemeState] = useState(loadTheme);
  const [skin, setSkinState] = useState(loadSkin);
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

  const setTheme = useCallback((t) => {
    setThemeState(t);
    writePref(storage(), THEME_KEY, t);
  }, []);
  useEffect(() => applyTheme(theme), [theme]);

  const setSkin = useCallback((s) => {
    const v = normalizeSkin(s);
    setSkinState(v);
    writePref(storage(), SKIN_KEY, v);
  }, []);
  useEffect(() => applySkin(skin), [skin]);

  // The browser chrome (theme-color) follows the page background of the
  // active skin and theme; with the system theme it follows the OS switch.
  useEffect(() => {
    const sync = () => {
      let meta = document.querySelector('meta[name="theme-color"]');
      if (!meta) {
        meta = document.createElement('meta');
        meta.setAttribute('name', 'theme-color');
        document.head.append(meta);
      }
      meta.setAttribute('content', getComputedStyle(document.body).backgroundColor);
    };
    sync();
    const mq = window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null;
    if (theme !== 'system' || !mq) return undefined;
    mq.addEventListener('change', sync);
    return () => mq.removeEventListener('change', sync);
  }, [theme, skin]);

  // Toasts: {id, message, role, sticky, action: {label, hash}}.
  const toastTimers = useRef(new Map());
  const toastsHeld = useRef(false);
  const toastsRef = useRef(null);
  const dismissToast = useCallback((id) => {
    const t = toastTimers.current.get(id);
    if (t && t.timer !== null) clearTimeout(t.timer);
    toastTimers.current.delete(id);
    setToasts((list) => list.filter((x) => x.id !== id));
  }, []);
  const toast = useCallback(
    (t) => {
      const id = `${Date.now()}-${Math.random()}`;
      const role = t.role || 'neutral';
      setToasts((list) => [...list.slice(-3), { id, message: t.message, role, sticky: Boolean(t.sticky), action: t.action || null }]);
      if (t.sticky) return;
      const ms = role === 'danger' ? TOAST_DANGER_MS : t.action ? TOAST_ACTION_MS : TOAST_MS;
      const timer = toastsHeld.current ? null : setTimeout(() => dismissToast(id), ms);
      toastTimers.current.set(id, { timer, left: ms, at: Date.now() });
    },
    [dismissToast],
  );
  useEffect(() => {
    const el = toastsRef.current;
    if (!el) return undefined;
    let hover = false;
    const hold = () => {
      if (toastsHeld.current) return;
      toastsHeld.current = true;
      for (const t of toastTimers.current.values()) {
        if (t.timer === null) continue;
        clearTimeout(t.timer);
        t.timer = null;
        t.left -= Date.now() - t.at;
      }
    };
    const release = () => {
      if (!toastsHeld.current || hover || el.contains(document.activeElement)) return;
      toastsHeld.current = false;
      for (const [id, t] of toastTimers.current) {
        t.at = Date.now();
        t.timer = setTimeout(() => dismissToast(id), Math.max(1500, t.left));
      }
    };
    const enter = () => {
      hover = true;
      hold();
    };
    const leave = () => {
      hover = false;
      release();
    };
    const focusOut = () => setTimeout(release, 0);
    el.addEventListener('mouseenter', enter);
    el.addEventListener('mouseleave', leave);
    el.addEventListener('focusin', hold);
    el.addEventListener('focusout', focusOut);
    return () => {
      el.removeEventListener('mouseenter', enter);
      el.removeEventListener('mouseleave', leave);
      el.removeEventListener('focusin', hold);
      el.removeEventListener('focusout', focusOut);
    };
  }, [dismissToast]);
  // A sticky toast is about the page it was raised on.
  useEffect(() => setToasts((list) => (list.some((t) => t.sticky) ? list.filter((t) => !t.sticky) : list)), [route.page, route.jobId]);

  // Jobs a page asked to watch (app.watchJobs, after New download submits
  // them): when they end, one visible toast per burst (lib/watch.js).
  const watched = useRef(new Set());
  const recentEnded = useRef(new Map());
  const endedBurst = useRef([]);
  const burstTimer = useRef(null);
  const routeRef = useRef(route);
  routeRef.current = route;
  const flushEnded = useCallback(() => {
    burstTimer.current = null;
    const jobs = endedBurst.current;
    endedBurst.current = [];
    if (jobs.length) toast(finishedToast(jobs, routeRef.current));
  }, [toast]);
  const noteEnded = useCallback(
    (job) => {
      watched.current.delete(job.id);
      endedBurst.current.push(job);
      if (burstTimer.current === null) burstTimer.current = setTimeout(flushEnded, BURST_MS);
    },
    [flushEnded],
  );
  const watchJobs = useCallback(
    (ids) => {
      for (const id of ids || []) {
        if (!id) continue;
        // The feed can report the end before the submit answer arrives.
        const done = recentEnded.current.get(id);
        if (done) noteEnded(done);
        else watched.current.add(id);
      }
      if (watched.current.size > 500) watched.current = new Set([...watched.current].slice(-500));
    },
    [noteEnded],
  );
  const noteEndedRef = useRef(noteEnded);
  noteEndedRef.current = noteEnded;

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
        if (isTerminal(job.status)) {
          recentEnded.current.set(job.id, job);
          if (recentEnded.current.size > 50) recentEnded.current.delete(recentEnded.current.keys().next().value);
        } else recentEnded.current.delete(job.id);
        if (watched.current.has(job.id) && endsWatch(job)) {
          // Its toast is announced through the toasts' own live region.
          noteEndedRef.current(job);
        } else if (prev && prev !== job.status && (job.status === 'completed' || job.status === 'failed')) {
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
          // Live again after an outage: the feed replays what it missed from
          // the cursor, and a new epoch tells every page to refetch what it
          // could not load meanwhile (error panels, defaults, previews).
          onReconnect: () => {
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
    // The device came back online: retry now instead of at the end of the
    // backoff, and let pages refetch what failed while requests could not
    // leave (the open stream itself may never have noticed).
    const onOnline = () => {
      retryRef.current();
      setEpoch((e) => e + 1);
    };
    window.addEventListener('online', onOnline);
    boot();
    return () => {
      window.removeEventListener('online', onOnline);
      if (burstTimer.current !== null) clearTimeout(burstTimer.current);
      stopped = true;
      if (bootTimer !== null) clearTimeout(bootTimer);
      coalescer.cancel();
      if (client) client.stop();
    };
  }, []);

  // A sprite that could not load at startup is tried again after an outage.
  useEffect(() => {
    if (epoch > 0 && !spriteLoaded()) loadSprite();
  }, [epoch]);

  // Simulate mode comes from /system; a backend without it still reports it in /config.
  useEffect(() => {
    let cancelled = false;
    getSystem().then(
      (s) => {
        if (cancelled) return;
        setSimulate(Boolean(s.simulate));
        setDefaultStorefront(typeof s.default_storefront === 'string' ? s.default_storefront : '');
      },
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
    document.title = running > 0 ? `${count(running)} running · ${page} · AMDL` : `${page} · AMDL`;
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
  // A layout effect: it reads the opener at commit, before the drawer's own
  // effect (a child's, so it runs first) moves focus into the panel.
  const prevJob = useRef(route.jobId);
  useLayoutEffect(() => {
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

  // The palette and the sheets belong to the page they opened over: any
  // route change (a link, Back, a shortcut) closes them. No history entry is
  // pushed for a sheet, which would fight the leave guard's bookkeeping.
  // Only the page and the drawer count: pages rewrite their query as filters
  // or the New download box change (debounced), which may land after Cmd+K.
  useEffect(() => setOverlay(null), [route.page, route.jobId]);

  // Keyboard shortcuts.
  const gPending = useRef(0);
  useEffect(() => {
    const onKey = (e) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        // Never over a modal dialog: it keeps focus and its Escape.
        if (overlay === 'palette') setOverlay(null);
        else if (!isDialogOpen()) setOverlay('palette');
        return;
      }
      if (e.key === 'Escape') {
        if (overlay) {
          e.preventDefault();
          setOverlay(null);
        } else if (isTyping(e.target)) {
          leaveField(e);
        } else if (route.jobId) {
          e.preventDefault();
          closeDrawer();
        }
        return;
      }
      if (e.metaKey || e.ctrlKey || e.altKey || isTyping(e.target) || overlay || isDialogOpen()) return;
      if (gPending.current && Date.now() - gPending.current < 1200) {
        gPending.current = 0;
        const target = CHORDS.find((n) => n.key === e.key);
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
      if (e.key === NEW_DOWNLOAD.key) {
        e.preventDefault();
        navigate(buildHash('new'));
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

  // Pasting Apple Music links outside a text field opens New download with
  // them; on that page they join the links already in ?url=.
  useEffect(() => {
    const onPaste = (e) => {
      if (isTyping(e.target) || overlay || isDialogOpen() || !e.clipboardData) return;
      const links = extractLinks(e.clipboardData.getData('text/plain'));
      if (links.length === 0) return;
      e.preventDefault();
      const onNew = route.page === 'new';
      const urls = onNew ? [...new Set([...newDownloadUrls(route.params), ...links])] : links;
      navigate(newDownloadHash(urls), { replace: onNew });
    };
    window.addEventListener('paste', onPaste);
    return () => window.removeEventListener('paste', onPaste);
  }, [overlay, route.page, route.params, navigate]);

  const ctx = useMemo(
    () => ({
      navigate,
      toast,
      watchJobs,
      announce,
      subscribeFeed,
      counts,
      conn,
      epoch,
      simulate,
      defaultStorefront,
      setLeaveGuard,
      resumeLeave,
      retryFeed: () => retryRef.current(),
    }),
    [navigate, toast, watchJobs, announce, subscribeFeed, counts, conn, epoch, simulate, defaultStorefront, setLeaveGuard, resumeLeave],
  );

  const title = TITLES[route.page] || 'AMDL';
  const summary = pageSummary(route.page, counts);
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
    case 'new':
      page = html`<${DownloadPage} route=${route} />`;
      break;
    case 'settings':
      page = html`<${SettingsPage} route=${route} />`;
      break;
    default:
      page = html`<div class="card"><div class="card-body"><p>This page does not exist. <a class="link" href=${buildHash('overview')}>Go to the overview</a>.</p></div></div>`;
  }

  return html`<${AppContext.Provider} value=${ctx}>
    <div class="shell">
      <button type="button" class="skip-link" onClick=${() => mainRef.current && mainRef.current.focus()}>Skip to content</button>
      <${Sidebar} route=${route} counts=${counts} />
      <div class="main-col">
        <header class="topbar">
          <div class="topbar-right">
            <${ConnectionIndicator} conn=${conn} onRetry=${() => retryRef.current()} />
            <button type="button" class="search-trigger" onClick=${() => setOverlay('palette')} aria-label="Search and commands" aria-keyshortcuts="Meta+K Control+K">
              <${Icon} name="search" />
              <span class="search-trigger-text">Search jobs…</span>
              <kbd class="search-trigger-kbd">⌘K</kbd>
            </button>
            <${SkinControl} skin=${skin} setSkin=${setSkin} />
            <${ThemeControl} theme=${theme} setTheme=${setTheme} />
          </div>
        </header>
        <main id="main" class="main" tabindex="-1" ref=${mainRef} data-page=${route.page}>
          <div class="page-head">
            <h1 class="page-title" tabindex="-1" ref=${h1Ref}>${title}</h1>
            ${summary ? html`<p class="page-sub">${summary}</p>` : null}
            ${route.page === 'overview'
              ? html`<div class="page-actions">
                  <${LinkButton} variant="primary" icon="plus" className="page-new" href=${buildHash('new')}>${NEW_DOWNLOAD.label}</${LinkButton}>
                </div>`
              : null}
          </div>
          <${Banners} simulate=${simulate} conn=${conn} onRetry=${() => retryRef.current()} />
          ${page}
        </main>
      </div>
      <${TabBar} route=${route} moreOpen=${overlay === 'more'} onMore=${() => setOverlay('more')} />
    </div>
    ${route.jobId ? html`<${JobDrawer} jobId=${route.jobId} onClose=${closeDrawer} />` : null}
    ${overlay === 'palette' ? html`<${CommandPalette} onClose=${() => setOverlay(null)} navigate=${navigate} setTheme=${setTheme} setSkin=${setSkin} />` : null}
    ${overlay === 'shortcuts' ? html`<${ShortcutSheet} onClose=${() => setOverlay(null)} />` : null}
    ${overlay === 'more' ? html`<${MoreSheet} route=${route} onClose=${() => setOverlay(null)} />` : null}
    <div class="toasts" role="status" aria-live="polite" ref=${toastsRef}>
      ${toasts.map(
        (t) => html`<div key=${t.id} class="toast" data-role=${t.role}>
          <${Icon} name=${t.role === 'danger' ? 'circle-x' : t.role === 'success' ? 'circle-check' : t.role === 'warning' ? 'triangle-alert' : 'info'} />
          <span class="toast-msg">${t.message}</span>
          ${t.action
            ? html`<${LinkButton} variant="ghost" size="sm" className="toast-action" href=${t.action.hash} onClick=${() => dismissToast(t.id)}>${t.action.label}</${LinkButton}>`
            : null}
          <${Button} variant="ghost" size="sm" icon="x" aria-label="Dismiss" onClick=${() => dismissToast(t.id)} />
        </div>`,
      )}
    </div>
    <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">${liveText}</div>
  </${AppContext.Provider}>`;
}
