// The status of one Settings domain (lib/domains.js domainView), drawn two
// ways: a board tile that links to its section, and the head of the
// section's card. Both keep fixed tracks in every state: text changes
// inside its box, a slot with nothing yet holds its room, and nothing below
// moves when an answer arrives.

import { html, useState } from '../ui/preact.js';
import { Icon, Badge, Button } from '../ui/components.js';
import { clockTime } from '../lib/format.js';
import { domainLabel } from '../lib/domains.js';
import { settingsHash } from '../lib/route.js';

/** The section's glyph on a tile; solid in its role for a warning or danger. */
const DomainIcon = ({ icon, role }) => html`<span class="domain-icon" data-role=${role || undefined} aria-hidden="true"><${Icon} name=${icon} /></span>`;

function Chips({ items }) {
  return html`<span class="viz-chips">
    ${items.map((c) => html`<span key=${c.label} class="viz-chip">${c.label}</span>`)}
  </span>`;
}

function Shares({ parts, label }) {
  return html`<span class="viz-split" role=${parts.length ? 'img' : undefined} aria-label=${parts.length ? label : undefined} aria-hidden=${parts.length ? undefined : 'true'}>
    ${parts.map((p) => html`<span key=${p.key} class="viz-seg" data-tone=${p.tone} style=${{ '--w': String(p.share) }}></span>`)}
  </span>`;
}

function Meter({ fraction, role, label, valuetext }) {
  const f = Math.max(0, Math.min(1, fraction || 0));
  return html`<span class="viz-meter-row">
    <span class="viz-meter" role="meter" aria-valuemin="0" aria-valuemax="100" aria-valuenow=${String(Math.round(f * 100))} aria-valuetext=${valuetext}>
      <span class="viz-meter-fill" data-role=${role || undefined} style=${{ '--f': String(f) }}></span>
    </span>
    ${label ? html`<span class="viz-meter-label">${label}</span>` : null}
  </span>`;
}

function Cells({ mode, total, filled, fraction }) {
  if (mode === 'meter') return html`<${Meter} fraction=${fraction} role="info" valuetext=${`${Math.round(fraction * 100)}% of workers busy`} />`;
  return html`<span class="viz-cells" role="img" aria-label=${`${filled} of ${total} workers busy`}>
    ${Array.from({ length: total }, (_, i) => html`<span key=${i} class="viz-cell" data-on=${i < filled ? '' : undefined}></span>`)}
  </span>`;
}

function Bars({ buckets, label }) {
  const max = Math.max(1, ...buckets);
  return html`<span class="viz-bars" role="img" aria-label=${label}>
    ${buckets.map((n, i) => html`<span key=${i} class="viz-bar" data-zero=${n === 0 ? '' : undefined} style=${{ '--v': String(n / max) }}></span>`)}
  </span>`;
}

const CHECK_ICONS = { true: 'circle-check', false: 'circle-x', null: 'circle-help' };

function Checks({ items }) {
  return html`<span class="viz-checks">
    ${items.map(
      (c) => html`<span key=${c.label} class="viz-check" data-role=${c.role} title=${c.title}>
        <${Icon} name=${CHECK_ICONS[String(c.ok)]} size="sm" /><span>${c.label}</span>
      </span>`,
    )}
  </span>`;
}

function Viz({ viz, note }) {
  if (!viz) return null;
  switch (viz.kind) {
    case 'chips':
      return html`<${Chips} items=${viz.items} />`;
    case 'shares':
      return html`<${Shares} parts=${viz.parts} label=${note ? note.text : ''} />`;
    case 'meter':
      return html`<${Meter} ...${viz} />`;
    case 'cells':
      return html`<${Cells} ...${viz} />`;
    case 'bars':
      return viz.buckets.length ? html`<${Bars} buckets=${viz.buckets} label=${viz.label} />` : null;
    case 'checks':
      return html`<${Checks} items=${viz.items} />`;
    default:
      return null;
  }
}

/** Whether `viz` draws anything: chips with no items or bars with no hours do not. */
const drawsViz = (viz) => Boolean(viz) && !(viz.kind === 'chips' && !viz.items.length) && !(viz.kind === 'bars' && !viz.buckets.length);

function Figure({ view, loading }) {
  if (loading) return html`<span class="skel skel-figure" aria-hidden="true"></span>`;
  const f = view.figure;
  if (!f) return null;
  return html`<span class="domain-value" data-word=${f.word ? '' : undefined} title=${f.title || undefined}>${f.value}</span>
    ${f.unit ? html`<span class="domain-unit">${f.unit}</span>` : null}
    ${f.second ? html`<span class="domain-second">${f.second}</span>` : null}`;
}

