// New download: Download again of an item already in the library, after
// its question (views/download-ask.js): the record's main slots again, or
// another quality (lib/download-ask.js againCall).

import { downloadRecord, resetRecord } from '../api.js';
import { againCall } from '../lib/download-ask.js';

/**
 * Runs the answer for `resource` ({kind, id}; `songs` limits it to one
 * song), notes its run as a submit would (noteSubmitted) and watches it,
 * and toasts what happened under `name`. Resolves to the result a submit
 * gives ({status: 'accepted', resource, job}), or null.
 */
export async function runAgain({ app, resource, songs, answer, name, noteSubmitted }) {
  const { call, body } = againCall({ slot: answer.slot, songs });
  let res;
  try {
    res = call === 'download' ? await downloadRecord(resource.kind, resource.id, body) : await resetRecord(resource.kind, resource.id, body);
  } catch (error) {
    app.toast({ message: `Couldn't download “${name}” again: ${(error && error.message) || 'the request failed'}`, role: 'danger' });
    return null;
  }
  const job = res && res.job;
  if (!job || !job.id) {
    app.toast({ message: `Nothing to download for “${name}”`, role: 'warning' });
    return null;
  }
  const result = { status: 'accepted', resource, job };
  noteSubmitted([{ url: '', result }]);
  if (app.watchJobs) app.watchJobs([job.id]);
  app.toast({ message: answer.slot ? `Adding a quality to “${name}”` : `Downloading “${name}” again`, role: 'success', action: { label: 'View in Activity', hash: '#/queue' } });
  return result;
}
