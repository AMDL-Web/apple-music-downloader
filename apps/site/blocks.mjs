// Pieces of HTML the page template puts together: escaping, icons, code
// blocks with their copy buttons, and tab lists. None of it depends on the
// content or the repository the site points at.

// ---- text ------------------------------------------------------------------

export const esc = (s) =>
  String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

// ---- code blocks -----------------------------------------------------------

export function codeLines(lines) {
  return lines
    .map((line) => {
      if (line.startsWith('$ ')) return `<span class="c-p" aria-hidden="true">$</span> ${esc(line.slice(2))}`;
      if (line.startsWith('PS> ')) return `<span class="c-p" aria-hidden="true">PS&gt;</span> ${esc(line.slice(4))}`;
      if (line.startsWith('# ')) return `<span class="c-c">${esc(line)}</span>`;
      if (/^\s*[[{}\]"]/.test(line)) return `<span class="c-o">${json(line)}</span>`;
      if (/^\s/.test(line)) return esc(line);
      return line === '' ? '' : `<span class="c-o">${esc(line)}</span>`;
    })
    .join('\n');
}

// Colors keys, strings and numbers of one JSON line; "…" marks elided parts.
function json(line) {
  return esc(line).replace(/(&quot;(?:[^&]|&(?!quot;))*?&quot;)(\s*:)?|(-?\b\d+(?:\.\d+)?\b)/g, (m, str, colon, num) => {
    if (str) return colon ? `<span class="c-k">${str}</span>${colon}` : `<span class="c-s">${str}</span>`;
    return `<span class="c-n">${num}</span>`;
  });
}

// The commands of a block, for its copy button: prompt lines, each with the
// indented lines a trailing backslash continues it onto.
export function commands(lines) {
  const out = [];
  let open = false;
  for (const line of lines) {
    if (/^(\$|PS>) /.test(line)) out.push(line.replace(/^(\$|PS>) /, ''));
    else if (open && /^\s/.test(line)) out[out.length - 1] += `\n${line}`;
    else {
      open = false;
      continue;
    }
    open = line.endsWith('\\');
  }
  return out.join('\n');
}

// ---- icons -----------------------------------------------------------------

const svg = (body, extra = '') =>
  `<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"${extra}>${body}</svg>`;
export const ICON = {
  // Octicons mark-github (MIT).
  github:
    '<svg viewBox="0 0 16 16" width="18" height="18" fill="currentColor" aria-hidden="true"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg>',
  sun: svg('<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>'),
  moon: svg('<path d="M20 14.5A8.5 8.5 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>'),
  monitor: svg('<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>'),
  copy: svg('<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3"/>', ' class="i-copy"'),
  check: svg('<polyline points="5 12.5 10 17.5 19 7"/>', ' class="i-check"'),
  arrow: svg('<line x1="5" y1="12" x2="19" y2="12"/><polyline points="13 6 19 12 13 18"/>'),
  out: svg('<line x1="7" y1="17" x2="17" y2="7"/><polyline points="8 7 17 7 17 16"/>'),
  link: svg('<path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1"/><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1"/>'),
  folder: svg('<path d="M3.5 7.5a2 2 0 0 1 2-2h4l2 2h7a2 2 0 0 1 2 2v7.5a2 2 0 0 1-2 2h-13a2 2 0 0 1-2-2z"/>'),
  note: svg('<path d="M9 18V6l10-2v12"/><circle cx="6.5" cy="18" r="2.5"/><circle cx="16.5" cy="16" r="2.5"/>'),
  plug: svg('<path d="M9 3v5M15 3v5M6.5 8h11v3.5a5.5 5.5 0 0 1-11 0zM12 17v4"/>'),
  bolt: svg('<path d="M13 3L5 13.5h6L10 21l8-10.5h-6z"/>'),
  menu: svg('<path d="M4 7h16M4 12h16M4 17h16"/>'),
  chevronLeft: svg('<polyline points="14.5 6 8.5 12 14.5 18"/>'),
  chevronRight: svg('<polyline points="9.5 6 15.5 12 9.5 18"/>'),
  chevronDown: svg('<polyline points="6 9.5 12 15.5 18 9.5"/>'),
  laptop: svg('<rect x="5" y="5" width="14" height="10" rx="1.5"/><path d="M2.5 18.5h19"/>'),
  grid: svg('<rect x="4" y="4" width="7" height="7" rx="1"/><rect x="13" y="4" width="7" height="7" rx="1"/><rect x="4" y="13" width="7" height="7" rx="1"/><rect x="13" y="13" width="7" height="7" rx="1"/>'),
  terminal: svg('<rect x="3" y="4.5" width="18" height="15" rx="2"/><polyline points="7 9.5 10 12 7 14.5"/><path d="M12.5 15h4"/>'),
  container: svg('<rect x="3.5" y="5" width="17" height="14" rx="1.5"/><path d="M3.5 9.5h17M3.5 14.5h17"/>'),
  book: svg('<path d="M4 5.5A1.5 1.5 0 0 1 5.5 4H11v16H5.5A1.5 1.5 0 0 1 4 18.5z"/><path d="M20 5.5A1.5 1.5 0 0 0 18.5 4H13v16h5.5a1.5 1.5 0 0 0 1.5-1.5z"/>'),
  globe: svg('<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.6 3.8 5.6 3.8 9s-1.3 6.4-3.8 9c-2.5-2.6-3.8-5.6-3.8-9S9.5 5.6 12 3z"/>'),
  search: svg('<circle cx="11" cy="11" r="6.5"/><path d="M16 16l4.5 4.5"/>'),
};

/** An icon at another size and stroke width (cards use 1.5, buttons 1.75). */
export const sized = (icon, size, stroke) => {
  let out = icon.replace('width="18" height="18"', `width="${size}" height="${size}"`);
  if (stroke) out = out.replace('stroke-width="1.75"', `stroke-width="${stroke}"`);
  return out;
};

export const copyButton = (ui, text) =>
  `<button class="copy" type="button" data-copy="${esc(text)}" data-copied="${esc(ui.copied)}" aria-label="${esc(ui.copy)}">${ICON.copy}${ICON.check}</button>`;

export const codeBlock = (ui, lines, { label = '', copy = true, wrap = false } = {}) =>
  `<div class="code${label ? '' : ' code-solo'}${wrap ? ' code-wrap' : ''}">${label ? `<div class="code-bar"><span>${esc(label)}</span>${copy ? copyButton(ui, commands(lines)) : ''}</div>` : ''}<pre tabindex="0"><code>${codeLines(lines)}</code></pre>${!label && copy ? copyButton(ui, commands(lines).length ? commands(lines) : lines.join('\n')) : ''}</div>`;

// A tab list over panels, drawn with the first one open so the page does not
// move when site.js wires it up. Without scripts, NOSCRIPT_TABS shows every
// panel, one after another, and no tab list, and hides the controls that need
// a script: the copy buttons, the theme switch and the stage arrows. Options:
// cls goes on the outer div, listCls on the tab list, between is HTML placed
// after the tab list; an item may give labelHtml (used as is) for its label.
export const tabs = (label, items, { cls = '', listCls = '', between = '' } = {}) =>
  `<div class="tabs${cls ? ` ${cls}` : ''}"><div class="tablist${listCls ? ` ${listCls}` : ''}" role="tablist" aria-label="${esc(label)}">${items
    .map((t, i) => `<button type="button" role="tab" id="tab-${t.id}" aria-controls="panel-${t.id}" aria-selected="${i === 0}"${i ? ' tabindex="-1"' : ''}>${t.labelHtml ?? esc(t.label)}</button>`)
    .join('')}</div>${between}${items.map((t, i) => `<div class="panel" id="panel-${t.id}" role="tabpanel" aria-labelledby="tab-${t.id}"${i ? ' hidden' : ''}>${t.html}</div>`).join('')}</div>`;

// Its style is listed in the page's Content-Security-Policy, so the page
// keeps it apart as well.
export const NOSCRIPT_STYLE = '.tablist,.copy,.theme,.stage-arrow{display:none}.panel[hidden]{display:block!important}.panel+.panel{margin-top:24px}';
export const NOSCRIPT_TABS = { style: NOSCRIPT_STYLE, html: `<noscript><style>${NOSCRIPT_STYLE}</style></noscript>` };
