// Pure hash routing and filter mapping. Routes are #/, #/queue,
// #/queue/:id, #/showcase, #/showcase/:id, #/logs, #/system, #/new and
// #/settings; filters live in the hash query so a filtered view is a
// shareable link.

export const PAGES = ['overview', 'queue', 'showcase', 'logs', 'system', 'new', 'settings'];

const PAGE_PATHS = { overview: '', queue: 'queue', showcase: 'showcase', logs: 'logs', system: 'system', new: 'new', settings: 'settings' };
const PATH_PAGES = Object.fromEntries(Object.entries(PAGE_PATHS).map(([k, v]) => [v, k]));

/** Pages whose route may carry a job id that opens the job drawer. */
const DRAWER_PAGES = new Set(['queue', 'showcase']);

/**
 * Parses location.hash into {page, jobId, params}. Unknown paths give
 * page "notfound".
 */
export function parseHash(hash) {
  let h = typeof hash === 'string' ? hash : '';
  if (h.startsWith('#')) h = h.slice(1);
  const qi = h.indexOf('?');
  const path = qi >= 0 ? h.slice(0, qi) : h;
  const query = qi >= 0 ? h.slice(qi + 1) : '';
  const params = {};
  for (const [k, v] of new URLSearchParams(query)) params[k] = v;
  const parts = path.split('/').filter(Boolean);
  const page = PATH_PAGES[parts[0] || ''];
  if (!page) return { page: 'notfound', jobId: null, params };
  if (parts.length === 1 || parts.length === 0) return { page, jobId: null, params };
  if (parts.length === 2 && DRAWER_PAGES.has(page)) return { page, jobId: decodeURIComponent(parts[1]), params };
  return { page: 'notfound', jobId: null, params };
}

/** Builds a hash from a page, optional job id and params (empty values omitted). */
export function buildHash(page, params = {}, jobId = null) {
  let path = `#/${PAGE_PATHS[page] ?? ''}`;
  if (jobId && DRAWER_PAGES.has(page)) path += `${path.endsWith('/') ? '' : '/'}${encodeURIComponent(jobId)}`;
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === null || v === '' || v === false) continue;
    qs.set(k, String(v));
  }
  const q = qs.toString();
  return q ? `${path}?${q}` : path;
}

export const QUEUE_TABS = ['active', 'failed', 'all'];

const TAB_STATUSES = { active: ['running', 'queued'], failed: ['failed'], all: [] };

/** Normalizes queue route params: tab, type and q. */
export function queueFilters(params = {}) {
  const tab = QUEUE_TABS.includes(params.tab) ? params.tab : 'active';
  const type = ['album', 'playlist', 'song', 'artist', 'station'].includes(params.type) ? params.type : '';
  const q = typeof params.q === 'string' ? params.q : '';
  return { tab, type, q };
}

/** The list filter (for store.jobMatches) of a queue view. */
export function queueMatchFilter(filters) {
  return { statuses: TAB_STATUSES[filters.tab] || [], types: filters.type ? [filters.type] : [], q: filters.q };
}

/**
 * GET /api/v1/downloads query for a queue view. Active jobs read oldest
 * first, which approximates execution order; failures newest first.
 */
export function queueQuery(filters, offset = 0, limit = 50) {
  const query = { limit, offset };
  const statuses = TAB_STATUSES[filters.tab] || [];
  if (statuses.length) query.status = statuses.join(',');
  if (filters.type) query.type = filters.type;
  if (filters.q) query.q = filters.q;
  if (filters.tab === 'active') {
    query.sort = 'created_at';
    query.order = 'asc';
  } else if (filters.tab === 'failed') {
    query.sort = 'updated_at';
    query.order = 'desc';
  }
  return query;
}

/** Whether new rows go to the end of a queue view (oldest-first order). */
export function queueAppends(filters) {
  return filters.tab === 'active';
}

export const SHOWCASE_TYPES = ['album', 'playlist', 'song'];

/** Normalizes the showcase type. */
export function showcaseType(params = {}) {
  return SHOWCASE_TYPES.includes(params.type) ? params.type : 'album';
}

/** GET /api/v1/downloads query for the showcase grid. */
export function showcaseQuery(type, offset = 0, limit = 24) {
  return { status: 'completed', type, sort: 'updated_at', order: 'desc', limit, offset };
}

/**
 * The links #/new?url= carries: one or more, separated by whitespace or
 * newlines (URL-encoded in the hash). Not validated here; lib/links.js
 * parses them.
 */
export function newDownloadUrls(params = {}) {
  return typeof params.url === 'string' ? params.url.split(/\s+/).filter(Boolean) : [];
}

/** The #/new hash pre-filled with these links, newline-separated; none gives #/new. */
export function newDownloadHash(urls = []) {
  return buildHash('new', { url: urls.join('\n') });
}

/**
 * The section #/settings?section= names, or '' (the page's first section).
 * Section names are lowercase words joined by underscores; the settings page
 * owns the list, and the command palette links to "account" and
 * "library_sync".
 */
export function settingsSection(params = {}) {
  const s = params.section;
  return typeof s === 'string' && /^[a-z][a-z0-9_]*$/.test(s) ? s : '';
}

/** The #/settings hash for a section ('' for the page itself). */
export function settingsHash(section = '') {
  return buildHash('settings', { section });
}

export const LOG_LEVELS = ['debug', 'info', 'warn', 'error'];
// Every component tag the backend logs with; TestLogComponentsMatchBackend keeps
// this list equal to the tags in the Go code.
export const LOG_COMPONENTS = ['api', 'jobs', 'media', 'catalog', 'hooks', 'librarysync', 'http', 'limits', 'main'];

/** Normalizes log route params. `level` is a comma list of exact levels. */
export function logFilters(params = {}) {
  const levels = (params.level || 'info,warn,error')
    .split(',')
    .map((l) => l.trim())
    .filter((l) => LOG_LEVELS.includes(l));
  return {
    levels: levels.length ? LOG_LEVELS.filter((l) => levels.includes(l)) : ['info', 'warn', 'error'],
    component: typeof params.component === 'string' ? params.component : '',
    job_id: typeof params.job_id === 'string' ? params.job_id : '',
    request_id: typeof params.request_id === 'string' ? params.request_id : '',
    q: typeof params.q === 'string' ? params.q : '',
  };
}

/** Route params for log filters (the inverse of logFilters). */
export function logParams(filters) {
  const level = filters.levels.join(',');
  return {
    level: level === 'info,warn,error' ? '' : level,
    component: filters.component,
    job_id: filters.job_id,
    request_id: filters.request_id,
    q: filters.q,
  };
}

/** GET /api/v1/logs and /logs/stream query for log filters. */
export function logQuery(filters, extra = {}) {
  const query = { level: filters.levels.join(',') };
  if (filters.component) query.component = filters.component;
  if (filters.job_id) query.job_id = filters.job_id;
  if (filters.request_id) query.request_id = filters.request_id;
  if (filters.q) query.q = filters.q;
  return { ...query, ...extra };
}

/** Encodes a query object as a URL query string (no leading "?"). */
export function encodeQuery(query = {}) {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) {
    if (v === undefined || v === null || v === '') continue;
    qs.set(k, String(v));
  }
  return qs.toString();
}
