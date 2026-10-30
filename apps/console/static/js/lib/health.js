// Health views: the backend, the wrapper, missing media tools, library sync
// and hooks, each a StatusView (lib/status.js) built from a fetch result.

/**
 * Health rows. Each takes a fetch result {data?, error?} where error has the
 * HTTP `status` (0 for a network failure).
 */
export function backendHealthView(res) {
  if (!res || res.error) {
    if (res && res.error && res.error.status === 503 && res.error.body && res.error.body.status === 'degraded') {
      return { icon: 'circle-x', role: 'danger', label: 'Degraded', detail: res.error.body.database_error || 'Database unavailable' };
    }
    return failedView(res, 'Unreachable');
  }
  if (res.data.status === 'ok') return { icon: 'circle-check', role: 'success', label: 'Healthy' };
  return { icon: 'circle-x', role: 'danger', label: 'Degraded', detail: res.data.database_error || 'Database unavailable' };
}

/**
 * One readable line for an upstream failure. Dial errors arrive as nested
 * Go error chains that bury the cause; the full text stays available as the
 * view's title.
 */
export function summarizeError(message) {
  if (!message) return undefined;
  const text = String(message);
  const addr = /dial tcp ([^\s:]+:\d+)/.exec(text);
  const at = addr ? ` (${addr[1]})` : '';
  if (/connection refused/i.test(text)) return `Connection refused${at}`;
  if (/no such host/i.test(text)) return `Host not found${at}`;
  if (/deadline exceeded|timed? ?out/i.test(text)) return `Timed out${at}`;
  const desc = text.replace(/^rpc error: code = \w+ desc = /, '');
  return desc.length > 80 ? `${desc.slice(0, 79)}…` : desc;
}

function failedView(res, label) {
  const message = res && res.error ? res.error.message : undefined;
  return { icon: 'circle-x', role: 'danger', label, detail: summarizeError(message), title: message };
}

export function wrapperView(res) {
  if (!res || res.error) return failedView(res, 'Unreachable');
  const w = res.data;
  const regions = Array.isArray(w.regions) && w.regions.length ? w.regions.join(', ').toUpperCase() : '';
  const accounts = Array.isArray(w.accounts) ? w.accounts.length : 0;
  if (!w.ready) {
    return { icon: 'triangle-alert', role: 'warning', label: 'Not ready', detail: w.status ? 'Wrapper is not ready' : 'No account logged in' };
  }
  // A wrapper that answers but has no storefront has no account to download with.
  if (w.status === false) {
    return { icon: 'triangle-alert', role: 'warning', label: 'No account', detail: 'Log in so downloads can be decrypted' };
  }
  const signedIn = accounts === 0 ? 'Signed in' : accounts === 1 ? '1 account signed in' : `${accounts} accounts signed in`;
  return { icon: 'circle-check', role: 'success', label: 'Ready', detail: regions || signedIn };
}

/**
 * Health rows for the media tools GET /system reports missing, as
 * [name, view]. Real downloads stop at the save step without them, so a
 * missing tool is a problem unless simulate mode is on (it needs none).
 */
export function missingToolRows(system) {
  if (!system || system.simulate || !Array.isArray(system.tools)) return [];
  return system.tools
    .filter((t) => t && t.name && t.available === false)
    .map((t) => [t.name, { icon: 'circle-x', role: 'danger', label: 'Missing', detail: 'needed for every download', title: t.error || undefined }]);
}

export function librarySyncView(res) {
  if (!res || res.error) return failedView(res, 'Unavailable');
  const s = res.data;
  if (!s.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (s.last_error) return { icon: 'triangle-alert', role: 'warning', label: 'Error', detail: s.last_error };
  return { icon: 'circle-check', role: 'success', label: 'Watching', detail: `Every ${s.interval_minutes} min` };
}

export function hooksView(res) {
  if (!res || res.error) {
    return failedView(res, 'Unavailable');
  }
  const h = res.data;
  const entries = Array.isArray(h.entries) ? h.entries : [];
  if (!h.enabled) return { icon: 'circle-minus', role: 'neutral', label: 'Off' };
  if (entries.length === 0) return { icon: 'circle-minus', role: 'neutral', label: 'None configured' };
  const enabled = entries.filter((e) => e.enabled).length;
  return { icon: 'circle-check', role: 'success', label: 'On', detail: `${enabled} of ${entries.length} enabled` };
}
