// System: health first, then the wrapper, storage, a short config summary that
// links to Settings, library sync, hooks with outcomes, and the runtime and
// build facts from /system last. Cards sit in pairs of similar height.

import { html } from '../ui/preact.js';
import {
  getSystem,
  getStats,
  getConfig,
  getHealth,
  getWrapperStatus,
  getLibrarySync,
  getHooks,
  settle,
} from '../api.js';
import { count, bytes, elapsed, absTime, relTime } from '../lib/format.js';
import { settingsHash } from '../lib/route.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView, missingToolRows } from '../lib/status.js';
import { useApp, Card, Badge, StatusText, KV, LinkButton, ErrorNote, SkeletonKV, Unavailable, Notice, RelTime, HealthList, CopyButton } from '../ui/components.js';
import { usePoll } from '../ui/hooks.js';
import { midTruncate, groupByDisk, diskRole } from '../lib/storage.js';

export function SystemPage() {
  const app = useApp();
  const sys = usePoll(getSystem, 30000, [app.epoch], 'system');
  const health = usePoll(() => Promise.all([settle(getHealth()), settle(getWrapperStatus()), settle(getLibrarySync()), settle(getHooks())]), 30000, [app.epoch], 'system-health');
  const stats = usePoll(() => getStats(14), 60000, [app.epoch], 'stats-14');
  const [h, w, ls, hk] = health.data || [];
  const entries = hk && !hk.error ? hk.data.entries || [] : [];
  // One flowing grid: pairs of cards, and a hooks table wide enough for its columns.
  return html`<div class="page page-system">
    <div class="grid-2">
      <${HealthCard} backend=${h} wrapper=${w} sync=${ls} hooks=${hk} tools=${missingToolRows(sys.data)} loading=${!health.data} />
      <${WrapperCard} res=${w} loading=${!health.data} />
      <${StorageCard} sys=${sys} />
      <${ConfigCard} />
      <${LibrarySyncCard} res=${ls} loading=${!health.data} />
      <${HooksCard} res=${hk} stats=${stats} loading=${!health.data} wide=${entries.length > 0} />
      <${RuntimeCard} sys=${sys} />
      <${AboutCard} sys=${sys} />
    </div>
  </div>`;
}

const notFound = (poll) => poll.error && poll.error.status === 404;

function AboutCard({ sys }) {
  const s = sys.data;
  let body;
  if (notFound(sys)) body = html`<${Unavailable} what="Build and uptime information (GET /api/v1/system)" />`;
  else if (sys.error && !s) body = html`<${ErrorNote} title="Couldn't load system information" error=${sys.error} onRetry=${sys.reload} />`;
  else if (!s) body = html`<${SkeletonKV} labels=${['Version', 'Commit', 'Built', 'Started', 'Uptime', 'Go', 'Platform']} />`;
  else
    body = html`<${KV}
      rows=${[
        ['Version', html`<span class="mono">${s.version}</span>`],
        ['Commit', s.commit ? html`<span class="mono">${s.commit}</span>` : null],
        ['Built', s.build_date ? absTime(s.build_date) : null],
        ['Started', s.started_at ? html`<span>${absTime(s.started_at)} <span class="muted">(${relTime(s.started_at)})</span></span>` : null],
        ['Uptime', typeof s.uptime_seconds === 'number' ? elapsed(s.uptime_seconds * 1000) : null],
        ['Go', s.go_version ? html`<span class="mono">${s.go_version}</span>` : null],
        ['Platform', s.os ? `${s.os}/${s.arch}` : null],
      ]}
    />`;
  return html`<${Card} className="span-6" title="About" labelId="about-title">${body}</${Card}>`;
}

