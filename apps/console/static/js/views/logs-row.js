// One row of the Logs page: the line, its job and request chips, and the
// expanded JSON detail.

import { html } from '../ui/preact.js';
import { absTime, clockTime, shortId, errorSummary, attrPairs } from '../lib/format.js';
import { logLevelView } from '../lib/status.js';
import { buildHash } from '../lib/route.js';
import { CopyButton } from '../ui/components.js';

const ATTRS_SHOWN = 3;
const ATTR_MAX = 48;
// Already shown as chips or as the component column; `error` joins them when the row prints its reason.
const OMIT_ATTRS = ['component', 'job_id', 'request_id'];

/** Up to three key=value attributes for the row's line, each cut at 48 characters. */
function lineAttrs(attrs, omit) {
  return attrPairs(attrs, omit)
    .slice(0, ATTRS_SHOWN)
    .map((p) => (p.length > ATTR_MAX ? `${p.slice(0, ATTR_MAX - 1)}…` : p));
}

export function LogRow({ entry, open, stop, names, onToggle, onFilter, narrow }) {
  const lv = logLevelView(entry.level);
  const attrs = entry.attributes || {};
  const jobId = typeof attrs.job_id === 'string' ? attrs.job_id : '';
  const requestId = typeof attrs.request_id === 'string' ? attrs.request_id : '';
  const component = typeof attrs.component === 'string' ? attrs.component : '';
  const json = JSON.stringify(attrs, null, 2);
  // A failure's reason is on the row, not behind a click.
  const reason = typeof attrs.error === 'string' && entry.level !== 'debug' ? errorSummary(attrs.error) : '';
  const pairs = lineAttrs(attrs, reason ? [...OMIT_ATTRS, 'error'] : OMIT_ATTRS);
  const tab = stop ? 0 : -1;
  const jobName = jobId ? names.get(jobId) : '';
  return html`<div class=${open ? 'log-entry log-row is-open' : 'log-entry log-row'} data-level=${entry.level} data-seq=${entry.sequence} data-row tabindex="-1">
    <button type="button" class="log-line" tabindex=${tab} aria-expanded=${String(open)} onClick=${onToggle}>
      <span class="log-time" title=${absTime(entry.time)}>${clockTime(entry.time, !narrow)}</span>
      <span class="log-level" data-role=${lv.role}><span class="log-dot"></span>${lv.short}</span>
      ${component ? html`<span class="log-comp">${component}</span>` : html`<span class="log-comp"></span>`}
      <span class="log-msg">${entry.message}${reason && !entry.message.includes(reason) ? html` <span class="log-err" title=${attrs.error}>${reason}</span>` : null}${pairs.length ? html` <span class="log-attrs">${pairs.join('  ')}</span>` : null}</span>
    </button>
    ${jobId || requestId
      ? html`<span class="log-chips">
          ${jobId
            ? html`<button type="button" class="attr-chip attr-chip-job" tabindex=${tab} title=${`Filter to ${jobName ? `${jobName} (${jobId})` : `job ${jobId}`}`} onClick=${() => onFilter({ job_id: jobId })}>${jobName ? html`<span class="attr-chip-name" dir="auto">${jobName}</span>` : `job ${shortId(jobId)}`}</button>`
            : null}
          ${requestId ? html`<button type="button" class="attr-chip" tabindex=${tab} title=${`Filter to request ${requestId}`} onClick=${() => onFilter({ request_id: requestId })}>req ${requestId.slice(0, 8)}</button>` : null}
        </span>`
      : null}
    ${open
      ? html`<div class="log-detail">
          <div class="log-detail-bar">
            <span class="muted small">#${entry.sequence} · ${absTime(entry.time)}${component ? ` · ${component}` : ''}</span>
            <${CopyButton} text=${JSON.stringify(entry, null, 2)} label="Copy record as JSON" />
            ${jobId ? html`<a class="link small" href=${buildHash('queue', {}, jobId)}>Open job</a>` : null}
          </div>
          <pre class="log-json">${json}</pre>
        </div>`
      : null}
  </div>`;
}
