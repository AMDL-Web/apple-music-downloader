// What a download asks before it goes, by the desktop app's rules
// (apps/desktop/internal/ui/download.go): the quality, when the release has
// both lossless and Dolby Atmos audio and the server's quality_priority lists
// ALAC and a Dolby codec; and, for Download again, a confirmation that names
// the files it replaces. A plain download never touches files already on
// disk: the server skips them. Pure: no DOM, so node --test covers it.

const LOSSLESS = ['lossless', 'hi-res-lossless'];
const DOLBY = ['ec3', 'ac3'];

/** The question's name for a codec. */
export const CHOICE_LABEL = { alac: 'Lossless', ec3: 'Dolby Atmos', ac3: 'Dolby Digital' };

/**
 * The codecs to choose between, in the server's priority order (the first
 * is the default), or [] when there is no choice: the release's audio_traits
 * need lossless and atmos, and quality_priority ALAC and ec3 or ac3.
 */
export function codecChoices(traits, priority) {
  const t = Array.isArray(traits) ? traits : [];
  if (!t.some((x) => LOSSLESS.includes(x)) || !t.includes('atmos')) return [];
  const p = Array.isArray(priority) ? priority : [];
  if (!p.includes('alac') || !p.some((c) => DOLBY.includes(c))) return [];
  return p.filter((c) => c === 'alac' || DOLBY.includes(c));
}

/**
 * What to ask before downloading: {codecs, again}. `follow` is false when
 * the page's quality menu names a quality, which then needs no question;
 * `again` is the number of files on disk a Download again replaces (0 for a
 * plain download), which always asks.
 */
export function downloadAsks({ traits, priority, follow = true, again = 0 }) {
  return { codecs: follow ? codecChoices(traits, priority) : [], again: Math.max(0, Number(again) || 0) };
}

/** Whether a download has a question to ask. */
export const hasAsks = (asks) => Boolean(asks && (asks.codecs.length > 0 || asks.again > 0));

/**
 * The overrides an answer adds: quality_priority led by the chosen codec,
 * then the rest of the server's order; force_overwrite for Download again.
 */
export function answerOverrides({ codec = '', priority = [], again = 0 }) {
  const out = {};
  if (codec) out.quality_priority = [codec, ...priority.filter((c) => c !== codec)];
  if (again > 0) out.force_overwrite = true;
  return out;
}

/** The one line Download again's confirmation says. */
export const againText = (n) => (n === 1 ? 'Replaces the file on disk.' : `Replaces the ${n} files on disk.`);
