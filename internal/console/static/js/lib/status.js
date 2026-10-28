// Pure status mapping: every state the API can report becomes one
// {icon, role, label, detail?} view, so color is never the only cue.
// Roles are the status token families in css/tokens.css.

/** @typedef {{icon: string, role: 'info'|'success'|'danger'|'warning'|'neutral'|'skipped', label: string, detail?: string, pulse?: boolean}} StatusView */

/** Counters of a job, with the 2.0 disjoint fields defaulted to 0. */
export function jobCounts(job) {
  const n = (v) => (typeof v === 'number' && v > 0 ? v : 0);
  const total = n(job && job.total_items);
  const completed = n(job && job.completed_items);
  const skipped = n(job && job.skipped_items);
  const failed = n(job && job.failed_items);
  const cancelled = n(job && job.cancelled_items);
  return { total, completed, skipped, failed, cancelled, done: completed + skipped };
}

/** Whether a completed job wrote nothing because every track was already on disk. */
export function isAlreadyInLibrary(job) {
  const c = jobCounts(job);
  return Boolean(job && job.status === 'completed' && c.total > 0 && c.completed === 0 && c.skipped === c.total);
}

/** @returns {StatusView} */
export function jobStatusView(job) {
  const status = job && job.status;
  const c = jobCounts(job);
  switch (status) {
    case 'queued':
      return { icon: 'circle-dashed', role: 'neutral', label: 'Queued' };
    case 'running':
      return { icon: 'dot', role: 'info', label: 'Running', pulse: true, detail: c.total === 0 ? 'Resolving' : undefined };
    case 'completed':
      if (isAlreadyInLibrary(job)) {
        return { icon: 'archive', role: 'skipped', label: 'Already in library', detail: 'Every track was already on disk' };
      }
      return {
        icon: 'circle-check',
        role: 'success',
        label: 'Completed',
        detail: c.skipped > 0 ? `${c.skipped} already on disk` : undefined,
      };
    case 'failed':
      return { icon: 'circle-x', role: 'danger', label: 'Failed', detail: (job && job.error) || undefined };
    case 'cancelled':
      return { icon: 'circle-slash', role: 'neutral', label: 'Cancelled' };
    default:
      return { icon: 'circle-help', role: 'neutral', label: status ? String(status) : 'Unknown' };
  }
}

const ITEM_STAGES = {
  resolving: ['search', 'Resolving'],
  downloading: ['download', 'Downloading'],
  decrypting: ['key', 'Decrypting'],
  remuxing: ['layers', 'Remuxing'],
  tagging: ['tag', 'Tagging'],
  saving: ['save', 'Saving'],
};

/** @returns {StatusView & {retry?: string}} */
export function itemStatusView(item) {
  const status = item && item.status;
  let view;
  if (ITEM_STAGES[status]) {
    const [icon, label] = ITEM_STAGES[status];
    view = { icon, role: 'info', label };
  } else {
    switch (status) {
      case 'queued':
        view = { icon: 'circle-dashed', role: 'neutral', label: 'Queued' };
        break;
      case 'waiting_download':
        view = { icon: 'clock', role: 'neutral', label: 'Waiting for download slot' };
        break;
      case 'waiting_decrypt':
        view = { icon: 'clock', role: 'neutral', label: 'Waiting for decrypt slot' };
        break;
      case 'completed':
        view = { icon: 'circle-check', role: 'success', label: 'Completed' };
        break;
      case 'skipped_existing':
        view = { icon: 'archive', role: 'skipped', label: 'Already on disk' };
        break;
      case 'failed':
        view = { icon: 'circle-x', role: 'danger', label: 'Failed', detail: (item && item.error) || undefined };
        break;
      case 'cancelled':
        view = { icon: 'circle-slash', role: 'neutral', label: 'Cancelled' };
        break;
      default:
        view = { icon: 'circle-help', role: 'neutral', label: status ? String(status) : 'Unknown' };
    }
  }
  if (item && item.attempt > 1 && !['completed', 'skipped_existing', 'cancelled'].includes(status)) {
    view.retry = item.max_attempts > 0 ? `Retry ${item.attempt}/${item.max_attempts}` : `Retry ${item.attempt}`;
  }
  return view;
}

