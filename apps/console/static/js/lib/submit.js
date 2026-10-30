// Pure submission logic for the New download page: the per-URL results of
// POST downloads (BatchSubmitResponse) as rows, what stays in the text box
// afterwards, live job updates from the feed, and the bookmarklet. Codes
// and statuses follow internal/api/openapi.yaml (SubmitResult,
// SubmitErrorCode). The checks before a submit are lib/submit-check.js.

import { parseLink } from './links.js';
import { sentence } from './format.js';
import { sanitizeOverrides } from './overrides.js';
import { summarizeError } from './health.js';

/** The backend's per-request URL limit; larger batches are sent in chunks. */
export const MAX_URLS_PER_REQUEST = 100;

/** Splits a list into chunks of at most `size`. */
export function chunk(list, size = MAX_URLS_PER_REQUEST) {
  const out = [];
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size));
  return out;
}

// Per-URL codes: the short state and the human reason. The server's own
// message is shown as well only where it can say more than this table.
export const CODES = {
  invalid_url: ['Not accepted', 'Not an Apple Music link the backend can read'],
  invalid_configuration: ['Not accepted', 'The server configuration cannot handle this link'],
  unsupported_input: ['Not accepted', 'This kind of link cannot be downloaded'],
  unsupported_storefront: ['Storefront not served', "The wrapper's Apple Music account is in another storefront"],
  decryptor_unavailable: ['Wrapper not ready', 'The wrapper has no account ready yet; try again in a moment'],
  duplicate_in_request: ['Listed twice', 'An earlier link in this batch downloads the same thing'],
  duplicate_active: ['Already downloading', ''],
  queue_full: ['Queue full', 'The job queue is full; try again shortly'],
  store_error: ['Server error', 'The server could not save the job'],
  internal_error: ['Server error', 'The server could not check this link'],
};
const SERVER_DETAIL = new Set(['invalid_url', 'invalid_configuration', 'unsupported_input', 'store_error', 'internal_error']);
const RETRYABLE = new Set(['queue_full', 'decryptor_unavailable', 'store_error', 'internal_error']);
const ROLES = { duplicate_active: 'info', duplicate_in_request: 'neutral', queue_full: 'warning', decryptor_unavailable: 'warning' };
const ICONS = { duplicate_active: 'rotate', duplicate_in_request: 'layers', queue_full: 'clock', decryptor_unavailable: 'triangle-alert' };
const UNREACHABLE = /cannot reach the wrapper|connection refused|no such host|deadline exceeded|timed? ?out/i;

/**
 * How one SubmitResult reads: {status, code, role, icon, label, message,
 * detail, retry, existingJobId, storefronts}. `label` is the short state,
 * `message` the human reason for the code, `detail` the server's message
 * where it says more (store errors, parse failures, unknown codes);
 * `retry` marks results a later submit may fix.
 */
export function resultView(result) {
  const r = result || {};
  const err = r.error || {};
  if (r.status === 'accepted') return { status: 'accepted', code: '', role: 'success', icon: 'circle-check', label: 'Queued', message: '', detail: '', retry: false };
  const code = err.code || r.status || '';
  const known = CODES[code];
  const isError = r.status === 'error';
  // decryptor_unavailable covers a wrapper that answers without an account
  // and one that does not answer at all; only the second needs a look at
  // wrapper.address, and waiting does not fix it.
  if (code === 'decryptor_unavailable' && UNREACHABLE.test(err.message || '')) {
    return {
      status: r.status || 'invalid', code, role: 'danger', icon: 'circle-x', label: 'Wrapper unreachable',
      message: 'The backend cannot reach the wrapper at wrapper.address; check that it is running',
      detail: summarizeError(err.message) || '', retry: true, existingJobId: '', storefronts: [],
    };
  }
  return {
    status: r.status || 'error',
    code,
    role: ROLES[code] || 'danger',
    icon: ICONS[code] || 'circle-x',
    label: known ? known[0] : isError ? 'Server error' : 'Not accepted',
    message: known ? known[1] : sentence(err.message) || 'The server did not accept this link',
    detail: known && SERVER_DETAIL.has(code) ? sentence(err.message) : '',
    retry: RETRYABLE.has(code) || r.status === 'queue_full' || isError,
    existingJobId: r.status === 'duplicate_active' && r.existing_job_id ? r.existing_job_id : '',
    storefronts: err.details && Array.isArray(err.details.supported_storefronts) ? err.details.supported_storefronts : [],
  };
}

