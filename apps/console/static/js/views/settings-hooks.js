// Settings › Hooks: the hooks hooks.yaml defines, each on two lines (name,
// type and its runs over the stats window; then the events and job types it
// covers). There is no write API, so the section only shows them.

import { html } from '../ui/preact.js';
import { count, relTime } from '../lib/format.js';
import { STATS_DAYS } from '../lib/sysdata.js';

const ALL_TYPES = ['song', 'album', 'playlist', 'artist', 'station'];
const EVENT_LABELS = { job_queued: 'queued', job_finished: 'finished', job_failed: 'failed', job_cancelled: 'cancelled' };

/** The hooks of GET /hooks with their outcomes from /stats `hooks`; `now` for the relative times. */
export function HooksList({ entries, outcomes, now }) {
  if (!entries.length) return null;
  const byName = new Map((outcomes || []).map((o) => [o.name, o]));
  return html`<ul class="settings-hooks">
    ${entries.map((h) => {
      const o = byName.get(h.name);
      const ok = (o && o.succeeded) || 0;
      const bad = (o && o.failed) || 0;
      const runs = ok + bad;
      const types = Array.isArray(h.job_types) ? h.job_types : [];
      const allTypes = types.length === 0 || ALL_TYPES.every((t) => types.includes(t));
      return html`<li key=${h.name} class="settings-hook" data-off=${h.enabled ? undefined : ''}>
        <div class="settings-hook-line">
          <span class="settings-hook-name mono">${h.name}</span>
          <span class="settings-hook-chip">${h.type}</span>
          ${h.enabled ? null : html`<span class="settings-hook-chip">Off</span>`}
          <span class="settings-hook-runs">
            ${runs > 0
              ? html`<span class="settings-hook-counts" aria-label=${`${count(ok)} succeeded, ${count(bad)} failed`}>${count(ok)} ✓ · ${count(bad)} ✕</span>
                  <span class="viz-split settings-hook-split" aria-hidden="true">
                    <span class="viz-seg" data-tone="success" style=${{ '--w': String(ok / runs) }}></span>
                    <span class="viz-seg" data-tone="danger" style=${{ '--w': String(bad / runs) }}></span>
                  </span>
                  <span class="settings-hook-last">${o && o.last_failed_at ? relTime(o.last_failed_at, now) : ''}</span>`
              : html`<span class="settings-hook-none">No runs in ${STATS_DAYS} days</span>`}
          </span>
        </div>
        <div class="settings-hook-line settings-hook-scope">
          ${(h.events || []).map((e) => html`<span key=${e} class="settings-hook-event">${EVENT_LABELS[e] || e}</span>`)}
          ${allTypes ? null : html`<span class="settings-hook-types">${types.join(', ')}</span>`}
        </div>
      </li>`;
    })}
  </ul>`;
}
