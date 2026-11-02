// Activity: the records held out of the queue, under the runs. Paused (n)
// lists paused albums and playlists with Resume; Couldn't add (n) lists the
// adds the resolver gave up on (a song without its album, an artist) with
// Retry and Remove. Each group is drawn only while it has rows, below the
// runs, so its arrival moves nothing above it. Kept fresh by the feed's
// resource_updated and the shell's epoch.

import { html, useState, useEffect, useRef } from '../ui/preact.js';
import { listAlbumRecords, listPlaylistRecords, resumeRecord, listPendingAdds, retryPendingAdd, deletePendingAdd } from '../api.js';
import { recordHash } from '../lib/route.js';
import { pendingTitle } from '../lib/records.js';
import { errorSummary } from '../lib/format.js';
import { useApp, Card, Button } from '../ui/components.js';
import { RecordRow } from './record-row.js';

const LIMIT = 50;
const REFRESH_MS = 1500;

/** Reads the paused records and the failed adds; refreshes on the feed's record news. */
function useHeld(app) {
  const [held, setHeld] = useState({ paused: [], failed: [] });
  const [stamp, setStamp] = useState(0);
  useEffect(() => {
    let live = true;
    Promise.allSettled([
      listAlbumRecords({ flag: 'paused', limit: LIMIT }),
      listPlaylistRecords({ flag: 'paused', limit: LIMIT }),
      listPendingAdds({ state: 'failed' }),
    ]).then(([a, p, f]) => {
      if (!live) return;
      const items = (r, kind) => (r.status === 'fulfilled' && r.value && Array.isArray(r.value.items) ? r.value.items.map((x) => ({ ...x, kind })) : null);
      setHeld((h) => ({
        paused: items(a, 'albums') && items(p, 'playlists') ? [...items(a, 'albums'), ...items(p, 'playlists')] : h.paused,
        failed: f.status === 'fulfilled' && f.value && Array.isArray(f.value.items) ? f.value.items : h.failed,
      }));
    });
    return () => {
      live = false;
    };
  }, [stamp, app.epoch, app.recordsStamp]);
  const timer = useRef(null);
  useEffect(() => {
    const off = app.subscribeFeed((batch) => {
      if (!batch.some((m) => m.type === 'resource_updated')) return;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setStamp((n) => n + 1), REFRESH_MS);
    });
    return () => {
      off();
      clearTimeout(timer.current);
    };
  }, []);
  return [held, () => setStamp((n) => n + 1)];
}

export function HeldGroups() {
  const app = useApp();
  const [held, reload] = useHeld(app);
  const [busy, setBusy] = useState('');
  const act = async (key, fn, done) => {
    setBusy(key);
    try {
      await fn();
      if (done) app.toast({ message: done, role: 'success' });
    } catch (e) {
      app.toast({ message: `Couldn't do that: ${(e && e.message) || 'the request failed'}`, role: 'danger' });
    }
    setBusy('');
    reload();
  };
  return [
    held.paused.length
      ? html`<${Card} key="paused" section="paused" className="card-flush" title=${`Paused (${held.paused.length})`} labelId="held-paused">
          <ul class="job-list">
            ${held.paused.map(
              (r) => html`<li key=${`${r.kind}:${r.id}`} class="job-item" data-action="">
                <${RecordRow} record=${r} href=${recordHash(r.kind, r.id)} bare />
                <${Button} size="sm" icon="play" className="job-retry" disabled=${busy === r.id} onClick=${() => act(r.id, () => resumeRecord(r.kind, r.id), 'Resumed')}>Resume</${Button}>
              </li>`,
            )}
          </ul>
        </${Card}>`
      : null,
    held.failed.length
      ? html`<${Card} key="failed" section="couldnt-add" className="card-flush" title=${`Couldn't add (${held.failed.length})`} labelId="held-failed">
          <ul class="held-list">
            ${held.failed.map(
              (p) => html`<li key=${p.id} class="held-row" data-row>
                <span class="held-main">
                  <span class="held-title">${pendingTitle(p)}</span>
                  ${p.last_error ? html`<span class="job-error" title=${p.last_error}>${errorSummary(p.last_error)}</span>` : null}
                </span>
                <span class="held-actions">
                  <${Button} size="sm" icon="rotate" disabled=${busy === `p${p.id}`} onClick=${() => act(`p${p.id}`, () => retryPendingAdd(p.id), 'Reading it again')}>Retry</${Button}>
                  <${Button} size="sm" variant="ghost" icon="x" aria-label=${`Remove ${pendingTitle(p)}`} title="Remove" disabled=${busy === `p${p.id}`} onClick=${() => act(`p${p.id}`, () => deletePendingAdd(p.id))} />
                </span>
              </li>`,
            )}
          </ul>
        </${Card}>`
      : null,
  ];
}
