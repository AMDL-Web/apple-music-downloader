import test from 'node:test';
import assert from 'node:assert/strict';
import { parseColor, contrast, over } from './color.mjs';
import { gaps, blankBands, shapeProblems, modeOf, clsWindows, nearestStep, edgeShift } from './geometry.mjs';
import { noiseReason, proseScore, hiddenDistinctions } from './text.mjs';
import { analyzeFrames } from './flicker.mjs';

test('parseColor reads the forms getComputedStyle returns', () => {
  assert.deepEqual(parseColor('rgb(10, 20, 30)'), { r: 10, g: 20, b: 30, a: 1 });
  assert.deepEqual(parseColor('rgba(10, 20, 30, 0.5)'), { r: 10, g: 20, b: 30, a: 0.5 });
  assert.deepEqual(parseColor('color(srgb 1 0 0 / 0.25)'), { r: 255, g: 0, b: 0, a: 0.25 });
  assert.deepEqual(parseColor('transparent'), { r: 0, g: 0, b: 0, a: 0 });
  // oklab(1 0 0) is white, oklab(0 0 0) black.
  assert.deepEqual(parseColor('oklab(1 0 0)'), { r: 255, g: 255, b: 255, a: 1 });
  assert.deepEqual(parseColor('oklab(0 0 0)'), { r: 0, g: 0, b: 0, a: 1 });
  assert.equal(parseColor('hotpink'), null);
});

test('contrast follows WCAG 2 relative luminance', () => {
  const white = { r: 255, g: 255, b: 255, a: 1 };
  assert.equal(contrast({ r: 0, g: 0, b: 0, a: 1 }, white).toFixed(1), '21.0');
  // #767676 is the classic lightest grey that passes 4.5:1 on white.
  assert.ok(contrast({ r: 0x76, g: 0x76, b: 0x76, a: 1 }, white) >= 4.5);
  assert.ok(contrast({ r: 0x77, g: 0x77, b: 0x77, a: 1 }, white) < 4.5);
});

test('over composites a translucent layer', () => {
  const half = over({ r: 0, g: 0, b: 0, a: 0.5 }, { r: 255, g: 255, b: 255, a: 1 });
  assert.equal(Math.round(half.r), 128);
  assert.equal(half.a, 1);
});

test('gaps and blankBands find the unexplained void', () => {
  assert.deepEqual(gaps([[0, 10], [5, 20], [60, 100]], 0, 100)[0], { start: 20, end: 60, size: 40 });
  // A 200px card whose content ends at 100: a 100px band at the bottom.
  const bands = blankBands([[0, 40], [48, 100]], 0, 200);
  assert.equal(bands.length, 1);
  assert.equal(bands[0].edge, 'bottom');
  assert.equal(bands[0].size, 100);
  // 40px of slack in a 300px box is under both thresholds.
  assert.deepEqual(blankBands([[0, 260]], 0, 300), []);
});

test('shapeProblems: a square highlight flush in a rounded card corner', () => {
  const card = { left: 0, top: 0, right: 200, bottom: 100 };
  const flush = { left: 0, top: 8, right: 200, bottom: 92 };
  const p = shapeProblems(flush, [0, 0, 0, 0], card, [22, 22, 22, 22]);
  assert.ok(p.some((x) => x.kind === 'square-in-round'));
  assert.ok(p.some((x) => x.kind === 'uneven-inset'));
  // The same highlight in a card that clips its children is fine at the corners.
  assert.ok(!shapeProblems(flush, [0, 0, 0, 0], card, [22, 22, 22, 22], { clips: true }).some((x) => x.kind === 'square-in-round'));
  // A row in the middle of a list touches neither corner.
  assert.deepEqual(shapeProblems({ left: 0, top: 40, right: 200, bottom: 60 }, [0, 0, 0, 0], { left: 0, top: 0, right: 200, bottom: 300 }, [22, 22, 22, 22]), []);
  // Concentric: card 22, inset 8, child 14 is right.
  assert.deepEqual(shapeProblems({ left: 8, top: 8, right: 192, bottom: 92 }, [14, 14, 14, 14], card, [22, 22, 22, 22]), []);
});

test('shapeProblems treats a huge radius as a pill', () => {
  // A 32px pill button 16px from a 22px card corner: its corners draw at 16, no complaint.
  const card = { left: 0, top: 0, right: 300, bottom: 100 };
  assert.deepEqual(shapeProblems({ left: 200, top: 16, right: 284, bottom: 48 }, [999, 999, 999, 999], card, [22, 22, 22, 22]), []);
});

