// Account's view for lib/domains.js: the session service and the storefronts
// its account serves. Pure.

import { summarizeError } from './health.js';
import { wrapperUnreachable, unreachableLine } from './settings-account.js';
import { frame } from './domain-frame.js';

// Account: the session service and the storefronts its account serves.
export function account(s, ctx) {
  const w = s.wrapper;
  const sys = s.system && s.system.data;
  const storefront = sys && typeof sys.default_storefront === 'string' ? sys.default_storefront.toLowerCase() : '';
  const down = w && w.error ? wrapperUnreachable(w.error) : null;
  const d = w && !down && w.data ? w.data : null;
  const regions = d && Array.isArray(d.regions) ? d.regions.filter((r) => typeof r === 'string' && r).map((r) => r.toLowerCase()) : [];
  const chips = { kind: 'chips', items: regions.map((r) => ({ label: r.toUpperCase(), outlined: r === storefront })) };
  const served = regions.length ? { value: regions.map((r) => r.toUpperCase()).join(' · '), unit: regions.length === 1 ? 'storefront' : 'storefronts' } : null;
  // Simulate jobs never call the session service (internal/media simulatedMedia).
  if (ctx.simulate && w) {
    const note = storefront ? { text: `Default storefront ${storefront.toUpperCase()}` } : null;
    return { ...frame('account', { data: {}, okAt: 0 }, ctx), asOf: 0, tag: { icon: 'circle-minus', role: 'neutral', label: 'Not used in simulate' }, figure: served || { value: 'Simulate', word: true }, viz: chips, note, summary: { text: 'Not used in simulate' } };
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
  const ready = d.ready && d.status !== false;
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
  const note = storefront
    ? unserved
      ? { text: `Default storefront ${storefront.toUpperCase()} isn't served by this account`, role: 'warning' }
      : { text: `Default storefront ${storefront.toUpperCase()}` }
    : null;
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