function RuntimeCard({ sys }) {
  const s = sys.data;
  let body;
  if (notFound(sys)) body = html`<${Unavailable} what="Runtime capacity" />`;
  else if (!s)
    body = sys.error
      ? null
      : html`<${SkeletonKV} labels=${['Mode', 'Motion covers', 'Workers', 'Queue', 'Recovery backlog', 'Pools']} />
          <ul class="tool-list" aria-hidden="true"><li class="tool-row"><span class="tool-name mono">ffmpeg</span><span class="tool-value"><span class="skel skel-lh"></span></span></li></ul>`;
  else {
    const j = s.jobs || {};
    const p = s.pools || {};
    body = html`
      <${KV}
        rows=${[
          ['Mode', s.simulate ? 'Simulate' : 'Normal'],
          ['Motion covers', s.motion_artwork_enabled === undefined ? null : s.motion_artwork_enabled ? 'On' : 'Off'],
          ['Workers', j.workers !== undefined ? html`<span class="num">${count(j.active)} active of ${count(j.workers)}</span>` : null],
          ['Queue', j.queue_capacity !== undefined ? html`<${QueueMeter} depth=${j.queue_depth} capacity=${j.queue_capacity} />` : null],
          ['Recovery backlog', j.recovery_backlog !== undefined ? count(j.recovery_backlog) : null],
          ['Pools', p.downloads !== undefined ? html`<span class="num">${count(p.downloads)} downloads · ${count(p.decrypts)} decrypts · ${count(p.wrapper_requests)} wrapper requests</span>` : null],
        ]}
      />
      ${Array.isArray(s.tools) && s.tools.length
        ? html`<ul class="tool-list" aria-label="Tools">
            ${s.tools.map(
              (t) => html`<li key=${t.name} class="tool-row">
                <span class="tool-name mono">${t.name}</span>
                <span class="tool-value">
                  <${StatusText} view=${t.available ? { icon: 'circle-check', role: 'success', label: 'Available' } : { icon: 'circle-x', role: 'danger', label: 'Missing' }} />
                  ${t.path ? html`<span class="mono muted small wrap-any">${t.path}</span>` : null}
                  ${t.available ? null : html`<span class="muted small" title=${t.error || undefined}>Install it, or set <code>tools.${t.name}</code> in configs/config.yaml to its path and restart.</span>`}
                </span>
              </li>`,
            )}
          </ul>`
        : null}`;
  }
  return html`<${Card} className="span-6" title="Runtime" labelId="runtime-title">${body}</${Card}>`;
}

function QueueMeter({ depth = 0, capacity }) {
  const frac = capacity > 0 ? Math.min(1, depth / capacity) : 0;
  return html`<span class="meter-inline">
    <span class="meter" role="meter" aria-valuemin="0" aria-valuemax=${capacity} aria-valuenow=${depth} aria-label="Queue depth">
      <span class="meter-fill" data-role=${frac >= 0.9 ? 'danger' : frac >= 0.6 ? 'warning' : 'info'} style=${{ '--v': String(frac) }}></span>
    </span>
    <span class="num">${count(depth)} of ${count(capacity)}</span>
  </span>`;
}

const FOLDER_NAMES = { downloads_dir: 'Downloads folder', temp_dir: 'Temporary folder' };
function StorageCard({ sys }) {
  const s = sys.data;
  let body;
  if (notFound(sys)) body = html`<${Unavailable} what="Free space" />`;
  else if (!s)
    // The usual shape: both folders on one disk, their paths and free space loading.
    body = sys.error
      ? null
      : html`<ul class="storage-list" aria-busy="true" aria-label="Loading">
          <li class="storage-row">
            <ul class="storage-folders">
              ${Object.entries(FOLDER_NAMES).map(
                ([k, name]) => html`<li key=${k} class="storage-folder">
                  <span class="storage-name">${name}</span>
                  <span class="storage-path mono muted small"><span class="skel skel-lh"></span></span>
                </li>`,
              )}
            </ul>
            <span class="meter"></span>
            <span class="small"><span class="skel skel-lh skel-short"></span></span>
          </li>
        </ul>`;
  else if (!Array.isArray(s.storage) || s.storage.length === 0) body = html`<p class="muted small">No storage information.</p>`;
  else
    body = html`<ul class="storage-list">
      ${groupByDisk(s.storage).map((g) => {
        const used = g.known ? 1 - g.free / g.total : 0;
        const role = diskRole(g);
        const label = g.folders.map((d) => FOLDER_NAMES[d.name] || d.name).join(' and ');
        return html`<li key=${g.folders[0].name} class="storage-row">
          <ul class="storage-folders">
            ${g.folders.map(
              (d) => html`<li key=${d.name} class="storage-folder">
                <span class="storage-name">${FOLDER_NAMES[d.name] || d.name}</span>
                <${CopyButton} text=${d.path} label=${`Copy ${(FOLDER_NAMES[d.name] || d.name).toLowerCase()} path`} />
                <span class="storage-path mono muted small" title=${d.path}>${midTruncate(d.path)}</span>
              </li>`,
            )}
          </ul>
          ${g.known
            ? html`<span class="meter" role="meter" aria-label=${`${label} used space`} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${Math.round(used * 100)} aria-valuetext=${`${bytes(g.free)} free of ${bytes(g.total)}`}>
                  <span class="meter-fill" data-role=${role} style=${{ '--v': String(used) }}></span>
                </span>
                <span class="small"><span class="num">${bytes(g.free)}</span> free of <span class="num">${bytes(g.total)}</span>${role === 'danger' || role === 'warning' ? html` · <${StatusText} view=${{ icon: 'triangle-alert', role, label: 'Low space' }} />` : null}</span>`
            : html`<${StatusText} view=${{ icon: 'circle-minus', role: 'neutral', label: g.error ? `Unknown: ${g.error}` : 'Unknown' }} />`}
        </li>`;
      })}
    </ul>`;
  return html`<${Card} className="span-6" title="Storage" labelId="storage-title">${body}</${Card}>`;
}