/** @returns {StatusView} */
export function hookStatusView(status) {
  switch (status) {
    case 'running':
      return { icon: 'dot', role: 'info', label: 'Running' };
    case 'succeeded':
      return { icon: 'circle-check', role: 'success', label: 'Succeeded' };
    case 'failed':
      return { icon: 'circle-x', role: 'danger', label: 'Failed' };
    case 'interrupted':
      return { icon: 'triangle-alert', role: 'warning', label: 'Interrupted' };
    default:
      return { icon: 'circle-help', role: 'neutral', label: status ? String(status) : 'Unknown' };
  }
}

/**
 * Health rows. Each takes a fetch result {data?, error?} where error has the
 * HTTP `status` (0 for a network failure).
 */
export function backendHealthView(res) {
  if (!res || res.error) {
    if (res && res.error && res.error.status === 503 && res.error.body && res.error.body.status === 'degraded') {
      return { icon: 'circle-x', role: 'danger', label: 'Degraded', detail: res.error.body.database_error || 'Database unavailable' };
    }
    return { icon: 'circle-x', role: 'danger', label: 'Unreachable', detail: res && res.error ? res.error.message : undefined };
  }
  if (res.data.status === 'ok') return { icon: 'circle-check', role: 'success', label: 'Healthy' };
  return { icon: 'circle-x', role: 'danger', label: 'Degraded', detail: res.data.database_error || 'Database unavailable' };
}

export function wrapperView(res) {
  if (!res || res.error) {
    return { icon: 'circle-x', role: 'danger', label: 'Unreachable', detail: res && res.error ? res.error.message : undefined };
  }
  const w = res.data;
  const regions = Array.isArray(w.regions) && w.regions.length ? w.regions.join(', ').toUpperCase() : '';
  if (w.ready) {
    const clients = `${w.client_count} ${w.client_count === 1 ? 'client' : 'clients'}`;
    return { icon: 'circle-check', role: 'success', label: 'Ready', detail: regions ? `${clients} · ${regions}` : clients };
  }
  const accounts = Array.isArray(w.accounts) ? w.accounts.length : 0;
  return { icon: 'triangle-alert', role: 'warning', label: 'Not ready', detail: accounts === 0 ? 'No account logged in' : 'Wrapper is not ready' };
}

