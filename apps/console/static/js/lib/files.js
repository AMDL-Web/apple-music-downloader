// Where a finished job's files are, from the `output_path` of its items: the
// absolute path of each track whose file is on disk (completed or
// skipped_existing; the API omits it for any other item). Pure.

// A Windows server hands out `C:\...` or `\\host\...` paths, where both
// separators work; any other path is POSIX, where a backslash is part of a name.
const isWindowsPath = (path) => /^[A-Za-z]:[\\/]/.test(path) || path.startsWith('\\\\');

/** The folder that holds the file at `path`: '' when it has none, the root for a file directly under it. */
export function parentDir(path) {
  const win = isWindowsPath(path);
  const cut = win ? Math.max(path.lastIndexOf('/'), path.lastIndexOf('\\')) : path.lastIndexOf('/');
  if (cut < 0) return '';
  if (cut === 0) return path.slice(0, 1);
  if (win && cut === 2 && path[1] === ':') return path.slice(0, 3);
  return path.slice(0, cut);
}

// Folders are the same folder when they differ only in separator style, and on
// Windows in case.
const folderKey = (dir) => (isWindowsPath(dir) ? dir.replace(/\\/g, '/').toLowerCase() : dir);

/**
 * The distinct folders the finished tracks of `items` were written to, in the
 * order they first appear, each spelled as its first track spells it. An album
 * or a single song gives one; a playlist across albums or an artist gives
 * several (so does an album whose name template puts discs in subfolders); a
 * job with no finished file gives none.
 */
export function outputFolders(items) {
  const seen = new Map();
  for (const item of items || []) {
    const path = item && item.output_path;
    if (typeof path !== 'string' || path === '') continue;
    const dir = parentDir(path);
    if (dir !== '' && !seen.has(folderKey(dir))) seen.set(folderKey(dir), dir);
  }
  return [...seen.values()];
}
