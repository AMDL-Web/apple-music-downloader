// Account's view for lib/domains.js: the session service and the storefronts
// its account serves, or what a wrapper amdl runs does instead of serving. Pure.

import { summarizeError } from './health.js';
import { wrapperUnreachable, unreachableLine, managedMode, WRAPPER_START, WRAPPER_LOGS } from './settings-account.js';
import { frame } from './domain-frame.js';

// Account: the session service and the storefronts its account serves.
export function account(s, ctx) {
  const w = s.wrapper;
  const sys = s.system && s.system.data;
  const storefront = sys && typeof sys.default_storefront === 'string' ? sys.default_storefront.toLowerCase() : '';
  const down = w && w.error ? wrapperUnreachable(w.error) : null;
  const d = w && !down && w.data ? w.data : null;
  const regions = d && Array.isArray(d.regions) ? d.regions.filter((r) => typeof r === 'string' && r).map((r) => r.toLowerCase()) : [];
  // The head names the one storefront, or counts them and names each; the
  // default storefront is the section's fact row.
  const one = regions.length === 1;
  const chips = { kind: 'chips', items: one ? [] : regions.map((r) => ({ label: r.toUpperCase() })) };
  const served = one ? { value: regions[0].toUpperCase(), unit: 'storefront' } : regions.length ? { value: String(regions.length), unit: 'storefronts' } : null;
  // Simulate jobs never call the session service (internal/media simulatedMedia).
  if (ctx.simulate && w) {
    return { ...frame('account', { data: {}, okAt: 0 }, ctx), asOf: 0, tag: { icon: 'circle-minus', role: 'neutral', label: 'Not used in simulate' }, figure: served || { value: 'Simulate', word: true }, viz: chips, summary: { text: 'Not used in simulate' } };
  }
  if (down) {
    return {
      ...frame('account', { data: {}, okAt: 0 }, ctx),
      asOf: 0,
      role: 'danger',
      tag: { icon: 'circle-x', role: 'danger', label: 'Unreachable' },
      figure: { value: 'Unreachable', word: true },
      note: { text: summarizeError(down.detail) || unreachableLine(down), title: down.detail },
      detail: summarizeError(down.detail) || '',
      summary: { text: "Can't reach the session service", role: 'danger' },
    };
  }
  const v = frame('account', w, ctx);
  if (v.state !== 'ok') return v;
  const mode = managedMode(d);
  if (mode) return { ...v, ...managedView(mode, d.managed) };
  const ready = d.ready && d.logged_in !== false;
  if (!ready) {
    return {
      ...v,
      role: 'warning',
      tag: { icon: 'triangle-alert', role: 'warning', label: 'Not logged in' },
      figure: { value: 'No account', word: true },
      viz: chips,
      note: { text: 'Log in so downloads can be decrypted', role: 'warning' },
      summary: { text: 'Not logged in', role: 'warning' },
    };
  }
  const unserved = storefront && regions.length > 0 && !regions.includes(storefront);
  const note = unserved ? { text: `Default storefront ${storefront.toUpperCase()} isn't served by this account`, role: 'warning' } : null;
  return {
    ...v,
    role: unserved ? 'warning' : null,
    tag: { icon: 'circle-check', role: 'success', label: 'Ready' },
    figure: served || { value: 'Signed in', word: true },
    viz: chips,
    note,
    detail: unserved ? note.text : '',
    summary: { text: regions.length ? `Ready · ${regions.map((r) => r.toUpperCase()).join(' · ')}` : 'Ready', role: unserved ? 'warning' : undefined },
  };
}

/** Account while a wrapper amdl runs does not serve: what it is doing, never "No account". */
function managedView(mode, m) {
  if (mode === 'stopped') {
    return {
      role: 'warning',
      tag: { icon: 'circle-minus', role: 'warning', label: 'Stopped' },
      figure: { value: 'Stopped', word: true },
      note: { text: `Start it with ${WRAPPER_START}`, role: 'warning' },
      detail: `start it with ${WRAPPER_START}`,
      summary: { text: 'Stopped', role: 'warning' },
    };
  }
  if (mode === 'failed') {
    const err = typeof m.last_error === 'string' ? m.last_error : '';
    const line = summarizeError(err) || `See ${WRAPPER_LOGS}`;
    return {
      role: 'danger',
      tag: { icon: 'circle-x', role: 'danger', label: 'Failed' },
      figure: { value: 'Failed', word: true },
      note: { text: line, role: 'danger', title: err || undefined },
      detail: line,
      summary: { text: 'Failed', role: 'danger' },
    };
  }
  const login = mode === 'login';
  return {
    tag: { icon: login ? 'key' : 'circle-dashed', role: 'neutral', label: login ? 'Logging in' : 'Starting' },
    figure: { value: login ? 'Logging in' : 'Starting', word: true },
    note: { text: login ? 'Restarting with the new login' : 'Booting on this machine' },
    summary: { text: login ? 'Logging in' : 'Starting' },
  };
}
