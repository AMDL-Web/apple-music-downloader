// A row's region as a small tag ("JP") while the logged-in accounts serve
// two regions or more (lib/accounts.js regionTag); with one, nothing is drawn.

import { html } from '../ui/preact.js';
import { accountRegions } from '../lib/accounts.js';
import { useStatus } from '../ui/system-status.js';

/** The regions of GET /accounts as lib/accounts.js accountRegions reads them, kept live. */
export function useRegions() {
  const { accounts } = useStatus(['accounts']);
  return accountRegions(accounts && accounts.data);
}

/** The tag of `region` (from regionTag); nothing for ''. */
export function RegionTag({ region }) {
  return region ? html`<span class="chip region-tag" title=${`Region ${region}`}>${region}</span>` : null;
}
