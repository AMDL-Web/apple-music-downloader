// Display helpers for the storage rows of Settings and the Overview status
// line. Pure.

const PATH_SHOWN = 48;

/** A long path cut in the middle, so its start and its last folders both show. */
export function midTruncate(path, max = PATH_SHOWN) {
  if (typeof path !== 'string' || path.length <= max) return path;
  const keep = max - 1;
  const head = Math.ceil(keep * 0.4);
  return `${path.slice(0, head)}…${path.slice(path.length - (keep - head))}`;
}

/**
 * The /system storage entries grouped by disk: folders on one disk report the
 * same total and free bytes, and share one meter and one "Low space" line.
 * An entry whose space is unknown stays on its own.
 */
export function groupByDisk(storage) {
  const groups = [];
  const byDisk = new Map();
  for (const d of storage) {
    const known = typeof d.free_bytes === 'number' && typeof d.total_bytes === 'number' && d.total_bytes > 0;
    const key = known ? `${d.total_bytes}:${d.free_bytes}` : null;
    if (key !== null && byDisk.has(key)) {
      byDisk.get(key).folders.push(d);
      continue;
    }
    const group = { known, free: d.free_bytes, total: d.total_bytes, error: d.error, folders: [d] };
    groups.push(group);
    if (key !== null) byDisk.set(key, group);
  }
  return groups;
}

/** A disk's state: danger at 95 % used, warning at 85 %, neutral when its space is unknown. */
export function diskRole(group) {
  if (!group.known) return 'neutral';
  const used = 1 - group.free / group.total;
  return used >= 0.95 ? 'danger' : used >= 0.85 ? 'warning' : 'info';
}

/** The fullest disk that is low on space, or null when every disk has room. */
export function lowSpaceDisk(storage) {
  const low = groupByDisk(Array.isArray(storage) ? storage : []).filter((g) => diskRole(g) === 'danger' || diskRole(g) === 'warning');
  if (low.length === 0) return null;
  return low.reduce((a, b) => (b.free / b.total < a.free / a.total ? b : a));
}
