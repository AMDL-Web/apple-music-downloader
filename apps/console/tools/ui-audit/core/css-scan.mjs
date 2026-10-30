// Static pass over the stylesheets: the design scale read from token
// declarations, then every literal that bypasses it and every transition on
// a layout property, each with file:line so the fix has an address.

import { readFileSync } from 'node:fs';
import { relative } from 'node:path';
import { nearestStep } from './lib/geometry.mjs';

/** Declarations as {prop, value, line} with comments removed; handles values spanning lines. */
export function declarations(css) {
  const out = [];
  const text = css.replace(/\/\*[\s\S]*?\*\//g, (m) => m.replace(/[^\n]/g, ' '));
  const re = /([a-z-]+|--[\w-]+)\s*:\s*([^;{}]+);/g;
  let m;
  while ((m = re.exec(text))) {
    const line = text.slice(0, m.index).split('\n').length;
    out.push({ prop: m[1], value: m[2].replace(/\s+/g, ' ').trim(), line });
  }
  return out;
}

/** The scale: every token matching a pattern whose value is a plain px length. */
export function readScale(files, patterns) {
  const scale = {};
  for (const [name, re] of Object.entries(patterns)) scale[name] = new Set([0]);
  for (const f of files) {
    for (const d of declarations(readFileSync(f, 'utf8'))) {
      for (const [name, re] of Object.entries(patterns)) {
        const m = d.value.match(/^(-?[\d.]+)px$/);
        if (re.test(d.prop) && m) scale[name].add(+m[1]);
      }
    }
  }
  const out = {};
  for (const [k, v] of Object.entries(scale)) out[k] = [...v].sort((a, b) => a - b);
  return out;
}

const SPACING = /^(padding|margin|gap|row-gap|column-gap)(-|$)/;
const RADIUS = /^border(-[a-z]+)*-radius$/;
const FONT = /^font-size$/;
const LAYOUT_TRANSITION = /\b(all|width|height|min-height|max-height|min-width|max-width|top|left|right|bottom|inset|margin[\w-]*|padding[\w-]*|grid-template[\w-]*|flex[\w-]*|font-size|line-height|border-width)\b/;

/**
 * findings for literal lengths and layout transitions.
 * opts: {root, scale: {space, radius, font}, allowValues: [px], skip: (file, decl) => bool}
 */
export function scanCss(files, { root = process.cwd(), scale, allowValues = [0, 1], skip = () => false } = {}) {
  const out = [];
  for (const f of files) {
    const file = relative(root, f);
    for (const d of declarations(readFileSync(f, 'utf8'))) {
      if (d.prop.startsWith('--') || skip(file, d)) continue;
      const kind = SPACING.test(d.prop) ? 'space' : RADIUS.test(d.prop) ? 'radius' : FONT.test(d.prop) ? 'font' : null;
      if (kind && !/calc\(|clamp\(|min\(|max\(/.test(d.value)) {
        const bare = d.value.replace(/var\([^)]*\)/g, '');
        for (const lit of bare.matchAll(/(-?[\d.]+)px/g)) {
          const v = Math.abs(+lit[1]);
          if (allowValues.includes(v)) continue;
          const { step, off } = nearestStep(v, scale[kind]);
          out.push({
            check: 'tokens',
            file,
            line: d.line,
            sel: `${file}:${d.line}`,
            msg: off <= 0.5 ? `${d.prop}: ${d.value} writes ${v}px as a literal; the scale has it as a token` : `${d.prop}: ${d.value} uses ${v}px, off the ${kind} scale (nearest ${step}px)`,
            data: { prop: d.prop, value: d.value, px: v, nearest: step, onScale: off <= 0.5 },
            severity: off <= 0.5 ? 'low' : 'medium',
          });
        }
      }
      if ((d.prop === 'transition' || d.prop === 'transition-property') && LAYOUT_TRANSITION.test(d.value.replace(/var\([^)]*\)|cubic-bezier\([^)]*\)/g, ''))) {
        const what = d.value.match(LAYOUT_TRANSITION)[1];
        out.push({ check: 'flicker', file, line: d.line, sel: `${file}:${d.line}`, msg: `${d.prop}: ${d.value} animates ${what === 'all' ? 'every property' : `a layout property (${what})`}`, data: { prop: d.prop, value: d.value }, severity: what === 'all' ? 'medium' : 'medium' });
      }
    }
  }
  return out;
}