export function librarySyncView(res) {
  if (!res || res.error) {
    if (res && res.error && res.error.status === 503) {
      return { icon: 'circle-minus', role: 'neutral', label: 'Not configured' };
    }
    return { icon: 'circle-x', role: 'danger', label: 'Unavailable', detail: res && res.error ? res.error.message : undefined };
  }
  const s = res.data;
  if (!s.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (s.last_error) return { icon: 'triangle-alert', role: 'warning', label: 'Error', detail: s.last_error };
  return { icon: 'circle-check', role: 'success', label: 'Watching', detail: `Every ${s.interval_minutes} min` };
}

export function hooksView(res) {
  if (!res || res.error) {
    return { icon: 'circle-x', role: 'danger', label: 'Unavailable', detail: res && res.error ? res.error.message : undefined };
  }
  const h = res.data;
  const entries = Array.isArray(h.entries) ? h.entries : [];
  if (!h.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (entries.length === 0) return { icon: 'circle-minus', role: 'neutral', label: 'None configured' };
  const enabled = entries.filter((e) => e.enabled).length;
  return { icon: 'circle-check', role: 'success', label: 'On', detail: `${enabled} of ${entries.length} enabled` };
}

const LEVELS = {
  debug: { short: 'DBG', role: 'neutral', label: 'Debug' },
  info: { short: 'INF', role: 'neutral', label: 'Info' },
  warn: { short: 'WRN', role: 'warning', label: 'Warn' },
  error: { short: 'ERR', role: 'danger', label: 'Error' },
};

/** Log level marker: 3-letter code, role and label. */
export function logLevelView(level) {
  return LEVELS[level] || { short: String(level || '?').slice(0, 3).toUpperCase(), role: 'neutral', label: String(level || 'Unknown') };
}

/** @returns {StatusView} */
/**
 * Copy for the log routes answering 503 feature_unavailable, which the backend
 * does when logging.buffer_size is 0 and it keeps no log history.
 */
export const LOG_RETENTION_OFF = 'In-memory log retention is off (logging.buffer_size: 0)';

export function connectionView(state) {
  switch (state) {
    case 'live':
      return { icon: 'dot', role: 'success', label: 'Live' };
    case 'reconnecting':
      return { icon: 'dot', role: 'warning', label: 'Reconnecting' };
    case 'offline':
      return { icon: 'dot', role: 'danger', label: 'Disconnected' };
    case 'paused':
      return { icon: 'dot', role: 'neutral', label: 'Paused' };
    default:
      return { icon: 'dot', role: 'neutral', label: 'Connecting' };
  }
}

/**
 * Segmented progress of a job: done (completed + skipped), failed, of total.
 * No aggregate percentage; the text is a count.
 */
export function jobProgress(job) {
  const c = jobCounts(job);
  const indeterminate = c.total === 0 && job && (job.status === 'running' || job.status === 'queued');
  let aria = `${c.done} of ${c.total} tracks`;
  if (c.skipped) aria += `, ${c.skipped} already on disk`;
  if (c.failed) aria += `, ${c.failed} failed`;
  if (c.cancelled) aria += `, ${c.cancelled} cancelled`;
  return {
    ...c,
    indeterminate,
    text: indeterminate ? 'Resolving' : `${c.done} / ${c.total}`,
    aria: indeterminate ? 'Resolving tracks' : aria,
  };
}

const clamp01 = (v) => (typeof v === 'number' && v > 0 ? Math.min(1, v) : 0);

/**
 * The seven pipeline stages of one item. Download and decrypt carry their
 * own fraction; the others are done/not done. There is deliberately no
 * overall percentage (ItemProgress in the API).
 */
export function stageSegments(item) {
  const p = (item && item.progress) || {};
  const status = item && item.status;
  const finished = status === 'completed';
  const download = finished ? 1 : clamp01(p.download);
  const decrypt = finished ? 1 : clamp01(p.decrypt);
  const frac = (key, label, v, active) => {
    let state = v >= 1 ? 'done' : v > 0 ? 'partial' : 'empty';
    if (active && v === 0) state = 'indeterminate';
    return { key, label, state, fraction: v };
  };
  const flag = (key, label, on) => ({ key, label, state: on ? 'done' : 'empty', fraction: on ? 1 : 0 });
  const verify = flag('verify', 'Verify', Boolean(p.verified));
  if (finished && !p.verified) verify.state = 'unverified';
  return [
    flag('resolve', 'Resolve', Boolean(p.resolved) || finished),
    frac('download', 'Download', download, status === 'downloading'),
    frac('decrypt', 'Decrypt', decrypt, false),
    flag('remux', 'Remux', Boolean(p.remuxed) || finished),
    verify,
    flag('tag', 'Tag', Boolean(p.tagged) || finished),
    flag('save', 'Save', Boolean(p.saved) || finished),
  ];
}

/** Accessible description of the stage segments, e.g. "Download 45%, decrypt not started". */
export function stageText(segments) {
  return segments
    .map((s) => {
      if (s.state === 'unverified') return `${s.label}: not verified`;
      if (s.state === 'indeterminate') return `${s.label}: running, size unknown`;
      if (s.key === 'download' || s.key === 'decrypt') {
        if (s.state === 'partial') return `${s.label}: ${Math.round(s.fraction * 100)}%`;
      }
      return `${s.label}: ${s.state === 'done' ? 'done' : 'not started'}`;
    })
    .join(', ');
}

const TERMINAL = new Set(['completed', 'failed', 'cancelled']);

/** Whether a job status is terminal. */
export function isTerminal(status) {
  return TERMINAL.has(status);
}
