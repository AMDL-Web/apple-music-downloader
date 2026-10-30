// Shell: connection state, theme, keyboard shortcuts, the global link paste,
// toasts and live announcements. The downloads feed is in ui/feed.js, the hash
// router and its leave guard are in ui/router.js.

import { html, useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo } from './ui/preact.js';
import { buildHash, newDownloadHash, newDownloadUrls, settingsSection, settingsField } from './lib/route.js';
import { sectionById } from './lib/settings-sections.js';
import { extractLinks } from './lib/links.js';
import { THEME_KEY, normalizeTheme, readPref, writePref } from './lib/prefs.js';
import { count, pageSummary } from './lib/format.js';

// Pages whose head has a live summary line: the line is there before the counts are.
const SUB_PAGES = ['overview', 'queue'];
import { AppContext, Icon } from './ui/components.js';
import { isDialogOpen } from './ui/dialogs.js';
import { useFieldFocus } from './ui/field-focus.js';
import { useToasts, useOfflineToasts, ToastStack } from './ui/toasts.js';
import { loadSprite, spriteLoaded } from './ui/icons.js';
import { useHashRouter } from './ui/router.js';
import { useFeed } from './ui/feed.js';
import { useStatus, statusEpoch } from './ui/system-status.js';
import { OverviewPage } from './views/overview.js';
import { QueuePage } from './views/queue.js';
import { LogsPage } from './views/logs.js';
import { DownloadPage } from './views/download.js';
import { SettingsPage } from './views/settings.js';
import { KeysToggle } from './views/settings-keys.js';
import { JobDrawer } from './views/drawer.js';
import { CommandPalette, ShortcutSheet, isMac } from './views/palette.js';
import { NAV, NEW_DOWNLOAD, Sidebar, TabBar, MoreSheet, ConnectionIndicator, SimulateTag, ThemeControl, Banners } from './views/shell.js';

const TITLES = {
  overview: 'Overview',
  queue: 'Queue',
  logs: 'Logs',
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

// The palette shortcut as this platform writes it.
const PALETTE_KEYS = isMac() ? '⌘K' : 'Ctrl K';

const isTyping = (el) =>
  el && (el.tagName === 'INPUT' || el.tagName === 'TEXTAREA' || el.tagName === 'SELECT' || el.isContentEditable);

/**
 * Escape in a field outside a dialog: a search field with text is cleared
 * first (an input event, so the page's own state follows); otherwise the
 * field gives up focus, so j/k and the other single keys work again. Other
 * fields are never cleared here; the New download box clears itself as
 * one undo step (views/download.js).
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
  const [theme, setThemeState] = useState(loadTheme);
  const [overlay, setOverlay] = useState(null);
  const opener = useRef(null);
  const mainRef = useRef(null);
  const h1Ref = useRef(null);

  const setTheme = useCallback((t) => {
    setThemeState(t);
    writePref(storage(), THEME_KEY, t);
  }, []);
  useEffect(() => applyTheme(theme), [theme]);

  // The browser chrome (theme-color) follows the page background of the
  // active theme; with the system theme it follows the OS switch.
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
  }, [theme]);

  const { toasts, toast, dismissToast, toastsRef, toastsHeld } = useToasts(route);

  const { feed, conn, epoch, retryRef, subscribeFeed, watchJobs, announce, liveText } = useFeed({ toast, route });
  useOfflineToasts(conn, toasts, dismissToast, toastsHeld);

  // A sprite that could not load at startup is tried again after an outage.
  useEffect(() => {
    if (epoch > 0 && !spriteLoaded()) loadSprite();
  }, [epoch]);

  // After an outage every status read is suspect (ui/system-status.js).
  useEffect(() => {
    if (epoch > 0) statusEpoch();
  }, [epoch]);

  // Simulate mode comes from /system; a backend without it still reports it in /config.
  const { system } = useStatus(['system']);
  const sys = system && system.data;
  const noSystem = Boolean(system && system.error && system.error.status === 404 && !sys);
  const { config } = useStatus(noSystem ? ['config'] : []);
  const cfg = config && config.data && config.data.config;
  const simulate = sys ? Boolean(sys.simulate) : Boolean(cfg && cfg.simulate && cfg.simulate.enabled);
  const defaultStorefront = sys && typeof sys.default_storefront === 'string' ? sys.default_storefront : '';

  const counts = feed.counts;
  const running = counts ? counts.running : 0;
  // An open Settings section leads its tab's title: "Storage & files · Settings · AMDL".
  const askedSection = route.page === 'settings' && (settingsSection(route.params) || settingsField(route.params)) ? sectionById(settingsSection(route.params), settingsField(route.params)).label : '';
  useEffect(() => {
    const page = [askedSection, TITLES[route.page] || 'AMDL'].filter(Boolean).join(' · ');
    document.title = running > 0 ? `${count(running)} running · ${page} · AMDL` : `${page} · AMDL`;
  }, [running, route.page, askedSection]);

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

  // After the heading focus above, so a palette result for a setting lands on its field.
  useFieldFocus(route);

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
        const target = CHORDS.find((n) => n.key === e.key || n.alias === e.key);
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
    case 'logs':
      page = html`<${LogsPage} key=${'logs'} route=${route} />`;
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
            ${simulate ? html`<${SimulateTag} />` : null}
            <${ConnectionIndicator} conn=${conn} onRetry=${() => retryRef.current()} />
            <button type="button" class="search-trigger" onClick=${() => setOverlay('palette')} aria-label="Search, paste a link or run a command" aria-keyshortcuts="Meta+K Control+K">
              <${Icon} name="search" />
              <span class="search-trigger-text">Search or paste a link…</span>
              <kbd class="search-trigger-kbd">${PALETTE_KEYS}</kbd>
            </button>
            <${ThemeControl} theme=${theme} setTheme=${setTheme} />
          </div>
        </header>
        <main id="main" class="main" tabindex="-1" ref=${mainRef} data-page=${route.page}>
          <div class="page-head">
            <h1 class="page-title" tabindex="-1" ref=${h1Ref}>${title}</h1>
            ${summary || SUB_PAGES.includes(route.page) ? html`<p class="page-sub">${summary || '\u00a0'}</p>` : null}
            ${route.page === 'settings' ? html`<div class="page-actions"><${KeysToggle} /></div>` : null}
          </div>
          <${Banners} conn=${conn} onRetry=${() => retryRef.current()} />
          ${page}
        </main>
      </div>
      <${TabBar} route=${route} moreOpen=${overlay === 'more'} onMore=${() => setOverlay('more')} />
    </div>
    ${route.jobId ? html`<${JobDrawer} jobId=${route.jobId} onClose=${closeDrawer} />` : null}
    ${overlay === 'palette' ? html`<${CommandPalette} onClose=${() => setOverlay(null)} navigate=${navigate} setTheme=${setTheme} />` : null}
    ${overlay === 'shortcuts' ? html`<${ShortcutSheet} onClose=${() => setOverlay(null)} />` : null}
    ${overlay === 'more' ? html`<${MoreSheet} route=${route} onClose=${() => setOverlay(null)} />` : null}
    <${ToastStack} toasts=${toasts} toastsRef=${toastsRef} dismissToast=${dismissToast} />
    <div class="sr-only" role="status" aria-live="polite" aria-atomic="true">${liveText}</div>
  </${AppContext.Provider}>`;
}
