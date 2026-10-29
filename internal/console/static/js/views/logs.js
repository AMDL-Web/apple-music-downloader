// Logs: level, component and text filters, job_id/request_id filter chips,
// a live tail with pause, expandable rows, and follow-bottom scrolling.

import { html, useState, useEffect, useRef, useLayoutEffect } from '../ui/preact.js';
import { listLogs, listDownloads, getDownload, logStreamUrl, parseData } from '../api.js';
import { clockTime, absTime, shortId, count, errorSummary, inputLabel } from '../lib/format.js';
import { logLevelView } from '../lib/status.js';
import { appendLogs, emptyLogs, LOG_CAP } from '../lib/store.js';
import { StreamClient, claimSecondary, releaseSecondary, createCoalescer, isFeatureUnavailable } from '../lib/stream.js';
import { buildHash, logFilters, logParams, logQuery, LOG_LEVELS, LOG_COMPONENTS } from '../lib/route.js';
import { useApp, Icon, Button, Empty, ErrorNote, SkeletonRows, CopyButton, StatusText } from '../ui/components.js';
import { useDebounced, useMedia } from '../ui/hooks.js';
import { connectionView, LOG_RETENTION_OFF } from '../lib/status.js';

const FOLLOW_SLACK_PX = 24;
const INITIAL_LIMIT = 200;

/**
 * Titles of the jobs the log mentions, by id: the most recently updated 200
 * jobs, kept current by the feed. A job outside them keeps its short id
 * (the filter chip also reads the one job it filters to).
 */
