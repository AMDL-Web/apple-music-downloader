// Codec labels for chips, legends and the formats bar. Pure; format.js
// re-exports everything here.

const codecNames = {
  alac: 'ALAC',
  aac: 'AAC',
  'aac-lc': 'AAC-LC',
  'aac-binaural': 'AAC Binaural',
  'aac-downmix': 'AAC Downmix',
  ec3: 'Dolby Atmos',
  ac3: 'Dolby Audio',
};

/** Sample rate in Hz as kHz text: 96000 -> "96", 44100 -> "44.1". */
export function kHz(sampleRate) {
  if (!(sampleRate > 0)) return '';
  const k = sampleRate / 1000;
  return Number.isInteger(k) ? String(k) : String(Math.round(k * 10) / 10);
}

/** Codec chip text from codec, bit_depth and sample_rate: "ALAC 24/96", "AAC". */
export function codecLabel(item) {
  if (!item || !item.codec) return '';
  const name = codecNames[item.codec] || item.codec.toUpperCase();
  if (item.bit_depth > 0 && item.sample_rate > 0) return `${name} ${item.bit_depth}/${kHz(item.sample_rate)}`;
  return name;
}

/**
 * What a codec chip's numbers mean, in the words of the Qualities sheet
 * (lib/lookup.js qualitySpec): "Lossless 24-bit / 44.1 kHz" for ALAC, the
 * bare "24-bit / 48 kHz" for another codec; empty without bit depth and rate.
 */
export function codecMeaning(item) {
  if (!item || !(item.bit_depth > 0 && item.sample_rate > 0)) return '';
  const spec = `${item.bit_depth}-bit / ${kHz(item.sample_rate)}\u00a0kHz`;
  return item.codec === 'alac' ? `Lossless ${spec}` : spec;
}

/**
 * The codec mix from /stats `codecs` (most tracks first) as shares of all
 * completed tracks. Past `max` formats, the tail is folded into "Other".
 * @returns {{key: string, label: string, tracks: number, share: number}[]}
 */
export function formatShares(codecs, max = 4) {
  const counted = (Array.isArray(codecs) ? codecs : []).filter((c) => c && c.tracks > 0);
  const total = counted.reduce((n, c) => n + c.tracks, 0);
  if (total === 0) return [];
  // /stats groups by sample rate too, but a lossy track's chip carries none:
  // AAC tracks with different rates (or none, from before the rate was read)
  // would otherwise show as several bars all labelled "AAC".
  const byKey = new Map();
  for (const c of counted) {
    const label = codecLabel(c) || 'Unknown';
    const key = c.bit_depth > 0 ? [c.codec, c.bit_depth, c.sample_rate].filter((v) => v).join('-') : c.codec || 'unknown';
    const row = byKey.get(key);
    if (row) row.tracks += c.tracks;
    else byKey.set(key, { key, label, tracks: c.tracks });
  }
  const rows = [...byKey.values()].sort((a, b) => b.tracks - a.tracks);
  const head = rows.length > max ? rows.slice(0, max - 1) : rows;
  const out = head.map((r) => ({ ...r, share: r.tracks / total }));
  if (rows.length > max) {
    const tracks = rows.slice(max - 1).reduce((n, c) => n + c.tracks, 0);
    out.push({ key: 'other', label: 'Other', tracks, share: tracks / total });
  }
  return out;
}

/** Whether an item's format is high resolution (more than 16-bit/48 kHz). */
export function isHiRes(item) {
  return Boolean(item && item.bit_depth >= 24 && item.sample_rate > 48000);
}
