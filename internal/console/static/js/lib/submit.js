// Pure submission logic for the New download page: why a link cannot be
// sent (or needs attention), the per-URL results of POST downloads
// (BatchSubmitResponse) as rows, what stays in the text box afterwards,
// live job updates from the feed, and the bookmarklet. Codes and statuses
// follow internal/api/openapi.yaml (SubmitResult, SubmitErrorCode).

import { parseLink } from './links.js';
import { PREVIEW_KINDS, shortError } from './lookup.js';
import { capitalized, sentence } from './format.js';
import { sanitizeOverrides } from './overrides.js';

/** The backend's per-request URL limit; larger batches are sent in chunks. */
export const MAX_URLS_PER_REQUEST = 100;

/**
 * Why a scanned link (lib/links.js scanLinks entry) cannot be downloaded or
 * needs attention, or null when it is fine. `lookup` is its preview status
 * (lib/lookup.js), `hasToken` whether a media-user-token is available
 * (typed on the page or set on the server). Returns
 * {code, role, blocking, message, title?}; a blocking issue keeps the link
 * out of the submission, and `title` holds a long server message in full.
 */
export function linkIssue(entry, lookup = null, { hasToken = false } = {}) {
  const link = entry && entry.link;
  if (!link) return { code: 'not_apple', role: 'danger', blocking: true, message: 'Not an Apple Music link' };
  if (link.kind === 'music-video') return { code: 'music_video', role: 'danger', blocking: true, message: 'Music videos cannot be downloaded' };
  if (link.kind === 'unknown' && link.noStorefront) {
    return { code: 'no_storefront', role: 'danger', blocking: true, message: 'No storefront in this link: add one, as in music.apple.com/us/album/…' };
  }
  if (!PREVIEW_KINDS.includes(link.kind)) {
    return { code: 'unsupported', role: 'danger', blocking: true, message: 'Not an album, song, playlist, artist or station link' };
  }
  const err = lookup && lookup.state === 'error' ? lookup.error : null;
  const msg = err ? String(err.message || '') : '';
  // Apple refuses a storefront it does not have (its 400, which the lookup
  // passes on as 502 upstream_error); a job of the link could only fail.
  // Only the word "storefront" is matched, so a reworded message still
  // counts, and the row's text is built from the link, not the message.
  if (err && (err.status === 502 || err.status === 400) && /storefront/i.test(msg)) {
    return { code: 'bad_storefront', role: 'danger', blocking: true, message: `Apple Music has no storefront “${link.storefront.toUpperCase()}”`, title: msg };
  }
  if (err && err.status === 400) return { code: 'refused', role: 'danger', blocking: true, message: capitalized(err.message) || 'The backend does not download this link' };
  // Apple answered 404 for the id: a job of this link could only fail. The
  // catalog read never carries the media-user-token (internal/applemusic
  // Playlist), so this holds for private playlists too.
  const notFound = Boolean(err && err.status === 502 && /catalog/i.test(err.message || '') && /\b404\b/.test(err.message || ''));
  if (notFound) {
    return { code: 'not_found', role: 'danger', blocking: true, message: 'Apple Music has nothing at this link', title: err.message };
  }
  // Only a station cannot download without the token; a private playlist
  // uses it for its cover alone (lib/links.js needsToken).
  const required = link.kind === 'station' || Boolean(lookup && lookup.state === 'ready' && lookup.data && lookup.data.needs_media_user_token);
  if (required && !hasToken) {
    return { code: 'needs_token', role: 'warning', blocking: false, message: link.kind === 'station' ? 'Stations need a media-user-token' : 'This link needs a media-user-token' };
  }
  if (err && err.code !== 'aborted') {
    return { code: 'preview_failed', role: 'warning', blocking: false, message: `No preview: ${shortError(err.message) || 'the lookup failed'}`, title: err.message };
  }
  return null;
}

/**
 * Whether a job of one of `urls` can use a media-user-token: a station or
 * a private playlist (lib/links.js needsToken), or a link whose preview
 * (`statusOf(url)`, a lib/lookup.js status) says it needs one.
 */