function useJobNames(app, filterJobId) {
  const [names, setNames] = useState(() => new Map());
  useEffect(() => {
    let live = true;
    const name = (job) => job.title || inputLabel(job.input);
    listDownloads({ limit: 200, sort: 'updated_at', order: 'desc' }).then(
      (body) => {
        if (!live) return;
        setNames((m) => {
          const next = new Map(m);
          for (const job of body.downloads || []) if (!next.has(job.id)) next.set(job.id, name(job));
          return next;
        });
      },
      () => {},
    );
    const off = app.subscribeFeed((batch) => {
      const fresh = batch.filter((msg) => msg.type === 'download_upserted' && msg.job);
      if (fresh.length === 0) return;
      setNames((m) => {
        const next = new Map(m);
        for (const msg of fresh) next.set(msg.job.id, name(msg.job));
        return next;
      });
    });
    return () => {
      live = false;
      off();
    };
  }, [app.epoch]);
  useEffect(() => {
    if (!filterJobId || names.has(filterJobId)) return undefined;
    let live = true;
    getDownload(filterJobId).then(
      (body) => {
        if (live && body && body.job) setNames((m) => new Map(m).set(filterJobId, body.job.title || inputLabel(body.job.input)));
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [filterJobId]);
  return names;
}

export function LogsPage({ route }) {
  const app = useApp();
  const filters = logFilters(route.params);
  const [qInput, setQInput] = useState(filters.q);
  const q = useDebounced(qInput, 300);
  const [logs, setLogs] = useState(emptyLogs());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [live, setLive] = useState(true);
  const [streamState, setStreamState] = useState('connecting');
  const [expanded, setExpanded] = useState(() => new Set());
  const narrow = useMedia('(max-width: 639px)');
  const [wrap, setWrap] = useState(narrow);
  const [follow, setFollow] = useState(true);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [unseen, setUnseen] = useState(0);
  const view = useRef(null);
  const followRef = useRef(true);
  followRef.current = follow;
  const lastSeqRef = useRef(0);
  lastSeqRef.current = logs.lastSeq;
  const names = useJobNames(app, filters.job_id);
  // One Tab stop for the whole list (roving tabindex): the row last focused,
  // else the newest. Up and Down (and j and k) move between rows.
  const [activeSeq, setActiveSeq] = useState(null);
  const tabSeq = activeSeq !== null && logs.entries.some((e) => e.sequence === activeSeq) ? activeSeq : logs.entries.length ? logs.entries[logs.entries.length - 1].sequence : null;
  const onRowKey = (e) => {
    const keys = { ArrowDown: 1, ArrowUp: -1, Home: -Infinity, End: Infinity };
    if (!(e.key in keys) || e.metaKey || e.ctrlKey || e.altKey || !e.target.closest('.log-line, .attr-chip')) return;
    const lines = Array.from(view.current.querySelectorAll('.log-line'));
    const at = lines.findIndex((n) => n.closest('.log-row').contains(e.target));
    if (at < 0) return;
    e.preventDefault();
    const next = lines[Math.max(0, Math.min(lines.length - 1, keys[e.key] === Infinity ? lines.length - 1 : keys[e.key] === -Infinity ? 0 : at + keys[e.key]))];
    next.focus();
  };
  const onRowFocus = (e) => {
    const row = e.target.closest && e.target.closest('.log-row');
    if (row) setActiveSeq(Number(row.getAttribute('data-seq')));
  };

  const key = `${filters.levels.join(',')}|${filters.component}|${filters.job_id}|${filters.request_id}|${filters.q}|${app.epoch}`;

  const setParams = (patch) => {
    app.navigate(buildHash('logs', logParams({ ...filters, ...patch })), { replace: true });
  };

  useEffect(() => {
    if (q !== filters.q) setParams({ q });
  }, [q]);
  useEffect(() => {
    if (filters.q !== qInput && filters.q !== q) setQInput(filters.q);
  }, [filters.q]);

  // Initial page for the current filters.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    setLogs(emptyLogs());
    setExpanded(new Set());
    setUnseen(0);
    setFollow(true);
    listLogs(logQuery(filters, { limit: INITIAL_LIMIT })).then(
      (page) => {
        if (cancelled) return;
        setLogs((s) => ({ ...appendLogs(s, page.logs || []), truncated: Boolean(page.truncated), cursor: page.next_cursor }));
        setLoading(false);
      },
      (err) => {
        if (cancelled) return;
        setError(err);
        setLoading(false);
      },
    );
    return () => {
      cancelled = true;
    };
  }, [key]);

  // Live tail from the cursor after the initial page; pausing keeps the cursor.
  useEffect(() => {
    if (loading || error || !live) {
      setStreamState(live ? 'connecting' : 'paused');
      return undefined;
    }
    const coalescer = createCoalescer((batch) => {
      setLogs((s) => appendLogs(s, batch));
      if (!followRef.current) setUnseen((n) => n + batch.length);
    }, 100);
    const client = new StreamClient({
      url: logStreamUrl(logQuery(filters, { limit: INITIAL_LIMIT })),
      cursor: lastSeqRef.current || logs.cursor || null,
      onEvent: (ev) => {
        if (ev.type !== 'log') return;
        const entry = parseData(ev);
        if (entry) coalescer.push(entry);
      },
      onState: (s) => setStreamState(s.state),
      onEnd: ({ status, body }) => {
        const err = body && body.error;
        if (status === 400) setError({ status, message: (err && err.message) || 'Invalid filter' });
        else if (isFeatureUnavailable(status, body)) setError({ status, code: err.code, message: err.message, body });
      },
    });
    claimSecondary(client);
    client.start();
    return () => {
      coalescer.cancel();
      client.stop();
      releaseSecondary(client);
    };
  }, [key, loading, error === null, live]);

  // Follow the bottom after new rows while the reader is at the bottom.
  useLayoutEffect(() => {
    const el = view.current;
    if (el && follow) el.scrollTop = el.scrollHeight;
  }, [logs.entries.length, follow, wrap]);

  const onScroll = () => {
    const el = view.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight <= FOLLOW_SLACK_PX;
    if (atBottom !== followRef.current) setFollow(atBottom);
    if (atBottom) setUnseen(0);
  };
  const jumpToLatest = () => {
    setFollow(true);
    setUnseen(0);
    const el = view.current;
    if (el) el.scrollTop = el.scrollHeight;
  };

  const toggleLevel = (level) => {
    const has = filters.levels.includes(level);
    const levels = has ? filters.levels.filter((l) => l !== level) : LOG_LEVELS.filter((l) => l === level || filters.levels.includes(l));
    if (levels.length) setParams({ levels });
  };
  const toggleRow = (seq) =>
    setExpanded((s) => {
      const n = new Set(s);
      if (n.has(seq)) n.delete(seq);
      else n.add(seq);
      return n;
    });
  const filtered = filters.component || filters.job_id || filters.request_id || filters.q || filters.levels.join(',') !== 'info,warn,error';
  const clear = () => {
    setQInput('');
    app.navigate(buildHash('logs'), { replace: true });
  };
  // GET /logs and /logs/stream answer 503 feature_unavailable when the backend
  // keeps no log history; that is a setting, not a failure to retry.
  const retentionOff = error !== null && isFeatureUnavailable(error.status, error.body);
  const sv = retentionOff
    ? { icon: 'circle-minus', role: 'neutral', label: 'Off' }
    : live
      ? connectionView(streamState)
      : { icon: 'pause', role: 'neutral', label: 'Paused' };

  let body;
  if (loading) body = html`<${SkeletonRows} n=${12} kind="log" />`;
  else if (retentionOff)
    body = html`<${Empty} icon="file-text" title=${LOG_RETENTION_OFF}>
      The backend writes records only to its standard output. Set logging.buffer_size above 0 and restart it to read them here.
    </${Empty}>`;
  else if (error && logs.entries.length === 0) body = html`<${ErrorNote} title=${error.status === 400 ? 'The filter was rejected' : "Couldn't load logs"} error=${error} />`;
  else if (logs.entries.length === 0)
    body = html`<${Empty} icon="file-text" title=${filtered ? 'No records match' : 'No log records yet'} action=${filtered ? html`<${Button} size="sm" onClick=${clear}>Clear filters</${Button}>` : null}>
      ${filtered ? null : 'Records appear here as the backend logs them. Retention is set by logging.buffer_size.'}
    </${Empty}>`;
  else
    body = [
      logs.truncated || logs.dropped
        ? html`<p key="trunc" class="log-notice"><${Icon} name="info" size="sm" />${logs.dropped
            ? `Showing the latest ${count(LOG_CAP)} records; ${count(logs.dropped)} older ones were dropped from this view.`
            : 'Older records were evicted from the in-memory buffer.'}</p>`
        : null,
      ...logs.entries.map(
        (e) => html`<${LogRow}
          key=${e.sequence}
          entry=${e}
          open=${expanded.has(e.sequence)}
          stop=${e.sequence === tabSeq}
          names=${names}
          onToggle=${() => toggleRow(e.sequence)}
          onFilter=${(patch) => setParams(patch)}
          narrow=${narrow}
        />`,
      ),
    ];

  return html`<div class="page page-logs">
    <div class="toolbar toolbar-logs">
      <${Button}
        size="sm"
        icon="chevron-down"
        className="filters-toggle"
        aria-expanded=${String(filtersOpen)}
        aria-controls="log-filters"
        onClick=${() => setFiltersOpen((o) => !o)}
      >Filters${filtered ? ' (on)' : ''}</${Button}>
      <div id="log-filters" class=${filtersOpen ? 'log-filters is-open' : 'log-filters'}>
      <div class="level-chips" role="group" aria-label="Levels">
        ${LOG_LEVELS.map((l) => {
          const lv = logLevelView(l);
          return html`<button key=${l} type="button" class="level-chip" data-role=${lv.role} aria-pressed=${String(filters.levels.includes(l))} onClick=${() => toggleLevel(l)}>
            <span class="log-dot"></span>${lv.label}
          </button>`;
        })}
      </div>
      <select class="select" aria-label="Component" value=${filters.component} onChange=${(e) => setParams({ component: e.currentTarget.value })}>
        <option value="">All components</option>
        ${LOG_COMPONENTS.map((c) => html`<option key=${c} value=${c}>${c}</option>`)}
        ${filters.component && !LOG_COMPONENTS.includes(filters.component) ? html`<option value=${filters.component}>${filters.component}</option>` : null}
      </select>
      </div>
      <label class="search">
        <${Icon} name="search" />
        <input type="search" data-filter placeholder="Search messages and attributes" aria-label="Search logs" value=${qInput} onInput=${(e) => setQInput(e.currentTarget.value)} />
      </label>
      <div class="toolbar-right">
        <${StatusText} view=${sv} />
        <${Button} size="sm" icon=${live ? 'pause' : 'play'} aria-pressed=${String(!live)} onClick=${() => setLive((v) => !v)}>${live ? 'Pause' : 'Resume'}</${Button}>
        <${Button} size="sm" variant="ghost" aria-pressed=${String(wrap)} onClick=${() => setWrap((w) => !w)}>Wrap</${Button}>
      </div>
    </div>
    ${filters.job_id || filters.request_id
      ? html`<div class="filter-chips" aria-label="Active filters">
          ${filters.job_id
            ? html`<span class="filter-chip">job: <span class="filter-chip-name" dir="auto" title=${filters.job_id}>${names.get(filters.job_id) || shortId(filters.job_id)}</span>
                <a class="filter-chip-link" href=${buildHash('queue', {}, filters.job_id)}>Open job</a>
                <button type="button" class="filter-chip-x" aria-label="Remove job filter" onClick=${() => setParams({ job_id: '' })}><${Icon} name="x" size="sm" /></button></span>`
            : null}
          ${filters.request_id
            ? html`<span class="filter-chip">request_id: <span class="mono">${filters.request_id}</span>
                <button type="button" class="filter-chip-x" aria-label="Remove request filter" onClick=${() => setParams({ request_id: '' })}><${Icon} name="x" size="sm" /></button></span>`
            : null}
        </div>`
      : null}
    ${error && !retentionOff && logs.entries.length > 0 ? html`<${ErrorNote} title="The filter was rejected" error=${error} />` : null}
    <div class="card card-flush log-card" data-section="log">
      <div class="card-body">
      <div
        class=${wrap ? 'log-view is-wrap' : 'log-view'}
        role="log"
        aria-label="Process logs"
        aria-live="off"
        ref=${view}
        onScroll=${onScroll}
        onKeyDown=${onRowKey}
        onFocusCapture=${onRowFocus}
      >
        ${body}
      </div>
      </div>
      ${unseen > 0 && !follow
        ? html`<button type="button" class="new-lines" onClick=${jumpToLatest}><${Icon} name="arrow-down" size="sm" />${count(unseen)} new ${unseen === 1 ? 'line' : 'lines'}</button>`
        : null}
    </div>
  </div>`;
}

function LogRow({ entry, open, stop, names, onToggle, onFilter, narrow }) {
  const lv = logLevelView(entry.level);
  const attrs = entry.attributes || {};
  const jobId = typeof attrs.job_id === 'string' ? attrs.job_id : '';
  const requestId = typeof attrs.request_id === 'string' ? attrs.request_id : '';
  const component = typeof attrs.component === 'string' ? attrs.component : '';
  const json = JSON.stringify(attrs, null, 2);
  // A failure's reason is on the row, not behind a click.
  const reason = typeof attrs.error === 'string' && entry.level !== 'debug' ? errorSummary(attrs.error) : '';
  const tab = stop ? 0 : -1;
  const jobName = jobId ? names.get(jobId) : '';
  return html`<div class=${open ? 'log-entry log-row is-open' : 'log-entry log-row'} data-level=${entry.level} data-seq=${entry.sequence} data-row tabindex="-1">
    <button type="button" class="log-line" tabindex=${tab} aria-expanded=${String(open)} onClick=${onToggle}>
      <span class="log-time" title=${absTime(entry.time)}>${clockTime(entry.time, !narrow)}</span>
      <span class="log-level" data-role=${lv.role}><span class="log-dot"></span>${lv.short}</span>
      ${component ? html`<span class="log-comp">${component}</span>` : html`<span class="log-comp"></span>`}
      <span class="log-msg">${entry.message}${reason && !entry.message.includes(reason) ? html` <span class="log-err" title=${attrs.error}>${reason}</span>` : null}</span>
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
