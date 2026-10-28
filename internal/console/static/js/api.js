// Thin client for the public /api/v1 contract (internal/api/openapi.yaml).
// Paths are written out in full so the Go contract guard in console_test.go
// can check each one against the spec. They resolve against the console's
// own location, so the app also works behind a path prefix.

import { encodeQuery } from './lib/route.js';

/** An error response: HTTP status (0 for a network failure) plus the 2.0 error envelope. */
export class ApiError extends Error {
  constructor(status, code, message, details, body) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.details = details;
    this.body = body;
  }
}

const REQUEST_TIMEOUT_MS = 15000;

/** Resolves an "/api/..." or "/docs" path against the server root above /console/. */
export function resolveUrl(path, query, base = globalThis.document ? globalThis.document.baseURI : 'http://localhost/console/') {
  const root = new URL('../', base);
  const url = new URL(path.replace(/^\//, ''), root);
  const qs = encodeQuery(query);
  if (qs) url.search = qs;
  return url.toString();
}

/** Turns a non-2xx body into an ApiError, reading {error: {code, message, details}}. */
export function toApiError(status, body, fallback) {
  const err = body && body.error;
  if (err && typeof err === 'object') {
    return new ApiError(status, err.code || '', err.message || fallback, err.details, body);
  }
  if (typeof err === 'string') return new ApiError(status, '', err, undefined, body);
  return new ApiError(status, '', fallback, undefined, body);
}

async function request(method, path, query) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS);
  let res;
  try {
    res = await fetch(resolveUrl(path, query), {
      method,
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      signal: controller.signal,
    });
  } catch (e) {
    clearTimeout(timer);
    const timedOut = e && e.name === 'AbortError';
    throw new ApiError(0, timedOut ? 'timeout' : 'network_error', timedOut ? 'The server did not answer in time.' : 'Cannot reach the server.');
  }
  let body = null;
  try {
    const text = await res.text();
    body = text ? JSON.parse(text) : null;
  } catch {
    body = null;
  } finally {
    clearTimeout(timer);
  }
  if (!res.ok) throw toApiError(res.status, body, `${method} ${path} failed with HTTP ${res.status}.`);
  return body;
}

/** Runs a request and returns {data} or {error}, for widgets that render either. */
export async function settle(promise) {
  try {
    return { data: await promise };
  } catch (error) {
    return { error };
  }
}

export const getHealth = () => request('GET', '/api/v1/health');
export const getSystem = () => request('GET', '/api/v1/system');
export const getStats = (days = 14) => request('GET', '/api/v1/stats', { days });
export const getConfig = () => request('GET', '/api/v1/config');
export const reloadConfig = () => request('POST', '/api/v1/config/reload');
export const getHooks = () => request('GET', '/api/v1/hooks');
export const getLibrarySync = () => request('GET', '/api/v1/library-sync');
export const getWrapperStatus = () => request('GET', '/api/v1/wrapper/status');
export const listDownloads = (query) => request('GET', '/api/v1/downloads', query);
export const getDownload = (id) => request('GET', `/api/v1/downloads/${encodeURIComponent(id)}`);
export const cancelDownload = (id) => request('POST', `/api/v1/downloads/${encodeURIComponent(id)}/cancel`);
export const retryDownload = (id) => request('POST', `/api/v1/downloads/${encodeURIComponent(id)}/retry`);
export const listLogs = (query) => request('GET', '/api/v1/logs', query);

/** Stream URLs; StreamClient appends ?cursor=. */
export const feedStreamUrl = () => resolveUrl('/api/v1/downloads/events');
export const jobStreamUrl = (id) => resolveUrl(`/api/v1/downloads/${encodeURIComponent(id)}/events`);
export const logStreamUrl = (query) => resolveUrl('/api/v1/logs/stream', query);

/** Links outside the console. */
export const docsUrl = () => resolveUrl('/docs');
export const specUrl = () => resolveUrl('/api/openapi.yaml');

/** Parses SSE data as JSON, or null. */
export function parseData(ev) {
  try {
    return JSON.parse(ev.data);
  } catch {
    return null;
  }
}
