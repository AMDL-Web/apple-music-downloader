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
 * The key to the chips of a track list: one {label, text} per distinct chip
 * that carries numbers ("ALAC 24/44.1" -> "Lossless 24-bit / 44.1 kHz"), in
 * first-seen order.
 */
export function codecLegend(items) {
  const seen = new Map();
  for (const it of items || []) {
    const text = codecMeaning(it);
    if (text && !seen.has(codecLabel(it))) seen.set(codecLabel(it), text);
  }
  return [...seen].map(([label, text]) => ({ label, text }));
}

/**
 * The codec mix from /stats `codecs` (most tracks first) as shares of all
 * completed tracks. Past `max` formats, the tail is folded into "Other".
 * @returns {{key: string, label: string, tracks: number, share: number}[]}
 */
export function formatShares(codecs, max = 4) {
  const rows = (Array.isArray(codecs) ? codecs : []).filter((c) => c && c.tracks > 0);
  const total = rows.reduce((n, c) => n + c.tracks, 0);
  if (total === 0) return [];
  const head = rows.length > max ? rows.slice(0, max - 1) : rows;
  const out = head.map((c) => ({
    key: [c.codec, c.bit_depth, c.sample_rate].filter((v) => v).join('-') || 'unknown',
    label: codecLabel(c) || 'Unknown',
    tracks: c.tracks,
    share: c.tracks / total,
  }));
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
