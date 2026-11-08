// The Queue's Failed tab heads with Couldn't add (n): the adds the resolver
// gave up on (a song without its album, an artist), each with Retry and
// Remove, its region tag first while the accounts serve two regions or
// more. Drawn only while it has rows.

import { html, useState } from '../ui/preact.js';
import { retryPendingAdd, deletePendingAdd } from '../api.js';
import { pendingTitle } from '../lib/records.js';
import { errorSummary } from '../lib/format.js';
import { regionTag } from '../lib/accounts.js';
import { useApp, Card, Button } from '../ui/components.js';
import { RegionTag, useRegions } from './region-tag.js';

/** Props: items (GET /library/pending?state=failed), onChanged() after an action. */
export function FailedAdds({ items, onChanged }) {
  const app = useApp();
  const [busy, setBusy] = useState('');
  const regions = useRegions();
  if (!items.length) return null;
  const act = async (key, fn, done) => {
    setBusy(key);
    try {
      await fn();
      if (done) app.toast({ message: done, role: 'success' });
    } catch (e) {
      app.toast({ message: `Couldn't do that: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy('');
    onChanged();
  };
  return html`<${Card} section="couldnt-add" className="card-flush" title=${`Couldn't add (${items.length})`} labelId="held-failed">
    <ul class="held-list">
      ${items.map(
        (p) => html`<li key=${p.id} class="held-row" data-row>
          <span class="held-main">
            <span class=${regionTag(p.storefront, regions) ? 'held-title has-region' : 'held-title'}><${RegionTag} region=${regionTag(p.storefront, regions)} /><span>${pendingTitle(p)}</span></span>
            ${p.last_error ? html`<span class="job-error" title=${p.last_error}>${errorSummary(p.last_error)}</span>` : null}
          </span>
          <span class="held-actions">
            <${Button} size="sm" icon="rotate" disabled=${busy === `p${p.id}`} onClick=${() => act(`p${p.id}`, () => retryPendingAdd(p.id), 'Reading it again')}>Retry</${Button}>
            <${Button} size="sm" variant="ghost" icon="x" aria-label=${`Remove ${pendingTitle(p)}`} title="Remove" disabled=${busy === `p${p.id}`} onClick=${() => act(`p${p.id}`, () => deletePendingAdd(p.id))} />
          </span>
        </li>`,
      )}
    </ul>
  </${Card}>`;
}
