// Per-account answers of the multi-account API, folded back into the one-
// account shapes the console's views were written for. Pure.

import { keysToLabels } from './settings-draft.js';

/** "name · REGION", the region alone when the name is empty, else the name. */
export function accountLabel(a) {
  if (!a) return '';
  const region = typeof a.region === 'string' ? a.region.toUpperCase() : '';
  return [a.name, region].filter(Boolean).join(' · ');
}

/** The accounts of GET /accounts, [] when the answer is missing or not a list. */
export function accountList(body) {
  return body && Array.isArray(body.accounts) ? body.accounts : [];
}

/** Whether an account of GET /accounts has amdl copy its Apple Music library, what the Library page shows. */
export function appleLibraryOn(body) {
  return accountList(body).some((a) => a.apple_library === true);
}

/**
 * The regions the logged-in accounts serve: {regions, fallback}. `regions`
 * are the distinct regions of the accounts not missing, lowercase;
 * `fallback` is the default account's (else the first's), the region an
 * empty storefront means.
 */
export function accountRegions(body) {
  const live = accountList(body).filter((a) => a && a.state !== 'missing' && typeof a.region === 'string' && a.region);
  const regions = [...new Set(live.map((a) => a.region.toLowerCase()))];
  const def = live.find((a) => a.default) || live[0];
  return { regions, fallback: def ? def.region.toLowerCase() : '' };
}

/**
 * A row's region tag ("JP") for `storefront`, or '' while the accounts
 * serve one region or none, where every row is the same region and the tag
 * would say nothing. An empty storefront is the default account's region.
 */
export function regionTag(storefront, info) {
  if (!info || info.regions.length < 2) return '';
  return (storefront || info.fallback || '').toUpperCase();
}

/**
 * The entry the single-account views read from GET /apple-library/status:
 * the default account's, else the first, else null.
 */
export function defaultAppleAccount(status) {
  const list = status && Array.isArray(status.accounts) ? status.accounts : [];
  return list.find((a) => a && a.default) || list[0] || null;
}

/**
 * The entry a single-account view reads of GET /library-sync, summed over
 * its accounts: one account gives its own figures, several give the tracks
 * and albums added up, the latest check and the first error of the accounts
 * that are polling. `enabled` and `interval_minutes` stay the master switch
 * and the interval.
 */
export function flattenLibrarySync(sync) {
  if (!sync) return sync;
  const list = Array.isArray(sync.accounts) ? sync.accounts : [];
  const polling = list.filter((a) => a.enabled);
  const sum = (key, from = list) => from.reduce((n, a) => n + (Number(a[key]) || 0), 0);
  const stamps = polling.map((a) => Date.parse(a.last_poll_at)).filter((t) => !Number.isNaN(t));
  const failed = polling.find((a) => a.last_error);
  return {
    enabled: sync.enabled,
    interval_minutes: sync.interval_minutes,
    anchor_size: sum('anchor_size'),
    last_submitted: sum('last_submitted'),
    total_submitted: sum('total_submitted'),
    last_poll_at: stamps.length ? new Date(Math.max(...stamps)).toISOString() : undefined,
    last_error: failed ? failed.last_error : '',
  };
}

/** One short line per account when there are several; nothing for one, whose figures are the head's. */
export function librarySyncLines(sync) {
  const list = sync && Array.isArray(sync.accounts) ? sync.accounts : [];
  if (list.length < 2) return [];
  return list.map((a) => {
    const prefix = accountLabel(a) || a.account_id;
    let text;
    if (!a.enabled) text = 'off';
    else if (a.last_error) text = keysToLabels(a.last_error);
    else text = [a.anchor_size > 0 ? `${a.anchor_size} tracks seen` : 'not anchored yet', a.total_submitted > 0 ? `${a.total_submitted} albums since start` : ''].filter(Boolean).join(' · ');
    return { id: a.account_id, label: prefix, text, role: a.enabled && a.last_error ? 'danger' : undefined };
  });
}
