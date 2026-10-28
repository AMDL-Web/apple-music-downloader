// System: build and runtime facts from /system, health of each part, the
// wrapper, library sync, hooks with outcomes, and a read-only config summary.

import { html, useState } from '../ui/preact.js';
import {
  getSystem,
  getStats,
  getConfig,
  reloadConfig,
  getHealth,
  getWrapperStatus,
  getLibrarySync,
  getHooks,
  settle,
  docsUrl,
  specUrl,
} from '../api.js';
import { count, bytes, elapsed, absTime, maskAccount, relTime } from '../lib/format.js';
import { backendHealthView, wrapperView, librarySyncView, hooksView } from '../lib/status.js';
import { useApp, Card, Badge, StatusText, KV, Icon, Button, LinkButton, ErrorNote, SkeletonRows, Unavailable, Notice, Empty, RelTime } from '../ui/components.js';
import { usePoll } from '../ui/hooks.js';

export function SystemPage() {
  const app = useApp();
  const sys = usePoll(getSystem, 30000, [app.epoch]);
  const health = usePoll(() => Promise.all([settle(getHealth()), settle(getWrapperStatus()), settle(getLibrarySync()), settle(getHooks())]), 30000, [app.epoch]);
  const stats = usePoll(() => getStats(14), 60000, [app.epoch]);
  const [h, w, ls, hk] = health.data || [];
  return html`<div class="page page-system">
    <div class="grid-2">
      <${AboutCard} sys=${sys} />
      <${RuntimeCard} sys=${sys} />
    </div>
    <div class="grid-2">
      <${HealthCard} backend=${h} wrapper=${w} sync=${ls} hooks=${hk} loading=${!health.data} />
      <${StorageCard} sys=${sys} />
    </div>
    <div class="grid-2">
      <${WrapperCard} res=${w} loading=${!health.data} />
      <${LibrarySyncCard} res=${ls} loading=${!health.data} />
    </div>
    <${HooksCard} res=${hk} stats=${stats} loading=${!health.data} />
    <${ConfigCard} />
    <${LinksCard} />
  </div>`;
}

const notFound = (poll) => poll.error && poll.error.status === 404;