// The rows the Health card always has, shown with their status loading.
const HEALTH_NAMES = ['Backend', 'Wrapper', 'Library sync', 'Hooks'];

function HealthCard({ backend, wrapper, sync, hooks, tools = [], loading }) {
  const rows = loading
    ? null
    : [
        ['Backend', backendHealthView(backend)],
        ['Wrapper', wrapperView(wrapper)],
        ['Library sync', librarySyncView(sync)],
        ['Hooks', hooksView(hooks)],
        ...tools,
      ];
  return html`<${Card} className="span-6" title="Health" labelId="sys-health-title" note="Checked every 30 s">
    ${rows
      ? html`<${HealthList} rows=${rows} wrapDetail />`
      : html`<${HealthList} rows=${HEALTH_NAMES.map((n) => [n, null])} />`}
  </${Card}>`;
}

function WrapperCard({ res, loading }) {
  let body;
  if (loading) body = html`<${SkeletonKV} labels=${['State', 'Error']} badges=${['State']} /><p class="muted small"><span class="skel skel-lh"></span></p>`;
  else if (!res || res.error) {
    const view = wrapperView(res);
    body = html`<${KV}
        rows=${[
          ['State', html`<${Badge} view=${view} />`],
          ['Error', view.detail ? html`<span class="wrap-any" title=${view.title || undefined}>${view.detail}</span>` : null],
        ]}
      />
      <p class="muted small">The backend reaches the wrapper (wrapper-lite, or a wrapper-manager in front of it) at <code>wrapper.address</code>; downloads need it, simulate mode does not. Accounts are managed in <a class="link" href=${settingsHash('account')}>Settings › Account</a>.</p>`;
  }
  else {
    const wr = res.data;
    const accounts = Array.isArray(wr.accounts) ? wr.accounts : [];
    body = html`<${KV}
        rows=${[
          ['Connected', wr.status ? 'Yes' : 'No'],
          ['Regions', Array.isArray(wr.regions) && wr.regions.length ? wr.regions.map((r) => r.toUpperCase()).join(', ') : null],
          accounts.length ? ['Accounts', html`<span class="num">${count(accounts.length)}</span>`] : null,
        ].filter(Boolean)}
      />
      ${accounts.length
        ? html`<div class="accounts">
            <ul class="account-list" aria-label="Logged-in accounts">
              ${accounts.map((a) => html`<li key=${a} class="mono">${a}</li>`)}
            </ul>
          </div>`
        : null}
      ${wr.login_supported === false
        ? html`<p class="muted small">This wrapper logs in on its own command line; <a class="link" href=${settingsHash('account')}>Settings › Account</a> shows how.</p>`
        : html`<p class="muted small">Log in and out of accounts in <a class="link" href=${settingsHash('account')}>Settings › Account</a>.</p>`}`;
  }
  return html`<${Card} className="span-6" title="Wrapper" labelId="wrapper-title">${body}</${Card}>`;
}

function LibrarySyncCard({ res, loading }) {
  let body;
  if (loading) body = html`<p class="muted small"><span class="skel skel-lh"></span></p>`;
  else if (!res || res.error) body = html`<${Badge} view=${librarySyncView(res)} />`;
  else if (!res.data.enabled) body = html`<p class="muted small">Library sync is off. You can turn it on in <a class="link" href=${settingsHash()}>Settings</a>.</p>`;
  else {
    const s = res.data;
    body = html`<${KV}
      rows=${[
        ['Interval', `${count(s.interval_minutes)} min`],
        ['Last poll', s.last_poll_at ? html`<${RelTime} value=${s.last_poll_at} />` : 'Never'],
        ['Last error', s.last_error ? html`<span class="text-warning wrap-any">${s.last_error}</span>` : null],
        ['Last seen in your library', `${count(s.anchor_size)} tracks`],
        ['Submitted', `${count(s.last_submitted)} last poll · ${count(s.total_submitted)} since start`],
      ]}
    />`;
  }
  return html`<${Card} className="span-6" title="Library sync" labelId="sync-title">${body}</${Card}>`;
}