const normalized = (url) => {
  const link = parseLink(url);
  return link ? link.url : url;
};

/**
 * Pairs the URLs sent with the results: by position when the counts match
 * (results are in request order), else by normalized URL. Returns
 * [{url, result}] in the order sent; a URL without a result gets null.
 */
export function pairResults(sent, results) {
  const list = Array.isArray(results) ? results : [];
  if (list.length === sent.length) return sent.map((url, i) => ({ url, result: list[i] }));
  const byUrl = new Map();
  for (const r of list) if (r && r.url && !byUrl.has(normalized(r.url))) byUrl.set(normalized(r.url), r);
  return sent.map((url) => ({ url, result: byUrl.get(normalized(url)) || null }));
}

const DONE = new Set(['accepted', 'duplicate_active', 'duplicate_in_request']);

/** The sent URLs to keep in the text box: those not queued, not already downloading and not a repeat. */
export function keptAfterSubmit(pairs) {
  return pairs.filter((p) => !p.result || !DONE.has(p.result.status)).map((p) => p.url);
}

/** The sent URLs the server took or already runs: those leave the text box (keptAfterSubmit keeps the rest). */
export function handledUrls(pairs) {
  const done = new Set(pairs.map((p) => p.url));
  for (const u of keptAfterSubmit(pairs)) done.delete(u);
  return done;
}

// The preview fields a job row shows before Apple names the job: the same
// names on LookupResponse and Job.
const PREVIEW_KEYS = [
  'title',
  'artist_name',
  'curator_name',
  'artwork_url',
  'release_date',
  'genre',
  'artwork_bg_color',
  'artwork_text_color1',
  'artwork_text_color2',
  'artwork_text_color3',
  'artwork_text_color4',
];

/** The preview fields of a LookupResponse a result row keeps, or null. */
export function previewSeed(data) {
  if (!data || typeof data !== 'object') return null;
  const out = {};
  for (const key of PREVIEW_KEYS) if (typeof data[key] === 'string' && data[key]) out[key] = data[key];
  return out.title ? out : null;
}

/** `job` with the preview's fields where the job has none yet (its own win once it resolves). */
export function seedJob(job, preview) {
  if (!job || !preview) return job;
  const out = { ...job };
  for (const key of PREVIEW_KEYS) if (!out[key] && preview[key]) out[key] = preview[key];
  return out;
}

/**
 * Result rows for the page: [{url, view, job, deleted, preview}] where `job`
 * is the Job of an accepted URL, kept live by applyFeedToResults, and
 * `preview` the link's preview fields (previewOf(url), previewSeed), shown
 * until the job resolves.
 */
export function resultRows(pairs, previewOf = () => null) {
  return pairs.map(({ url, result }) => ({
    url,
    view: result ? resultView(result) : { status: 'error', role: 'danger', icon: 'circle-x', label: 'No answer', message: 'The server did not report on this link', detail: '', retry: true },
    job: result && result.status === 'accepted' && result.job ? result.job : null,
    deleted: false,
    preview: previewSeed(previewOf(url)),
  }));
}

const time = (v) => {
  const t = Date.parse(v || '');
  return Number.isNaN(t) ? 0 : t;
};

/**
 * Applies a batch of feed messages to result rows: a snapshot of a job a
 * row holds replaces it unless it is older; a deleted job marks its row.
 * Jobs the rows do not hold are ignored.
 */
export function applyFeedToResults(rows, batch) {
  let next = rows;
  for (const msg of batch || []) {
    if (!msg) continue;
    const id = msg.type === 'download_deleted' ? msg.job_id : msg.job && msg.job.id;
    if (!id) continue;
    const i = next.findIndex((r) => r.job && r.job.id === id);
    if (i < 0) continue;
    const row = next[i];
    let updated = row;
    if (msg.type === 'download_deleted') updated = { ...row, deleted: true };
    else if (msg.type === 'download_upserted' && time(msg.job.updated_at) >= time(row.job.updated_at)) updated = { ...row, job: msg.job };
    if (updated !== row) {
      next = next.slice();
      next[i] = updated;
    }
  }
  return next;
}

// The summary's parts after "Started N downloads": accepted jobs that have
// finished (or were deleted), then the rows the server did not queue.
// Queued and running jobs are what "started" means, so they get no part of
// their own and the words never contradict the list.
const SUMMARY = [
  ['completed', 'completed'],
  ['failed', 'failed'],
  ['cancelled', 'cancelled'],
  ['deleted', 'deleted'],
  ['duplicate_active', 'already downloading'],
  ['duplicate_in_request', 'listed twice'],
  ['other', 'not accepted'],
];