export function needsMediaUserToken(urls, statusOf = () => null) {
  return (urls || []).some((url) => {
    const link = parseLink(url);
    if (!link) return false;
    if (link.needsToken) return true;
    const s = statusOf(url);
    return Boolean(s && s.state === 'ready' && s.data && s.data.needs_media_user_token);
  });
}

/**
 * The overrides for one request of `urls`: `overrides` plus the typed
 * `token` as media_user_token, only while some of these links can use it,
 * so the token never travels with links that do not.
 */
export function requestOverrides(overrides, urls, token, statusOf) {
  const t = typeof token === 'string' ? token.trim() : '';
  return t && needsMediaUserToken(urls, statusOf) ? { ...overrides, media_user_token: t } : overrides;
}

/** The URLs to submit: every link without a blocking issue, in order. */
export function submittable(entries, issueOf) {
  return entries.filter((e) => e.link && !(issueOf(e) || {}).blocking).map((e) => e.url);
}

/** Splits a list into chunks of at most `size`. */
export function chunk(list, size = MAX_URLS_PER_REQUEST) {
  const out = [];
  for (let i = 0; i < list.length; i += size) out.push(list.slice(i, i + size));
  return out;
}

// Per-URL codes: the short state and the human reason. The server's own
// message is shown as well only where it can say more than this table.
const CODES = {
  invalid_url: ['Not accepted', 'Not an Apple Music link the backend can read'],
  invalid_configuration: ['Not accepted', 'The server configuration cannot handle this link'],
  unsupported_input: ['Not accepted', 'This kind of link cannot be downloaded'],
  unsupported_storefront: ['Storefront not served', 'The decryptor does not serve this storefront'],
  decryptor_unavailable: ['Decryptor not ready', 'The wrapper is not ready yet; try again in a moment'],
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

/**
 * Result rows for the page: [{url, view, job, deleted}] where `job` is the
 * Job of an accepted URL, kept live by applyFeedToResults.
 */
export function resultRows(pairs) {
  return pairs.map(({ url, result }) => ({
    url,
    view: result ? resultView(result) : { status: 'error', role: 'danger', icon: 'circle-x', label: 'No answer', message: 'The server did not report on this link', detail: '', retry: true },
    job: result && result.status === 'accepted' && result.job ? result.job : null,
    deleted: false,
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

// The summary's parts in order: an accepted row by its job's live status
// (or deleted), then the rows the server did not queue.
const SUMMARY = [
  ['running', 'running'],
  ['queued', 'queued'],
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
    return SUMMARY.some(([k]) => k === st) ? st : 'queued';
  }
  if (r.view.status === 'duplicate_active' || r.view.status === 'duplicate_in_request') return r.view.status;
  return 'other';
}

/**
 * One line for the toast, the live region and the Submitted card, from the
 * rows as they are now: "1 running · 2 completed · 1 already downloading ·
 * 1 not accepted". Accepted rows count by their job's live status.
 */
export function submitSummary(rows) {
  const n = new Map();
  for (const r of rows) {
    const k = summaryState(r);
    n.set(k, (n.get(k) || 0) + 1);
  }
  return (
    SUMMARY.filter(([k]) => n.get(k))
      .map(([k, text]) => `${n.get(k)} ${text}`)
      .join(' · ') || 'Nothing was submitted'
  );
}

/** sessionStorage key for the last results, so a reload of the tab keeps them (never the token). */
export const RESULTS_KEY = 'amdl.console.download.results';

const isRow = (r) =>
  Boolean(r && typeof r.url === 'string' && r.view && typeof r.view === 'object' && typeof r.view.status === 'string' && typeof r.view.label === 'string') &&
  (r.job === null || (typeof r.job === 'object' && typeof r.job.id === 'string'));

/**
 * Results as stored text: {rows, overrides} with the overrides sanitized
 * (lib/overrides.js), so a media-user-token never reaches storage. ''
 * when there are no rows.
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
    const rows = v && Array.isArray(v.rows) ? v.rows.filter(isRow).map((r) => ({ url: r.url, view: r.view, job: r.job, deleted: r.deleted === true })) : [];
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