function HooksCard({ res, stats, loading, wide }) {
  let body;
  const outcomes = new Map(((stats.data && stats.data.hooks) || []).map((h) => [h.name, h]));
  if (loading) body = html`<p class="quiet-note"><span class="skel skel-lh"></span></p>`;
  else if (!res || res.error) body = html`<${ErrorNote} title="Couldn't load hooks" error=${res && res.error} />`;
  else {
    const entries = res.data.entries || [];
    if (entries.length === 0) body = html`<p class="quiet-note">No hooks configured</p>`;
    else
      body = html`${!res.data.enabled ? html`<${Notice} icon="circle-minus">Hooks are switched off globally in hooks.yaml.</${Notice}>` : null}
        <div class="table-wrap">
          <table class="table">
            <thead>
              <tr><th scope="col">Name</th><th scope="col">Type</th><th scope="col">Events</th><th scope="col">Job types</th><th scope="col">State</th><th scope="col" class="num">Succeeded</th><th scope="col" class="num">Failed</th><th scope="col">Last failure</th></tr>
            </thead>
            <tbody>
              ${entries.map((e) => {
                const o = outcomes.get(e.name);
                return html`<tr key=${e.name}>
                  <th scope="row" class="mono" data-label="Name">${e.name}</th>
                  <td data-label="Type">${e.type}</td>
                  <td data-label="Events" class="small">${(e.events || []).join(', ') || 'none'}</td>
                  <td data-label="Job types" class="small">${(e.job_types || []).length ? e.job_types.join(', ') : 'all types'}</td>
                  <td data-label="State"><${StatusText} view=${e.enabled ? { icon: 'circle-check', role: 'success', label: 'Enabled' } : { icon: 'circle-minus', role: 'neutral', label: 'Disabled' }} /></td>
                  <td data-label="Succeeded" class="num">${o ? count(o.succeeded) : '—'}</td>
                  <td data-label="Failed" class="num">${o ? count(o.failed) : '—'}</td>
                  <td data-label="Last failure">${o && o.last_failed_at ? html`<${RelTime} value=${o.last_failed_at} />` : '—'}</td>
                </tr>`;
              })}
            </tbody>
          </table>
        </div>
        ${notFound(stats) ? html`<p class="muted small">Outcome counts need GET /api/v1/stats, which this backend does not have.</p>` : html`<p class="muted small">Outcomes over the last 14 days of retained jobs.</p>`}`;
  }
  return html`<${Card} className=${wide ? undefined : 'span-6'} title="Hooks" labelId="hooks-title">${body}</${Card}>`;
}

const CODEC_NAMES = { alac: 'ALAC', ec3: 'Dolby Atmos', ac3: 'Dolby Digital', aac: 'AAC', 'aac-binaural': 'AAC binaural', 'aac-downmix': 'AAC downmix' };

/** A few runtime settings at a glance; Settings edits them. Folders, simulate and library sync show in Storage, Runtime and Library sync. */
function ConfigCard() {
  const app = useApp();
  const cfg = usePoll(getConfig, 0, [app.epoch], 'config');
  const data = cfg.data;
  let body;
  if (cfg.error && !data) body = html`<${ErrorNote} title="Couldn't load the config" error=${cfg.error} onRetry=${cfg.reload} />`;
  else if (!data) body = html`<${SkeletonKV} labels=${['Codec priority', 'Log level', 'Media user token']} /><p class="muted small"><span class="skel skel-lh"></span></p>`;
  else {
    const c = data.config || {};
    const d = c.download || {};
    body = html`<${KV}
        rows=${[
          ['Codec priority', Array.isArray(d.quality_priority) ? d.quality_priority.map((q) => CODEC_NAMES[q] || q).join(' → ') : null],
          ['Log level', c.logging ? c.logging.level : null],
          ['Media user token', c.catalog ? (c.catalog.media_user_token ? 'Set' : 'Not set') : null],
        ]}
      />
      ${data.persisted === false ? html`<${Notice} role="warning" icon="triangle-alert">Changes are not written to configs/config.yaml, so a restart loses them.</${Notice}>` : null}
      <p class="muted small">Startup-only settings are not exposed by the API; edit configs/config.yaml and restart for those.</p>`;
  }
  return html`<${Card}
    className="span-6"
    title="Configuration"
    labelId="config-title"
    section="config"
    actions=${html`<${LinkButton} size="sm" icon="settings" href=${settingsHash()}>Open settings</${LinkButton}>`}
  >${body}</${Card}>`;
}
