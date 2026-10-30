// Motion covers: Apple serves an album's animated cover as an HLS master
// playlist (job.motion_artwork_url), which only Safari plays. Each of its
// video renditions is one fragmented MP4 file cut into byte ranges, and that
// file plays in any browser as a plain <video src>. These helpers read the
// playlists and name the MP4 to play. Pure, so node:test covers them.

const attrs = (line) => {
  const out = {};
  for (const m of line.matchAll(/([A-Z0-9-]+)=("[^"]*"|[^,]*)/g)) out[m[1]] = m[2].replace(/^"|"$/g, '');
  return out;
};

const absolute = (uri, base) => {
  try {
    return new URL(uri, base).href;
  } catch {
    return '';
  }
};

/**
 * The video renditions of a master playlist: [{width, height, codecs,
 * bandwidth, url}] in playlist order. I-frame (trick play) playlists are
 * left out; relative URIs resolve against `base`.
 */
export function motionVariants(master, base) {
  const lines = String(master || '').split(/\r?\n/);
  const out = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim();
    if (!line.startsWith('#EXT-X-STREAM-INF:')) continue;
    const a = attrs(line.slice('#EXT-X-STREAM-INF:'.length));
    const uri = (lines[i + 1] || '').trim();
    const res = /^(\d+)x(\d+)$/.exec(a.RESOLUTION || '');
    if (!res || !uri || uri.startsWith('#')) continue;
    const url = absolute(uri, base);
    if (!url) continue;
    out.push({ width: Number(res[1]), height: Number(res[2]), codecs: a.CODECS || '', bandwidth: Number(a.BANDWIDTH) || 0, url });
  }
  return out;
}

/**
 * The rendition to play in a box `px` device pixels wide: the smallest H.264
 * (avc1) one whose shorter side reaches `px`, the lowest bandwidth among
 * equals; the largest when none does. HEVC renditions are skipped, since
 * not every browser decodes them. null without an H.264 rendition.
 */
export function pickMotionVariant(variants, px) {
  const avc = (variants || []).filter((v) => /(^|,)\s*avc1\./.test(v.codecs));
  if (avc.length === 0) return null;
  const side = (v) => Math.min(v.width, v.height);
  const big = avc.filter((v) => side(v) >= px).sort((a, b) => side(a) - side(b) || a.bandwidth - b.bandwidth);
  if (big.length) return big[0];
  return avc.sort((a, b) => side(b) - side(a) || a.bandwidth - b.bandwidth)[0];
}

/**
 * The single MP4 file a rendition's media playlist cuts into byte ranges,
 * or '' when the segments are separate files (no one file to play).
 */
export function motionFile(media, base) {
  const lines = String(media || '').split(/\r?\n/).map((l) => l.trim());
  const map = lines.find((l) => l.startsWith('#EXT-X-MAP:'));
  if (!map) return '';
  const file = attrs(map.slice('#EXT-X-MAP:'.length)).URI;
  if (!file) return '';
  const segments = lines.filter((l) => l && !l.startsWith('#'));
  if (segments.length === 0 || segments.some((s) => s !== file)) return '';
  return absolute(file, base);
}
