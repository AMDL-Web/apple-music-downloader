// Activity: the empty state of each tab, and of a search that matched nothing.

import { html } from '../ui/preact.js';
import { buildHash } from '../lib/route.js';
import { Empty, Button, LinkButton } from '../ui/components.js';

const TAB_LABEL = { all: 'All', active: 'Active', done: 'Done', failed: 'Failed' };

const startAction = () => html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`;

/** Props: filters, all (the All tab's count), onClear(), onShowAll(). */
export function QueueEmpty({ filters, all, onClear, onShowAll }) {
  if (filters.q || filters.type) {
    // The search covers the current tab only; the title names it.
    const inTab = filters.tab !== 'all' ? ` on the ${TAB_LABEL[filters.tab]} tab` : '';
    return html`<${Empty} icon="search" title=${`Nothing matches these filters${inTab}`} action=${html`<${Button} size="sm" onClick=${onClear}>Clear filters</${Button}>`} />`;
  }
  if (filters.tab === 'failed') return html`<${Empty} icon="circle-check" title="Nothing failed" />`;
  if (filters.tab === 'done') return html`<${Empty} icon="list" title="Nothing downloaded yet" />`;
  if (filters.tab === 'active') {
    return html`<${Empty}
      icon="list"
      title="No active downloads"
      action=${html`<div class="empty-actions">
        ${all > 0 ? html`<${Button} size="sm" onClick=${onShowAll}>See all (${all})</${Button}>` : null}
        ${startAction()}
      </div>`}
    />`;
  }
  return html`<${Empty} icon="list" title="No downloads yet" action=${startAction()} />`;
}
