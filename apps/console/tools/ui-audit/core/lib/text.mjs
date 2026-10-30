// Text rules: noise values, explanatory prose, truncation that hides what
// tells siblings apart. Pure; injected into the page like geometry.mjs.

const NOISE = [
  [/^(0|0\.0+)$/, 'a bare zero'],
  [/^0\s+(?:[A-Za-z][\w-]*)(?:\s+[A-Za-z][\w-]*)?$/, 'a zero count'],
  [/^(0|0\.0+)\s*(B|KB|MB|GB|bytes|tracks?|jobs?|items?|%)$/i, 'a zero amount'],
  [/^[—–-]$/, 'a dash placeholder'],
  [/^(none|n\/a|na|null|undefined|nan|unknown|never|empty|nil)$/i, 'a placeholder word'],
  [/\b(undefined|NaN|\[object Object\]|Invalid Date|null)\b/, 'a leaked programming value'],
];

/** Why `text` reads as noise, or null. */
export function noiseReason(text) {
  const t = String(text || '').replace(/\s+/g, ' ').trim();
  if (!t) return null;
  for (const [re, why] of NOISE) if (re.test(t)) return why;
  return null;
}

const HOW_TO = /\b(you can|click|tap|press|paste|use this|this (?:page|card|panel|section|list|setting|is where)|here you|helps you|lets you|allows you|to do this|learn more|for example)\b/i;

/**
 * Whether a line of UI text reads as an explanation (a sentence about the UI)
 * rather than data, status or an action: several words, sentence-shaped, or
 * an instruction.
 */
export function proseScore(text) {
  const t = String(text || '').replace(/\s+/g, ' ').trim();
  const words = t.split(' ').filter((w) => /[A-Za-z]{2,}/.test(w));
  if (words.length < 6) return 0;
  let score = 0;
  if (/[.!?]$/.test(t) || /\. [A-Z]/.test(t)) score += 1;
  if (words.length >= 10) score += 1;
  if (HOW_TO.test(t)) score += 2;
  const digits = (t.match(/\d/g) || []).length;
  if (digits / t.length > 0.15) score -= 2;
  return score;
}

/**
 * Siblings truncated to the same visible prefix: the ellipsis hides the part
 * that tells them apart. items: [{text, shown}] where shown is the number of
 * characters that fit.
 */
export function hiddenDistinctions(items) {
  const groups = new Map();
  for (const it of items) {
    if (!(it.shown < it.text.length)) continue;
    const prefix = it.text.slice(0, Math.max(1, Math.floor(it.shown))).trim().toLowerCase();
    if (!groups.has(prefix)) groups.set(prefix, []);
    groups.get(prefix).push(it);
  }
  return [...groups.entries()].filter(([, g]) => g.length > 1 && new Set(g.map((x) => x.text)).size > 1).map(([prefix, g]) => ({ prefix, texts: g.map((x) => x.text) }));
}
