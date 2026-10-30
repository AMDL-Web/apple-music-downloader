// Queue: the empty state of each tab, and of a search that matched nothing.

import { html } from '../ui/preact.js';
import { buildHash } from '../lib/route.js';
import { Empty, Button, LinkButton } from '../ui/components.js';

const startAction = () => html`<${LinkButton} variant="primary" size="sm" icon="plus" href=${buildHash('new')}>Start a download</${LinkButton}>`;

export function QueueEmpty({ filters, tabs, onClear, onShowAll }) {
  if (filters.q || filters.type) {
    // The search covers the current tab only; say so, and which tab.
    const tab = tabs.find((t) => t.value === filters.tab);
    const inTab = filters.tab !== 'all' && tab ? ` on the ${tab.label} tab` : '';
    return html`<${Empty}
      icon="search"
      title=${`No jobs match these filters${inTab}`}
      action=${html`<${Button} size="sm" onClick=${onClear}>Clear filters</${Button}>`}
    >${filters.tab !== 'all' ? 'Other tabs may match; All searches every job.' : null}</${Empty}>`;
  }
  if (filters.tab === 'failed') return html`<${Empty} icon="circle-check" title="No failed jobs" />`;
  if (filters.tab === 'completed') return html`<${Empty} icon="list" title="No completed jobs yet" />`;
  if (filters.tab === 'cancelled') return html`<${Empty} icon="list" title="No cancelled jobs" />`;
  if (filters.tab === 'active') {
    const all = tabs.find((t) => t.value === 'all');
    return html`<${Empty}
      icon="list"
      title="No active downloads"
      action=${html`<div class="empty-actions">
        ${all && all.count > 0 ? html`<${Button} size="sm" onClick=${onShowAll}>See all jobs (${all.count})</${Button}>` : null}
        ${startAction()}
      </div>`}
    />`;
  }
  return html`<${Empty} icon="list" title="No jobs yet" action=${startAction()} />`;
}
