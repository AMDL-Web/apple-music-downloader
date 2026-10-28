// Command palette (Cmd/Ctrl+K) and the keyboard shortcut sheet.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads, docsUrl } from '../api.js';
import { jobTitle, jobSubtitle, typeLabel } from '../lib/format.js';
import { buildHash, settingsHash } from '../lib/route.js';
import { Icon, Button } from '../ui/components.js';
import { Dialog, useDialog } from '../ui/dialogs.js';
import { useDebounced } from '../ui/hooks.js';
import { SKINS } from '../lib/prefs.js';

const STATIC_ITEMS = [
  { id: 'act-new', group: 'Actions', label: 'New download', icon: 'plus', hash: buildHash('new'), keys: 'n' },
  { id: 'nav-overview', group: 'Navigate', label: 'Overview', icon: 'grid', hash: buildHash('overview'), keys: 'g o' },
  { id: 'nav-queue', group: 'Navigate', label: 'Queue', icon: 'list', hash: buildHash('queue'), keys: 'g q' },
  { id: 'nav-showcase', group: 'Navigate', label: 'Showcase', icon: 'disc', hash: buildHash('showcase'), keys: 'g s' },
  { id: 'nav-logs', group: 'Navigate', label: 'Logs', icon: 'file-text', hash: buildHash('logs'), keys: 'g l' },
  { id: 'nav-system', group: 'Navigate', label: 'System', icon: 'server', hash: buildHash('system'), keys: 'g y' },
  { id: 'nav-settings', group: 'Navigate', label: 'Settings', icon: 'settings', hash: settingsHash(), keys: 'g ,' },
  { id: 'nav-docs', group: 'Navigate', label: 'API docs', icon: 'book', external: true },
  { id: 'f-failed', group: 'Filters', label: 'Show failed jobs', icon: 'circle-x', hash: buildHash('queue', { tab: 'failed' }) },
  { id: 'f-running', group: 'Filters', label: 'Show active jobs', icon: 'dot', hash: buildHash('queue') },
  { id: 'f-errors', group: 'Filters', label: 'Show error logs', icon: 'triangle-alert', hash: buildHash('logs', { level: 'warn,error' }) },
  { id: 'set-account', group: 'Settings', label: 'Apple Music account', icon: 'key', hash: settingsHash('account') },
  { id: 'set-library-sync', group: 'Settings', label: 'Library sync', icon: 'rotate', hash: settingsHash('library_sync') },
  { id: 'theme-system', group: 'Theme', label: 'Use system theme', icon: 'monitor', theme: 'system' },
  { id: 'theme-light', group: 'Theme', label: 'Use light theme', icon: 'sun', theme: 'light' },
  { id: 'theme-dark', group: 'Theme', label: 'Use dark theme', icon: 'moon', theme: 'dark' },
  ...SKINS.map((s) => ({ id: `skin-${s.value}`, group: 'Style', label: `Style: ${s.label}`, icon: 'palette', skin: s.value })),
];

