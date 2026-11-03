// What a download asks before it goes, by the desktop app's rules
// (apps/desktop/internal/ui/download.go): the quality, when the release has
// both lossless and Dolby Atmos audio and the server's quality_priority lists
// ALAC and a Dolby codec; and, for an item already in the library, whether
// to download its tracks again (a reset of its record, replacing the files)
// or add another quality beside them (another slot). A plain download only
// fetches what the record does not have. Pure: no DOM, so node --test
// covers it.

import { codecName } from './overrides.js';

const LOSSLESS = ['lossless', 'hi-res-lossless'];
const DOLBY = ['ec3', 'ac3'];

/** The question's name for a codec: its name everywhere in the console (lib/overrides.js codecName). */
export const CHOICE_LABEL = Object.fromEntries(['alac', 'ec3', 'ac3', 'aac', 'aac-binaural', 'aac-downmix'].map((c) => [c, codecName(c)]));

// The codecs another slot may be, in the order the menu lists them.
const SLOT_CODECS = ['alac', 'ec3', 'ac3', 'aac', 'aac-binaural', 'aac-downmix'];

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
 * The codecs another quality of a release may be: those its audio_traits
 * offer (every one when it names none). A Dolby codec needs atmos, ALAC
 * lossless.
 */
export function slotChoices(traits) {
  const t = Array.isArray(traits) ? traits : [];
  if (t.length === 0) return SLOT_CODECS.slice();
  return SLOT_CODECS.filter((c) => (c === 'alac' ? t.some((x) => LOSSLESS.includes(x)) : DOLBY.includes(c) ? t.includes('atmos') : true));
}

/**
 * What to ask before downloading: {codecs, again, slots}. `follow` is false
 * when the page's quality menu names a quality, which then needs no
 * question; `again` is the number of downloaded tracks a Download again
 * replaces (0 for a plain download), which always asks and offers another
 * quality (`slots`) instead.
 */
export function downloadAsks({ traits, priority, follow = true, again = 0 }) {
  const n = Math.max(0, Number(again) || 0);
  if (n > 0) return { codecs: [], again: n, slots: slotChoices(traits) };
  return { codecs: follow ? codecChoices(traits, priority) : [], again: 0, slots: [] };
}

/** Whether a download has a question to ask. */
export const hasAsks = (asks) => Boolean(asks && (asks.codecs.length > 0 || asks.again > 0));

/** The overrides an answer adds: quality_priority led by the chosen codec, then the rest of the server's order. */
export function answerOverrides({ codec = '', priority = [] }) {
  return codec ? { quality_priority: [codec, ...priority.filter((c) => c !== codec)] } : {};
}

/**
 * The record call an answer to Download again makes: another quality as a
 * slot ({call: 'download', body: {codec, songs?}}) when `slot` names a
 * codec, else the main slots again ({call: 'reset', body: {what: 'audio',
 * slot: 'main', songs?}}). `songs` limits it to one song of an album.
 */
export function againCall({ slot = '', songs } = {}) {
  const only = Array.isArray(songs) && songs.length ? { songs } : {};
  if (slot) return { call: 'download', body: { codec: slot, ...only } };
  return { call: 'reset', body: { what: 'audio', slot: 'main', ...only } };
}

/** The one line Download again's confirmation says. */
export const againText = (n) => (n === 1 ? 'Replaces the downloaded file.' : `Replaces the ${n} downloaded files.`);
