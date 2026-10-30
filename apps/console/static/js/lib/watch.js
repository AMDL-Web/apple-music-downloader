// Completion toasts for jobs a page asked the shell to watch
// (app.watchJobs): which feed messages end a watched job, and the one toast
// a burst of endings becomes. The shell owns the timers and the toast list.

import { buildHash } from './route.js';
import { isTerminal } from './status.js';
import { jobTitle, typeLabel } from './format.js';

/** Endings within this window after the first one share one toast. */
export const BURST_MS = 3000;

/** Pages with a job drawer route; "Open" stays on them, else it opens the Queue's. */
const DRAWER_PAGES = new Set(['overview', 'queue', 'new']);

const ENDED = { completed: 'finished', failed: 'failed', cancelled: 'was cancelled' };

/** Whether this job, if watched, has ended and gets its toast now. */
export function endsWatch(job) {
  return Boolean(job && job.id && isTerminal(job.status));
}

/** `burst` with `job` added; a job already in it is replaced, so a re-watched id counts once. */
export function addEnded(burst, job) {
  const at = burst.findIndex((j) => j.id === job.id);
  if (at < 0) return [...burst, job];
  const next = burst.slice();
  next[at] = job;
  return next;
}

/**
 * The toast for watched jobs that ended together: {message, role, action}.
 * One job names itself and opens its drawer over the current page (the
 * Queue's when the page has no drawer); several are counted and link to
 * the Queue (its Failed tab when every one failed).
 */
export function finishedToast(jobs, route = { page: 'overview', params: {} }) {
  if (jobs.length === 1) {
    const job = jobs[0];
    const onPage = DRAWER_PAGES.has(route.page);
    return {
      message: `${[typeLabel(job.type), jobTitle(job)].filter(Boolean).join(' ')} ${ENDED[job.status] || job.status}`,
      role: job.status === 'completed' ? 'success' : job.status === 'failed' ? 'danger' : 'neutral',
      action: { label: 'Open', hash: buildHash(onPage ? route.page : 'queue', onPage ? route.params : {}, job.id) },
    };
  }
  const by = (s) => jobs.filter((j) => j.status === s).length;
  const done = by('completed');
  const failed = by('failed');
  const cancelled = by('cancelled');
  const parts = [];
  if (done) parts.push(`${done} completed`);
  if (failed) parts.push(`${failed} failed`);
  if (cancelled) parts.push(`${cancelled} cancelled`);
  const message = done === jobs.length ? `${jobs.length} downloads finished` : `${jobs.length} downloads ended: ${parts.join(', ')}`;
  const role = failed === 0 ? (done > 0 ? 'success' : 'neutral') : failed === jobs.length ? 'danger' : 'warning';
  return { message, role, action: { label: 'Queue', hash: buildHash('queue', { tab: failed === jobs.length ? 'failed' : 'all' }) } };
}
