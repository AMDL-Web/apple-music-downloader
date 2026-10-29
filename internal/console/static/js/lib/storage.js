// Display helpers for the System page's Storage card. Pure.

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
