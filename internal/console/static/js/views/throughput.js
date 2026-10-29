// Throughput: the /stats card on the Overview, with totals, the formats mix
// and two per-day small multiples. Under SPARSE_DAYS days with data the card
// is one sentence instead: a chart of one bar says nothing.

import { html, useState } from '../ui/preact.js';
import { getStats } from '../api.js';
import { count, bytes, dayLabel, formatShares } from '../lib/format.js';
import { useApp, Card, Empty, ErrorNote, Skeleton, Unavailable, Facts } from '../ui/components.js';
import { usePoll } from '../ui/hooks.js';

const STATS_DAYS = 14;
const SPARSE_DAYS = 3;

const hasData = (d) => (d.tracks_completed || 0) > 0 || (d.bytes_written || 0) > 0 || (d.jobs_completed || 0) > 0 || (d.jobs_failed || 0) > 0;

/** "246 tracks, 11.1 GB", for a window that has little history; the card head names the period. */
export function sparseSummary(totals) {
  const tracks = totals.tracks_completed || 0;
  const failed = totals.tracks_failed ? `, ${count(totals.tracks_failed)} failed` : '';
  if (tracks === 0 && !(totals.bytes_written > 0)) return 'Nothing downloaded.';
  return `${count(tracks)} ${tracks === 1 ? 'track' : 'tracks'}, ${bytes(totals.bytes_written || 0)}${failed}.`;
}

export function ThroughputCard() {
  const app = useApp();
  const poll = usePoll(() => getStats(STATS_DAYS), 60000, [app.epoch]);
  if (poll.error && poll.error.status === 404) {
    return html`<${Card} section="throughput" title="Throughput" labelId="throughput-title"><${Unavailable} what="Throughput history (GET /api/v1/stats)" /></${Card}>`;
  }
  const stats = poll.data;
  const totals = stats && stats.totals;
  const period = `Last ${STATS_DAYS} days`;
  const daily = (stats && stats.daily) || [];
  const jobsDone = daily.reduce((n, d) => n + (d.jobs_completed || 0), 0);
  const jobsFailed = daily.reduce((n, d) => n + (d.jobs_failed || 0), 0);
  if (totals && daily.filter(hasData).length < SPARSE_DAYS) {
    return html`<${Card} section="throughput" title="Throughput" labelId="throughput-title" note=${period}>
      <p class="tp-sparse">${sparseSummary(totals)}</p>
    </${Card}>`;
  }
  const today = stats && typeof stats.generated_at === 'string' ? stats.generated_at.slice(0, 10) : '';
  return html`<${Card}
    section="throughput"
    title="Throughput"
    labelId="throughput-title"
    note=${period}
  >
    ${totals
      ? html`<${Facts}
          className="tp-totals"
          label=${`Totals, ${period.toLowerCase()}`}
          rows=${[
            ['tracks', 'Tracks completed', count(totals.tracks_completed)],
            totals.tracks_failed ? ['tracks-failed', 'Tracks failed', count(totals.tracks_failed), 'danger'] : null,
            ['bytes', 'Written to disk', bytes(totals.bytes_written)],
            ['jobs', 'Jobs finished', count(jobsDone)],
            jobsFailed ? ['jobs-failed', 'Jobs failed', count(jobsFailed), 'danger'] : null,
          ].filter(Boolean)}
        />`
      : null}
    ${stats ? html`<${Formats} codecs=${stats.codecs} />` : null}
    ${stats
      ? html`<${Throughput} daily=${daily} today=${today} />`
      : poll.error
        ? html`<${ErrorNote} title="Couldn't load stats" error=${poll.error} onRetry=${poll.reload} />`
        : html`<${Skeleton} kind="chart" />`}
  </${Card}>`;
}

/**
 * The codec mix of every retained job (/stats `codecs`): a stacked bar
 * (decorative) and a list with label and track count. Nothing renders
 * before any track has completed.
 */
function Formats({ codecs }) {
  const shares = formatShares(codecs);
  if (shares.length === 0) return null;
  return html`<div class="tp-formats" role="group" aria-labelledby="tp-formats-title">
    <p class="tp-formats-title" id="tp-formats-title">Formats, all jobs</p>
    <div class="formats-bar" aria-hidden="true">
      ${shares.map((f, i) => html`<span key=${f.key} class="formats-seg" data-key=${f.key} style=${{ '--w': String(f.share), '--i': String(i) }}></span>`)}
    </div>
    <ul class="formats-list">
      ${shares.map(
        (f, i) => html`<li key=${f.key} class="formats-item" data-key=${f.key} style=${{ '--i': String(i) }}>
          <span class="formats-swatch" aria-hidden="true"></span>
          <span class="formats-label">${f.label}</span>
          <span class="formats-count">${count(f.tracks)} ${f.tracks === 1 ? 'track' : 'tracks'}</span>
        </li>`,
      )}
    </ul>
  </div>`;
}

