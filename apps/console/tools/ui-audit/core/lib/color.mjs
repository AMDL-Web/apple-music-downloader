// Colour parsing, compositing and WCAG contrast. Pure and dependency-free:
// the probe injects this file into the page (export keywords stripped), and
// the Node tests import it as is.

export function parseColor(s) {
  if (!s || s === 'transparent' || s === 'none') return { r: 0, g: 0, b: 0, a: 0 };
  let m = s.match(/^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)(?:\s*[,/]\s*([\d.]+%?))?\s*\)$/);
  if (m) return { r: +m[1], g: +m[2], b: +m[3], a: alpha(m[4]) };
  m = s.match(/^color\(srgb\s+([-\d.e]+)\s+([-\d.e]+)\s+([-\d.e]+)(?:\s*\/\s*([\d.]+%?))?\s*\)$/);
  if (m) return { r: clamp255(+m[1] * 255), g: clamp255(+m[2] * 255), b: clamp255(+m[3] * 255), a: alpha(m[4]) };
  m = s.match(/^oklab\(\s*([-\d.e]+%?)\s+([-\d.e]+)\s+([-\d.e]+)(?:\s*\/\s*([\d.]+%?))?\s*\)$/);
  if (m) return { ...oklabToRgb(pct(m[1]), +m[2], +m[3]), a: alpha(m[4]) };
  m = s.match(/^oklch\(\s*([-\d.e]+%?)\s+([-\d.e]+)\s+([-\d.e]+)(?:deg)?(?:\s*\/\s*([\d.]+%?))?\s*\)$/);
  if (m) {
    const h = (+m[3] * Math.PI) / 180;
    return { ...oklabToRgb(pct(m[1]), +m[2] * Math.cos(h), +m[2] * Math.sin(h)), a: alpha(m[4]) };
  }
  return null;
}

function pct(v) {
  return v.endsWith('%') ? parseFloat(v) / 100 : +v;
}

function alpha(v) {
  if (v === undefined) return 1;
  return v.endsWith('%') ? parseFloat(v) / 100 : +v;
}

function clamp255(v) {
  return Math.max(0, Math.min(255, Math.round(v)));
}

function oklabToRgb(L, a, b) {
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const m = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s];
  const enc = (x) => clamp255(255 * (x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1 / 2.4) - 0.055));
  return { r: enc(lin[0]), g: enc(lin[1]), b: enc(lin[2]) };
}

/** `top` drawn over `bottom` (both {r,g,b,a}); the result's alpha is the union. */
export function over(top, bottom) {
  const a = top.a + bottom.a * (1 - top.a);
  if (a === 0) return { r: 0, g: 0, b: 0, a: 0 };
  const mix = (t, b) => (t * top.a + b * bottom.a * (1 - top.a)) / a;
  return { r: mix(top.r, bottom.r), g: mix(top.g, bottom.g), b: mix(top.b, bottom.b), a };
}

export function luminance({ r, g, b }) {
  const ch = (v) => {
    const c = v / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * ch(r) + 0.7152 * ch(g) + 0.0722 * ch(b);
}

export function contrast(fg, bg) {
  const a = luminance(fg);
  const b = luminance(bg);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

export function sameColor(a, b, tol = 2) {
  if (!a || !b) return false;
  return Math.abs(a.r - b.r) <= tol && Math.abs(a.g - b.g) <= tol && Math.abs(a.b - b.b) <= tol && Math.abs(a.a - b.a) <= 0.02;
}

export function colorText(c) {
  if (!c) return '?';
  const h = (v) => Math.round(v).toString(16).padStart(2, '0');
  return `#${h(c.r)}${h(c.g)}${h(c.b)}${c.a < 1 ? h(c.a * 255) : ''}`;
}
