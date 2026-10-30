// Command palette (Cmd/Ctrl+K) and the keyboard shortcut sheet.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listDownloads } from '../api.js';
import { paletteItems, resultCount } from '../lib/palette.js';
import { Icon, Button } from '../ui/components.js';
import { Dialog, useDialog } from '../ui/dialogs.js';
import { focusField } from '../ui/field-focus.js';
import { useDebounced, useMedia } from '../ui/hooks.js';

/** Whether this is an Apple platform, where the command key is ⌘. */
export const isMac = () => /mac|iphone|ipad/i.test((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || '');

// A touch screen has no keyboard hints to show, and a tab bar for the pages:
// the Navigate group shows there only once something is typed.
const TOUCH = '(hover: none)';

export function CommandPalette({ onClose, navigate, setTheme }) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  // q: the query these jobs answer.
  const [jobs, setJobs] = useState({ items: [], loading: false, q: '' });
  const debounced = useDebounced(query, 200);
  const touch = useMedia(TOUCH);
  const input = useRef(null);
  const panel = useRef(null);
  // Focus starts in the input, Tab stays in the panel, Escape closes it, and
  // focus returns to whatever had it when the palette opened (a command that
  // opens another page moves it to that page's heading instead).
  useDialog(panel, onClose);

  useEffect(() => {
    const q = debounced.trim();
    if (!q) {
      setJobs({ items: [], loading: false, q });
      return undefined;
    }
    let cancelled = false;
    setJobs((j) => ({ ...j, loading: true }));
    listDownloads({ q, limit: 8 }).then(
      (body) => !cancelled && setJobs({ items: body.downloads, loading: false, q }),
      () => !cancelled && setJobs({ items: [], loading: false, q }),
    );
    return () => {
      cancelled = true;
    };
  }, [debounced]);

  const items = paletteItems(query, jobs.items).filter((it) => !(touch && !query.trim() && it.group === 'Navigate'));
  // Announced once the job search for what was typed has answered.
  const settled = query.trim() !== '' && query.trim() === jobs.q && !jobs.loading;
  const current = Math.min(active, Math.max(0, items.length - 1));

  useEffect(() => setActive(0), [query, jobs.items.length]);

  const run = (it) => {
    if (!it) return;
    onClose();
    if (it.theme) setTheme(it.theme);
    else if (it.hash) {
      // Already at that address: no route change moves focus, so do it here.
      if (it.field && window.location.hash === it.hash) focusField(it.field);
      else navigate(it.hash);
    }
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
          placeholder="Search or paste a link"
          value=${query}
          onInput=${(e) => setQuery(e.currentTarget.value)}
          onKeyDown=${onKey}
        />
        <button type="button" class="btn btn-ghost palette-close" onClick=${onClose}>Cancel</button>
      </div>
      <p class="sr-only" role="status" aria-live="polite">${settled ? resultCount(items.length) : ''}</p>
      <ul id="palette-list" role="listbox" class="palette-list" aria-label="Results">
        ${rows}
        ${items.length === 0 && !jobs.loading ? html`<li role="presentation" class="palette-empty">No results</li>` : null}
        ${jobs.loading ? html`<li role="presentation" class="palette-empty">Searching jobs…</li>` : null}
      </ul>
      <div class="palette-foot" aria-hidden="true"><kbd>↑</kbd><kbd>↓</kbd> move <kbd>Enter</kbd> open <kbd>Esc</kbd> close</div>
    </div>
  </div>`;
}

// One shortcut row: keys are <kbd>, plain strings are the words between or
// around them ("then", "in a field").
const k = (key) => ({ key });

function shortcutGroups(mod) {
  const go = (key, label) => [[k('g'), ' then ', k(key)], label];
  return [
    [
      'Global',
      [
        [[k('n')], 'New download'],
        go('o', 'Overview'),
        go('q', 'Queue'),
        go('l', 'Logs'),
        [[k('g'), ' then ', k(','), ' or ', k('y')], 'Settings'],
        [[k(mod), ' ', k('K')], 'Command palette'],
        [[k('?')], 'This list'],
        [[k('Esc')], 'Close the drawer or dialog'],
      ],
    ],
    [
      'Lists',
      [
        [[k('/')], 'Focus the page filter'],
        [[k('j'), ' / ', k('k')], 'Next / previous row'],
        [[k('Enter')], 'Open the focused row (select it while selecting)'],
        [[k('x')], 'Select the focused job (Queue)'],
        [[k('Esc')], 'Leave selection'],
        [[k('Esc'), ' in a field'], 'Clear the search, then leave the field'],
      ],
    ],
    [
      'New download',
      [
        [['Paste a link'], 'New download with that link'],
        [[k(mod), ' ', k(mod === '⌘' ? 'Return' : 'Enter')], 'Download the links in the box'],
        [[k('↓'), ' or ', k(mod === '⌘' ? 'Return' : 'Enter'), ' in the box'], 'Go to the search results'],
        [[k('↑'), ' ', k('↓'), ' ', k('←'), ' ', k('→')], 'Move between search results'],
        [[k(mod === '⌘' ? 'Return' : 'Enter'), ' on a result'], 'Download it, or open the artist'],
      ],
    ],
    ['Settings', [[[k(mod), ' ', k('S')], 'Save settings']]],
  ];
}

export function ShortcutSheet({ onClose }) {
  const groups = shortcutGroups(isMac() ? '⌘' : 'Ctrl');
  return html`<${Dialog} name="shortcuts" className="sheet sheet-shortcuts" labelledBy="shortcuts-title" onClose=${onClose}>
    <div class="sheet-head">
      <h2 id="shortcuts-title" class="card-title">Keyboard shortcuts</h2>
      <${Button} variant="ghost" icon="x" aria-label="Close" onClick=${onClose} data-autofocus />
    </div>
    <div class="shortcut-body">
      <div class="shortcut-groups">
        ${groups.map(
          ([title, rows]) => html`<section key=${title} class="shortcut-group" aria-label=${title}>
            <h3 class="shortcut-group-title">${title}</h3>
            <dl class="kv">
              ${rows.map(
                ([keys, label]) => html`<div class="kv-row" key=${label}>
                  <dt class="shortcut-keys">${keys.map((p, i) => (typeof p === 'string' ? p : html`<kbd key=${i}>${p.key}</kbd>`))}</dt>
                  <dd>${label}</dd>
                </div>`,
              )}
            </dl>
          </section>`,
        )}
      </div>
    </div>
  </${Dialog}>`;
}
