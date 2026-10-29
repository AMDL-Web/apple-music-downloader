// One entry per release: jobs of the same link target (the album, playlist or
// song a job downloads) collapse to the first one listed. Pure.

import { parseLink } from './links.js';
import { linkTarget } from './lookup.js';

/** "type:id" of what a job downloads, or null when its input is not a catalog link. */
export function jobTargetKey(job) {
  const target = linkTarget(parseLink(job && job.input));
  return target ? `${target.type}:${target.id}` : null;
}

/**
 * `jobs` without repeats of the same target, keeping the first of each, so a
 * list sorted newest first keeps the newest. A job whose input is not a
 * catalog link is never merged.
 */
export function uniqueByTarget(jobs) {
  const seen = new Set();
  return jobs.filter((job) => {
    const key = jobTargetKey(job);
    if (key === null) return true;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
