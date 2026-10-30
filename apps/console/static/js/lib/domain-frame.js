// The parts every Settings domain view shares (lib/domains.js): the loading,
// missing and failed states, their tags and the number helpers. Pure.

import { count } from './format.js';
import { summarizeError } from './health.js';

export const HOUR = 60 * 60 * 1000;
export const pct = (x) => `${Math.round(x * 100)}%`;
export const plural = (n, one, many) => `${count(n)} ${n === 1 ? one : many}`;
const notFound = (e) => Boolean(e && e.error && e.error.status === 404);

const TAGS = {
  unavailable: { icon: 'circle-help', role: 'neutral', label: 'Unavailable' },
  missing: { icon: 'circle-minus', role: 'neutral', label: 'Not on this server' },
};

/** The saved config the store's snapshot holds: the token, the codec order, whether it is on disk. */
export function savedConfig(s) {
  const res = s.config && s.config.data;
  const c = (res && res.config) || null;
  return {
    known: Boolean(c),
    tokenSet: c && c.catalog ? typeof c.catalog.media_user_token === 'string' && c.catalog.media_user_token !== '' : undefined,
    priority: c && c.download && Array.isArray(c.download.quality_priority) ? c.download.quality_priority : undefined,
    persisted: res ? res.persisted !== false : undefined,
  };
}

/**
 * The common states of a domain read from one entry: loading before an
 * answer, missing on 404, failed with no data, else ok, stale (`asOf`)
 * when the last read failed or the feed is offline.
 */
export function frame(id, entry, ctx) {
  if (!entry) return { id, state: 'loading', role: null, tag: null, figure: null, viz: null, note: null, detail: '', asOf: 0, summary: null };
  if (notFound(entry)) return { id, state: 'missing', role: null, tag: TAGS.missing, figure: null, viz: null, note: null, detail: '', asOf: 0, summary: { text: 'Not on this server' } };
  if (entry.data === undefined) {
    const err = entry.error;
    return {
      id,
      state: 'failed',
      role: null,
      tag: TAGS.unavailable,
      figure: { value: "Couldn't read status", word: true },
      viz: null,
      note: err && err.message ? { text: summarizeError(err.message), title: err.message } : null,
      detail: '',
      asOf: 0,
      summary: { text: "Couldn't read status" },
    };
  }
  const stale = Boolean(entry.error) || (ctx.conn && ctx.conn.state === 'offline');
  return { id, state: 'ok', role: null, tag: null, figure: null, viz: null, note: null, detail: '', asOf: stale ? entry.okAt : 0, summary: null };
}

export const roleOf = (tagRole) => (tagRole === 'danger' || tagRole === 'warning' ? tagRole : null);