export function CommandPalette({ onClose, navigate, setTheme, setSkin }) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const [jobs, setJobs] = useState({ items: [], loading: false });
  const debounced = useDebounced(query, 200);
  const input = useRef(null);
  const panel = useRef(null);
  // Focus starts in the input, Tab stays in the panel, Escape closes it, and
  // focus returns to whatever had it when the palette opened (a command that
  // opens another page moves it to that page's heading instead).
  useDialog(panel, onClose);

  useEffect(() => {
    const q = debounced.trim();
    if (!q) {
      setJobs({ items: [], loading: false });
      return undefined;
    }
    let cancelled = false;
    setJobs((j) => ({ ...j, loading: true }));
    listDownloads({ q, limit: 8 }).then(
      (body) => !cancelled && setJobs({ items: body.downloads, loading: false }),
      () => !cancelled && setJobs({ items: [], loading: false }),
    );
    return () => {
      cancelled = true;
    };
  }, [debounced]);

  const q = query.trim().toLowerCase();
  const items = [
    ...STATIC_ITEMS.filter((it) => !q || it.label.toLowerCase().includes(q) || it.group.toLowerCase().includes(q)),
    ...jobs.items.map((job) => ({
      id: `job-${job.id}`,
      group: 'Jobs',
      label: jobTitle(job),
      sub: [jobSubtitle(job), typeLabel(job.type)].filter(Boolean).join(' · '),
      icon: 'music',
      hash: buildHash('queue', {}, job.id),
      dir: 'auto',
    })),
  ];
  const current = Math.min(active, Math.max(0, items.length - 1));

  useEffect(() => setActive(0), [query, jobs.items.length]);

  const run = (it) => {
    if (!it) return;
    onClose();
    if (it.theme) setTheme(it.theme);
    else if (it.skin) setSkin(it.skin);
    else if (it.external) window.open(docsUrl(), '_blank', 'noopener,noreferrer');
    else if (it.hash) navigate(it.hash);
  };

  const onKey = (e) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setActive((a) => (items.length ? (Math.min(a, items.length - 1) + 1) % items.length : 0));
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      setActive((a) => (items.length ? (Math.min(a, items.length - 1) - 1 + items.length) % items.length : 0));
    } else if (e.key === 'Enter') {
      e.preventDefault();
      run(items[current]);
    } else if (e.key === 'Tab') {
      e.preventDefault();
    }
  };

  let lastGroup = null;
  const rows = [];
  items.forEach((it, i) => {
    if (it.group !== lastGroup) {
      lastGroup = it.group;
      rows.push(html`<li key=${`g-${it.group}`} role="presentation" class="palette-group">${it.group}</li>`);
    }
    rows.push(html`<li
      key=${it.id}
      id=${`pal-${i}`}
      role="option"
      aria-selected=${String(i === current)}
      class="palette-item"
      onMouseMove=${() => setActive(i)}
      onClick=${() => run(it)}
    >
      <${Icon} name=${it.icon} />
      <span class="palette-label" dir=${it.dir}>${it.label}</span>
      ${it.sub ? html`<span class="palette-sub" dir="auto">${it.sub}</span>` : null}
      ${it.keys ? html`<kbd class="palette-keys">${it.keys}</kbd>` : null}
      ${it.external ? html`<${Icon} name="external" size="sm" />` : null}
    </li>`);
  });

  return html`<div class="overlay-host">
    <div class="scrim" onClick=${onClose}></div>
    <div class="palette" role="dialog" aria-modal="true" aria-label="Command palette" ref=${panel}>
      <div class="palette-input">
        <${Icon} name="search" />
        <input
          ref=${input}
          data-autofocus
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="palette-list"
          aria-activedescendant=${items.length ? `pal-${current}` : undefined}
          aria-autocomplete="list"
          placeholder="Search jobs, pages and actions"
          value=${query}
          onInput=${(e) => setQuery(e.currentTarget.value)}
          onKeyDown=${onKey}
        />
      </div>
      <ul id="palette-list" role="listbox" class="palette-list" aria-label="Results">
        ${rows}
        ${items.length === 0 && !jobs.loading ? html`<li role="presentation" class="palette-empty">No results</li>` : null}
        ${jobs.loading ? html`<li role="presentation" class="palette-empty">Searching jobs…</li>` : null}
      </ul>
      <div class="palette-foot" aria-hidden="true"><kbd>↑</kbd><kbd>↓</kbd> move <kbd>Enter</kbd> open <kbd>Esc</kbd> close</div>
    </div>
  </div>`;
}

const SHORTCUTS = [
  ['n', 'New download'],
  ['g then o', 'Overview'],
  ['g then q', 'Queue'],
  ['g then s', 'Showcase'],
  ['g then l', 'Logs'],
  ['g then y', 'System'],
  ['g then ,', 'Settings'],
  ['/', 'Focus the page filter'],
  ['j / k', 'Next / previous row'],
  ['Enter', 'Open the focused row (select it while selecting)'],
  ['x', 'Select the focused job (Queue)'],
  ['Esc', 'Close the drawer or dialog, then leave selection'],
  ['Paste a link', 'New download with that link, outside a text field'],
  ['Cmd/Ctrl + K', 'Command palette'],
  ['?', 'This list'],
];

export function ShortcutSheet({ onClose }) {
  return html`<${Dialog} name="shortcuts" labelledBy="shortcuts-title" onClose=${onClose}>
    <div class="sheet-head">
      <h2 id="shortcuts-title" class="card-title">Keyboard shortcuts</h2>
      <${Button} variant="ghost" icon="x" aria-label="Close" onClick=${onClose} data-autofocus />
    </div>
    <dl class="kv">
      ${SHORTCUTS.map(([k, v]) => html`<div class="kv-row" key=${k}><dt><kbd>${k}</kbd></dt><dd>${v}</dd></div>`)}
    </dl>
    <p class="muted small">Single-key shortcuts are off while you type in a field.</p>
  </${Dialog}>`;
}