function AboutCard({ sys }) {
  const s = sys.data;
  let body;
  if (notFound(sys)) body = html`<${Unavailable} what="Build and uptime information (GET /api/v1/system)" />`;
  else if (sys.error && !s) body = html`<${ErrorNote} title="Couldn't load system information" error=${sys.error} onRetry=${sys.reload} />`;
  else if (!s) body = html`<${SkeletonRows} n=${5} kind="line" />`;
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
  else if (!s) body = sys.error ? null : html`<${SkeletonRows} n=${5} kind="line" />`;
  else {
    const j = s.jobs || {};
    const p = s.pools || {};
    body = html`
      <${KV}
        rows=${[
          ['Mode', s.simulate ? 'Simulate' : 'Normal'],
          ['Token signing', s.signing_mode === 'local' ? 'Local (.p8 key)' : s.signing_mode === 'legacy' ? 'Legacy (scraped web token)' : s.signing_mode],
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
                <span class="mono">${t.name}</span>
                <${StatusText} view=${t.available ? { icon: 'circle-check', role: 'success', label: 'Available' } : { icon: 'circle-x', role: 'danger', label: 'Missing' }} />
                ${t.path ? html`<span class="mono muted small wrap-any">${t.path}</span>` : null}
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

function StorageCard({ sys }) {
  const s = sys.data;
  let body;
  if (notFound(sys)) body = html`<${Unavailable} what="Free space" />`;
  else if (!s) body = sys.error ? null : html`<${SkeletonRows} n=${2} kind="line" />`;
  else if (!Array.isArray(s.storage) || s.storage.length === 0) body = html`<p class="muted small">No storage information.</p>`;
  else
    body = html`<ul class="storage-list">
      ${s.storage.map((d) => {
        const known = typeof d.free_bytes === 'number' && typeof d.total_bytes === 'number' && d.total_bytes > 0;
        const used = known ? 1 - d.free_bytes / d.total_bytes : 0;
        const role = !known ? 'neutral' : used >= 0.95 ? 'danger' : used >= 0.85 ? 'warning' : 'info';
        return html`<li key=${d.name} class="storage-row">
          <div class="storage-head">
            <span class="mono">${d.name}</span>
            <span class="mono muted small wrap-any">${d.path}</span>
          </div>
          ${known
            ? html`<span class="meter" role="meter" aria-label=${`${d.name} used space`} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${Math.round(used * 100)} aria-valuetext=${`${bytes(d.free_bytes)} free of ${bytes(d.total_bytes)}`}>
                  <span class="meter-fill" data-role=${role} style=${{ '--v': String(used) }}></span>
                </span>
                <span class="small"><span class="num">${bytes(d.free_bytes)}</span> free of <span class="num">${bytes(d.total_bytes)}</span>${role === 'danger' || role === 'warning' ? html` · <${StatusText} view=${{ icon: 'triangle-alert', role, label: 'Low space' }} />` : null}</span>`
            : html`<${StatusText} view=${{ icon: 'circle-minus', role: 'neutral', label: d.error ? `Unknown: ${d.error}` : 'Unknown' }} />`}
        </li>`;
      })}
    </ul>`;
  return html`<${Card} className="span-6" title="Storage" labelId="storage-title">${body}</${Card}>`;
}

function HealthCard({ backend, wrapper, sync, hooks, loading }) {
  const rows = loading
    ? null
    : [
        ['Backend', backendHealthView(backend)],
        ['Wrapper', wrapperView(wrapper)],
        ['Library sync', librarySyncView(sync)],
        ['Hooks', hooksView(hooks)],
      ];
  return html`<${Card} className="span-6" title="Health" labelId="sys-health-title" note="Checked every 30 s">
    ${rows
      ? html`<ul class="health-list">
          ${rows.map(
            ([name, view]) => html`<li key=${name} class="health-row">
              <span class="health-name">${name}</span>
              <${Badge} view=${view} />
              ${view.detail ? html`<span class="health-detail wrap-any" title=${view.title || undefined}>${view.detail}</span>` : null}
            </li>`,
          )}
        </ul>`
      : html`<${SkeletonRows} n=${4} kind="line" />`}
  </${Card}>`;
}

function WrapperCard({ res, loading }) {
  const [reveal, setReveal] = useState(false);
  let body;
  if (loading) body = html`<${SkeletonRows} n=${3} kind="line" />`;
  else if (!res || res.error) {
    const view = wrapperView(res);
    body = html`<${KV}
        rows=${[
          ['State', html`<${Badge} view=${view} />`],
          ['Error', view.detail ? html`<span class="wrap-any" title=${view.title || undefined}>${view.detail}</span>` : null],
        ]}
      />
      <p class="muted small">The backend reaches wrapper-manager at <code>wrapper.address</code>; downloads need it, simulate mode does not.</p>`;
  }
  else {
    const wr = res.data;
    const accounts = Array.isArray(wr.accounts) ? wr.accounts : [];
    body = html`<${KV}
        rows=${[
          ['State', html`<${Badge} view=${wrapperView(res)} />`],
          ['Connected', wr.status ? 'Yes' : 'No'],
          ['Clients', html`<span class="num">${count(wr.client_count)}</span>`],
          ['Regions', Array.isArray(wr.regions) && wr.regions.length ? wr.regions.map((r) => r.toUpperCase()).join(', ') : null],
          ['Accounts', html`<span class="num">${count(accounts.length)}</span>`],
        ]}
      />
      ${accounts.length
        ? html`<div class="accounts">
            <ul class="account-list" aria-label="Logged-in accounts">
              ${accounts.map((a) => html`<li key=${a} class="mono">${reveal ? a : maskAccount(a)}</li>`)}
            </ul>
            <${Button} size="sm" variant="ghost" icon=${reveal ? 'eye-off' : 'eye'} aria-pressed=${String(reveal)} onClick=${() => setReveal((r) => !r)}>
              ${reveal ? 'Hide accounts' : 'Reveal accounts'}
            </${Button}>
          </div>`
        : null}
      <p class="muted small">Log in and out of accounts from amdl-web or the iOS app.</p>`;
  }
  return html`<${Card} className="span-6" title="Wrapper" labelId="wrapper-title">${body}</${Card}>`;
}

function LibrarySyncCard({ res, loading }) {
  let body;
  if (loading) body = html`<${SkeletonRows} n=${3} kind="line" />`;
  else if (!res || res.error) body = html`<${Badge} view=${librarySyncView(res)} />${res && res.error && res.error.status === 503 ? html` <span class="muted small">This process has no library watcher.</span>` : null}`;
  else {
    const s = res.data;
    body = html`<${KV}
      rows=${[
        ['State', html`<${Badge} view=${librarySyncView(res)} />`],
        ['Interval', `${count(s.interval_minutes)} min`],
        ['Last poll', s.last_poll_at ? html`<${RelTime} value=${s.last_poll_at} />` : 'Never'],
        ['Last error', s.last_error ? html`<span class="text-warning wrap-any">${s.last_error}</span>` : null],
        ['Anchor', `${count(s.anchor_size)} tracks`],
        ['Submitted', `${count(s.last_submitted)} last poll · ${count(s.total_submitted)} since start`],
      ]}
    />`;
  }
  return html`<${Card} className="span-6" title="Library sync" labelId="sync-title">${body}</${Card}>`;
}

function HooksCard({ res, stats, loading }) {
  let body;
  const outcomes = new Map(((stats.data && stats.data.hooks) || []).map((h) => [h.name, h]));
  if (loading) body = html`<${SkeletonRows} n=${3} kind="line" />`;
  else if (!res || res.error) body = html`<${ErrorNote} title="Couldn't load hooks" error=${res && res.error} />`;
  else {
    const entries = res.data.entries || [];
    if (entries.length === 0) body = html`<${Empty} icon="info" title="No hooks configured">Hooks are defined in configs/hooks.yaml.</${Empty}>`;
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
  return html`<${Card} title="Hooks" labelId="hooks-title">${body}</${Card}>`;
}

const CONFIG_SECTIONS = ['download', 'catalog', 'logging', 'simulate', 'library_sync'];

function formatValue(key, value) {
  if (key === 'media_user_token') return value ? 'Set' : 'Not set';
  if (typeof value === 'boolean') return value ? 'On' : 'Off';
  if (Array.isArray(value)) return value.length ? value.join(', ') : '(empty)';
  if (value === null || value === undefined || value === '') return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

function ConfigCard() {
  const app = useApp();
  const cfg = usePoll(getConfig, 0, [app.epoch]);
  const [reloading, setReloading] = useState(false);
  const [reloadResult, setReloadResult] = useState(null);
  const doReload = async () => {
    setReloading(true);
    try {
      const body = await reloadConfig();
      setReloadResult(body);
      app.toast(body.reload_error ? { message: 'Config file is invalid; the previous config stays active', role: 'warning', sticky: true } : { message: 'Config reloaded', role: 'success' });
      cfg.reload();
    } catch (e) {
      app.toast({ message: e.status === 404 ? 'This backend cannot reload its config from the API' : e.message, role: 'danger', sticky: true });
    } finally {
      setReloading(false);
    }
  };
  const data = cfg.data;
  const reloadError = (reloadResult && reloadResult.reload_error) || (data && data.reload_error);
  let body;
  if (cfg.error && !data) body = html`<${ErrorNote} title="Couldn't load the config" error=${cfg.error} onRetry=${cfg.reload} />`;
  else if (!data) body = html`<${SkeletonRows} n=${8} kind="line" />`;
  else
    body = html`
      ${reloadError
        ? html`<${Notice} role="warning" icon="triangle-alert">Config file is unreadable; showing the last valid config. <span class="mono wrap-any">${reloadError}</span></${Notice}>`
        : null}
      ${data.persisted === false ? html`<${Notice}>Changes are not persisted to a file.</${Notice}>` : null}
      <div class="config-grid">
        ${CONFIG_SECTIONS.filter((s) => data.config && data.config[s]).map(
          (section) => html`<section key=${section} class="config-section" aria-labelledby=${`cfg-${section}`}>
            <h3 class="section-title mono" id=${`cfg-${section}`}>${section}</h3>
            <dl class="kv kv-mono">
              ${Object.entries(data.config[section]).map(
                ([k, v]) => html`<div class="kv-row" key=${k}><dt>${k}</dt><dd class="wrap-any">${formatValue(k, v) || html`<span class="muted">—</span>`}</dd></div>`,
              )}
            </dl>
          </section>`,
        )}
      </div>
      <p class="muted small">Startup-only settings are not exposed by the API. Edit settings in amdl-web or configs/config.yaml.</p>`;
  return html`<${Card}
    title="Configuration"
    labelId="config-title"
    note="Read-only"
    actions=${html`<${Button} size="sm" icon="rotate" onClick=${doReload} disabled=${reloading}>${reloading ? 'Reloading…' : 'Reload config'}</${Button}>`}
  >${body}</${Card}>`;
}

function LinksCard() {
  return html`<${Card} title="API" labelId="api-title">
    <div class="link-row">
      <${LinkButton} href=${docsUrl()} icon="book" external>API docs</${LinkButton}>
      <${LinkButton} href=${specUrl()} icon="braces" external>OpenAPI spec</${LinkButton}>
    </div>
    <p class="muted small">The API docs page loads Swagger UI from a CDN and needs internet access.</p>
  </${Card}>`;
}