/** Compact tick value: 1284 -> "1.3K". */
function compact(n) {
  if (n >= 1e6) return `${(n / 1e6).toFixed(n >= 1e7 ? 0 : 1)}M`;
  if (n >= 1e3) return `${(n / 1e3).toFixed(n >= 1e4 ? 0 : 1)}K`;
  return String(n);
}

/**
 * Two small multiples sharing the date axis: tracks completed and bytes
 * written per day. Two measures, two charts, never a second y-axis.
 */
export function Throughput({ daily, today = '' }) {
  const [hover, setHover] = useState(null);
  if (daily.length === 0) return html`<${Empty} icon="info" title="No history yet" />`;
  const shown = hover !== null ? daily[hover] : daily[daily.length - 1];
  const maxTracks = Math.max(0, ...daily.map((d) => d.tracks_completed || 0));
  const maxBytes = Math.max(0, ...daily.map((d) => d.bytes_written || 0));
  const mid = Math.floor((daily.length - 1) / 2);
  const series = [
    { key: 'tracks', title: 'Tracks completed per day', field: 'tracks_completed', max: maxTracks, fmt: (v) => compact(v), whole: true },
    { key: 'bytes', title: 'Data written per day', field: 'bytes_written', max: maxBytes, fmt: (v) => bytes(v), whole: false },
  ];
  // The max/2 gridline sits at half height, so its label is exactly max/2; a
  // count series shows it only when that is a whole number of tracks.
  const hasMid = (s) => s.max >= 2 && (!s.whole || s.max % 2 === 0);
  return html`<div class="chart" onMouseLeave=${() => setHover(null)}>
    <p class="chart-readout" aria-live="off">
      <span class="chart-readout-day">${dayLabel(shown.date)}${hover === null ? (shown.date === today ? ' (today so far)' : ' (latest day)') : ''}</span>
      <span>${count(shown.tracks_completed || 0)} tracks</span>
      <span>${bytes(shown.bytes_written || 0)}</span>
      <span>${count(shown.jobs_completed || 0)} jobs completed${shown.jobs_failed ? `, ${count(shown.jobs_failed)} failed` : ''}</span>
    </p>
    ${series.map(
      (s) => html`<figure key=${s.key} class="chart-multiple" aria-hidden="true">
        <figcaption class="chart-title">${s.title}</figcaption>
        <div class="chart-plot">
          <div class="chart-yaxis">
            <span>${s.fmt(s.max)}</span>
            ${hasMid(s) ? html`<span>${s.fmt(s.max / 2)}</span>` : null}
            <span>0</span>
          </div>
          <div class=${hasMid(s) ? 'chart-bars has-mid' : 'chart-bars'} style=${{ '--n': String(daily.length) }}>
            ${daily.map((d, i) => {
              const v = d[s.field] || 0;
              return html`<div
                key=${d.date}
                class=${hover === i ? 'chart-col is-hover' : 'chart-col'}
                onMouseEnter=${() => setHover(i)}
              >
                <span class=${v > 0 ? 'chart-bar' : 'chart-bar is-zero'} style=${{ '--v': String(s.max > 0 ? v / s.max : 0) }}></span>
              </div>`;
            })}
          </div>
        </div>
      </figure>`,
    )}
    <div class="chart-xaxis" aria-hidden="true" style=${{ '--n': String(daily.length) }}>
      ${daily.map((d, i) => {
        const last = i === daily.length - 1;
        const cls = [i === 0 ? 'is-first' : '', i === mid && i > 0 && !last ? 'is-mid' : '', last ? 'is-latest' : ''].filter(Boolean).join(' ');
        return html`<span key=${d.date} class=${cls || undefined} data-day=${String(Number(d.date.slice(8, 10)) || '')}>${dayLabel(d.date)}</span>`;
      })}
    </div>
    <div class="sr-only"><table>
      <caption>Completed tracks and bytes written per day (UTC)</caption>
      <thead><tr><th scope="col">Date</th><th scope="col">Tracks completed</th><th scope="col">Bytes written</th><th scope="col">Jobs completed</th><th scope="col">Jobs failed</th></tr></thead>
      <tbody>
        ${daily.map(
          (d) => html`<tr key=${d.date}><th scope="row">${d.date}</th><td>${d.tracks_completed || 0}</td><td>${bytes(d.bytes_written || 0)}</td><td>${d.jobs_completed || 0}</td><td>${d.jobs_failed || 0}</td></tr>`,
        )}
      </tbody>
    </table></div>
  </div>`;
}