test('modeOf, nearestStep and clsWindows', () => {
  assert.deepEqual(modeOf([16, 16, 16.5, 24], 1).outliers, [24]);
  assert.deepEqual(nearestStep(13, [0, 4, 8, 12, 16]), { step: 12, off: 1 });
  const w = clsWindows([{ t: 0, value: 0.1 }, { t: 500, value: 0.1 }, { t: 3000, value: 0.05 }]);
  assert.equal(w.total.toFixed(2), '0.25');
  assert.equal(w.maxWindow.toFixed(2), '0.20');
});

test('noiseReason flags zero counts and leaked values, not data', () => {
  assert.equal(noiseReason('0 cancelled'), 'a zero count');
  assert.equal(noiseReason('—'), 'a dash placeholder');
  assert.equal(noiseReason('undefined tracks'), 'a leaked programming value');
  assert.equal(noiseReason('12 tracks'), null);
  assert.equal(noiseReason('ALAC 24/96'), null);
});

test('proseScore separates explanations from labels', () => {
  assert.ok(proseScore('Paste one or more Apple Music links here and press Download to start.') >= 2);
  assert.ok(proseScore('This card shows the jobs that finished in the last 24 hours.') >= 2);
  assert.equal(proseScore('Open in Apple Music'), 0);
  assert.ok(proseScore('ALAC 24/48 · 22 tracks · 459 MB · Oct 30 16:00') < 2);
});

test('hiddenDistinctions: rows cut to the same prefix', () => {
  const d = hiddenDistinctions([
    { text: 'Symphony No. 9: I. Allegro', shown: 12 },
    { text: 'Symphony No. 9: II. Molto vivace', shown: 12 },
    { text: 'Blonde', shown: 6 },
  ]);
  assert.equal(d.length, 1);
  assert.equal(d[0].texts.length, 2);
});

const frame = (t, o = {}) => ({ t, text: 400, skel: 0, h: 800, focus: 'body', scroll: 0, theme: '', bg: 0.02, bareImgs: 0, added: 0, removed: 0, ...o });

test('analyzeFrames: blank frames, loader flashes, focus loss, theme flashes', () => {
  const blank = analyzeFrames([frame(0), frame(16, { text: 30 }), frame(33, { text: 30 }), frame(50)]);
  assert.equal(blank[0].kind, 'blank');
  assert.equal(blank[0].ms, 34);
  const flash = analyzeFrames([frame(0), frame(16, { skel: 8 }), frame(60, { skel: 0 })], { skeletonMinMs: 300 });
  assert.ok(flash.some((f) => f.kind === 'skeleton-flash'));
  const slow = analyzeFrames([frame(0), frame(16, { skel: 8 }), frame(700, { skel: 0 })], { skeletonMinMs: 300 });
  assert.ok(!slow.some((f) => f.kind === 'skeleton-flash'));
  const lost = analyzeFrames([frame(0, { focus: 'a.job-row' }), frame(16, { focus: 'body' })]);
  assert.ok(lost.some((f) => f.kind === 'focus-lost'));
  const handed = analyzeFrames([frame(0, { focus: 'textarea' }), frame(16, { focus: 'body' }), frame(33, { focus: 'h1.page-title' })]);
  assert.ok(!handed.some((f) => f.kind === 'focus-lost'));
  const theme = analyzeFrames([frame(0, { bg: 1 }), frame(16, { bg: 0.02 })]);
  assert.ok(theme.some((f) => f.kind === 'theme-flash'));
  assert.ok(!analyzeFrames([frame(0, { bg: 1 }), frame(16, { bg: 0.02 })], { expectThemeChange: true }).some((f) => f.kind === 'theme-flash'));
  const early = analyzeFrames([frame(0, { scroll: 600 }), frame(16, { scroll: 0 }), frame(40, { added: 30, removed: 30 })], { expectScroll: 0 });
  assert.ok(early.some((f) => f.kind === 'scroll-before-commit'));
});

test('edgeShift counts a resize with one edge held as no move', () => {
  assert.equal(edgeShift(8.4, 0), 0);
  assert.equal(edgeShift(0, -44), 0);
  assert.equal(edgeShift(44, 44), 44);
  assert.equal(edgeShift(-9, -18), -9);
});

test('blankBands leaves centred content alone', () => {
  // A 214 px empty state with its 102 px of content in the middle.
  assert.deepEqual(blankBands([[56, 158]], 0, 214), []);
  // The same content pinned to the top is a void below it.
  assert.equal(blankBands([[0, 102]], 0, 214)[0].edge, 'bottom');
});