/** The note line: the reason or the context, "As of 14:02 ·" first while the values are stale. */
function Note({ view, loading, linked }) {
  if (loading) return html`<span class="skel skel-lh skel-note" aria-hidden="true"></span>`;
  const n = view.note;
  const asOf = view.asOf ? html`<span class="domain-asof"><${Icon} name="clock" size="sm" />As of ${clockTime(new Date(view.asOf).toISOString(), false).slice(0, 5)} · </span>` : null;
  if (!n && !asOf) return null;
  return html`${asOf}<span class="domain-note-text" data-role=${n && n.role ? n.role : undefined} title=${n ? n.title || n.text : undefined}>${n ? n.text : ''}</span>
    ${n && n.link ? (linked ? html` · <a class="link" href=${n.link.href}>${n.link.label}</a>` : html`<span class="domain-note-link"> · ${n.link.label}</span>`) : null}`;
}

/** The tag, or its room held while the first answer is out. */
function Tag({ view, loading }) {
  if (loading) return html`<span class="badge skel-badge domain-tag" aria-hidden="true"><${Icon} name="circle-dashed" size="sm" /><span class="badge-label skel skel-text"> </span></span>`;
  return view.tag ? html`<span class="domain-tag"><${Badge} view=${view.tag} /></span>` : null;
}

/**
 * One domain: `variant` 'tile' (a link to its section, on the board) or
 * 'head' (the section card's head, with `actions` at the end of its first
 * row). `view` is null for a section without status (a plain head).
 */
export function DomainHead({ section, view, variant = 'head', actions, titleId }) {
  const loading = Boolean(view) && view.state === 'loading';
  const role = view && view.role ? view.role : undefined;
  const vizKind = view && view.viz ? view.viz.kind : undefined;
  if (variant === 'tile') {
    return html`<a class="status-tile" href=${settingsHash(section.id)} data-domain=${section.id} data-role=${role} aria-label=${domainLabel(view, section.label)}>
      <span class="tile-top">
        <${DomainIcon} icon=${section.icon} role=${role} />
        <span class="tile-label">${section.label}</span>
        <${Tag} view=${view} loading=${loading} />
      </span>
      <span class="domain-figure"><${Figure} view=${view} loading=${loading} /></span>
      <span class="domain-viz" data-kind=${vizKind}>${loading ? null : html`<${Viz} viz=${view.viz} note=${view.note} />`}</span>
      <span class="domain-note"><${Note} view=${view} loading=${loading} /></span>
    </a>`;
  }
  if (!view) {
    return html`<header class="domain-head" data-plain="">
      <div class="domain-title-row">
        <${DomainIcon} icon=${section.icon} />
        <h2 class="card-title" id=${titleId}>${section.label}</h2>
      </div>
    </header>`;
  }
  // With no chart and no note once the answer is in, the head is its title
  // and figure: the rows held for them while loading close once.
  const compact = !loading && !drawsViz(view.viz) && !view.note;
  return html`<header class="domain-head" data-role=${role} data-domain=${section.id} data-compact=${compact ? '' : undefined}>
    <div class="domain-title-row">
      <${DomainIcon} icon=${section.icon} role=${role} />
      <h2 class="card-title" id=${titleId}>${section.label}</h2>
      <${Tag} view=${view} loading=${loading} />
      ${actions ? html`<div class="domain-actions">${actions}</div>` : null}
    </div>
    <div class="domain-figure"><${Figure} view=${view} loading=${loading} /></div>
    ${compact
      ? null
      : html`<div class="domain-viz" data-kind=${vizKind}>${loading ? null : html`<${Viz} viz=${view.viz} note=${view.note} />`}</div>
          <p class="domain-note"><${Note} view=${view} loading=${loading} linked /></p>`}
  </header>`;
}

/** A head action that reads the domain's status again: busy until the answer is in (D18). */
export function RefreshButton({ label, onRefresh, disabled }) {
  const [busy, setBusy] = useState(false);
  const run = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await onRefresh();
    } finally {
      setBusy(false);
    }
  };
  return html`<${Button}
    variant="ghost"
    size="sm"
    icon="rotate"
    className="domain-refresh"
    aria-label=${label}
    title=${disabled ? 'Offline' : label}
    aria-busy=${busy ? 'true' : undefined}
    aria-disabled=${busy || disabled ? 'true' : undefined}
    data-busy=${busy ? '' : undefined}
    onClick=${disabled ? undefined : run}
  />`;
}