/** The state a result row counts as in the summary. */
function summaryState(r) {
  if (r.view.status === 'accepted') {
    if (r.deleted) return 'deleted';
    const st = r.job && r.job.status;
    return SUMMARY.some(([k]) => k === st) ? st : 'started';
  }
  if (r.view.status === 'duplicate_active' || r.view.status === 'duplicate_in_request') return r.view.status;
  return 'other';
}

/**
 * One line for the toast, the live region and the Submitted card, from the
 * rows as they are now: "Started 3 downloads · 1 completed · 1 already
 * downloading · 1 not accepted". Every accepted row counts as started;
 * those that finished are named again by how.
 */
export function submitSummary(rows) {
  const n = new Map();
  for (const r of rows) {
    const k = summaryState(r);
    n.set(k, (n.get(k) || 0) + 1);
  }
  const started = rows.filter((r) => r.view.status === 'accepted').length;
  const parts = SUMMARY.filter(([k]) => n.get(k)).map(([k, text]) => `${n.get(k)} ${text}`);
  if (started) parts.unshift(`Started ${started} ${started === 1 ? 'download' : 'downloads'}`);
  return parts.join(' · ') || 'Nothing was submitted';
}

/** sessionStorage key for the last results, so a reload of the tab keeps them (never the token). */
export const RESULTS_KEY = 'amdl.console.download.results';

const isRow = (r) =>
  Boolean(r && typeof r.url === 'string' && r.view && typeof r.view === 'object' && typeof r.view.status === 'string' && typeof r.view.label === 'string') &&
  (r.job === null || (typeof r.job === 'object' && typeof r.job.id === 'string'));

/**
 * Results as stored text: {rows, overrides} with the overrides sanitized
 * (lib/overrides.js). '' when there are no rows.
 */
export function storedResults(results) {
  const rows = results && Array.isArray(results.rows) ? results.rows.filter(isRow) : [];
  if (!rows.length) return '';
  return JSON.stringify({ rows, overrides: sanitizeOverrides(results.overrides) });
}

/** Parses stored results (lib/prefs.js readPref normalizer); null when absent or unusable. */
export function parseStoredResults(raw) {
  if (typeof raw !== 'string' || !raw) return null;
  try {
    const v = JSON.parse(raw);
    const rows = v && Array.isArray(v.rows)
      ? v.rows.filter(isRow).map((r) => ({ url: r.url, view: r.view, job: r.job, deleted: r.deleted === true, preview: previewSeed(r.preview) }))
      : [];
    return rows.length ? { rows, overrides: sanitizeOverrides(v.overrides) } : null;
  } catch {
    return null;
  }
}

/**
 * What a failed POST downloads means for the page:
 * - {kind: 'results', response, retryable}: no URL was accepted, and
 *   details carries the BatchSubmitResponse (422 no_urls_accepted, 500
 *   store_error or internal_error, which a retry may fix);
 * - {kind: 'overrides', message}: 422 invalid_overrides;
 * - {kind: 'error', message}: anything else, worded for the page.
 */
export function classifySubmitError(err) {
  const e = err || {};
  const d = e.details;
  if (d && typeof d === 'object' && Array.isArray(d.results)) {
    return { kind: 'results', response: d, retryable: e.status >= 500 };
  }
  if (e.code === 'invalid_overrides') return { kind: 'overrides', message: e.message || 'The options were refused' };
  if (e.status === 413) return { kind: 'error', message: 'The request is too large. Submit fewer links at a time.' };
  if (e.status === 400) return { kind: 'error', message: `The server refused the request: ${e.message || 'invalid request'}` };
  return { kind: 'error', message: e.message || 'The download could not be submitted' };
}

/**
 * The bookmarklet that sends the current Apple Music page here:
 * javascript:location.href='<console>#/new?url='+encodeURIComponent(location.href).
 * `pageUrl` is this console's address (any path prefix); its hash is dropped
 * and quotes and backslashes are escaped so the string stays one literal.
 */
export function bookmarklet(pageUrl) {
  const base = String(pageUrl || '').split('#')[0].replace(/\\/g, '%5C').replace(/'/g, '%27');
  return `javascript:location.href='${base}#/new?url='+encodeURIComponent(location.href)`;
}
