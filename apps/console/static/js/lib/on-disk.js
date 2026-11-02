// Pure helpers for what a search hit, a link row or an album shows about the
// library: a job under way first, else how much of it is on disk, from the
// server's LibraryState (POST lookup's and an album hit's `library`). The
// files decide; a finished job alone marks nothing. No DOM.

/**
 * The mark of an item: {state: 'active', job} while a job of it is queued
 * or running (`prior`, lib/search-jobs.js hitPrior or lib/lookup.js
 * priorJob), else {state: 'complete' | 'partial', onDisk, total, job} from
 * `library` (job: the item's finished job, when known, to open), else null.
 */
export function diskMark(prior, library) {
  if (prior && prior.state === 'active') return { state: 'active', job: prior.job };
  const lib = library && typeof library === 'object' ? library : null;
  if (!lib || (lib.state !== 'complete' && lib.state !== 'partial')) return null;
  const onDisk = Math.max(0, Number(lib.tracks_on_disk) || 0);
  const total = Math.max(onDisk, Number(lib.tracks_total) || 0);
  return { state: lib.state, onDisk, total, job: prior && prior.job ? prior.job : null };
}

/** "9/12": a partial mark's tracks on disk of the album's. */
export const diskFraction = (mark) => `${mark.onDisk}/${mark.total}`;

/** The files a Download again of a marked item replaces; 0 when it has none on disk. */
export const filesOnDisk = (mark) => (mark && (mark.state === 'complete' || mark.state === 'partial') ? mark.onDisk : 0);
