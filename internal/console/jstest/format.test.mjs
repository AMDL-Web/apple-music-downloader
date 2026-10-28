process.env.TZ = 'UTC';

import test from 'node:test';
import assert from 'node:assert/strict';
import {
  artworkUrl,
  artworkSrcset,
  isArtworkTemplate,
  bytes,
  count,
  trackLength,
  elapsed,
  relTime,
  absTime,
  clockTime,
  dayLabel,
  kHz,
  codecLabel,
  isHiRes,
  jobTitle,
  jobSubtitle,
  shortId,
  maskAccount,
  safeHttpUrl,
  contrastRatio,
  artTextColor,
  artPalette,
  paletteStyle,
} from '../static/js/lib/format.js';

const TEMPLATE = 'https://is1-ssl.mzstatic.com/image/thumb/Music/ab/cd/source/{w}x{h}bb.{f}';
const PRESIGNED = 'https://example-bucket.s3.amazonaws.com/cover.jpg?X-Amz-Signature=abc';

test('artworkUrl fills {w}, {h} and {f}', () => {
  assert.equal(artworkUrl(TEMPLATE, 300), 'https://is1-ssl.mzstatic.com/image/thumb/Music/ab/cd/source/300x300bb.jpg');
  assert.equal(artworkUrl(TEMPLATE, 600, 'webp'), 'https://is1-ssl.mzstatic.com/image/thumb/Music/ab/cd/source/600x600bb.webp');
});

test('artworkUrl leaves a pre-signed URL without placeholders untouched', () => {
  assert.equal(isArtworkTemplate(PRESIGNED), false);
  assert.equal(artworkUrl(PRESIGNED, 300), PRESIGNED);
  assert.equal(artworkSrcset(PRESIGNED, [300, 600]), '');
  assert.equal(artworkUrl('', 300), '');
  assert.equal(artworkUrl(undefined, 300), '');
});

test('artworkSrcset lists width descriptors', () => {
  assert.equal(
    artworkSrcset(TEMPLATE, [300, 600]),
    'https://is1-ssl.mzstatic.com/image/thumb/Music/ab/cd/source/300x300bb.jpg 300w, https://is1-ssl.mzstatic.com/image/thumb/Music/ab/cd/source/600x600bb.jpg 600w',
  );
});

test('bytes uses decimal units', () => {
  assert.equal(bytes(0), '0 B');
  assert.equal(bytes(999), '999 B');
  assert.equal(bytes(1500), '1.5 kB');
  assert.equal(bytes(987654321), '988 MB');
  assert.equal(bytes(9876543210), '9.9 GB');
  assert.equal(bytes(-1), '—');
  assert.equal(bytes(undefined), '—');
});

test('count adds thousands separators', () => {
  assert.equal(count(1284), '1,284');
  assert.equal(count(0), '0');
  assert.equal(count(null), '—');
});

test('trackLength and elapsed', () => {
  assert.equal(trackLength(225000), '3:45');
  assert.equal(trackLength(3723000), '1:02:03');
  assert.equal(trackLength(0), '');
  assert.equal(elapsed(45000), '45s');
  assert.equal(elapsed(192000), '3m 12s');
  assert.equal(elapsed(7500000), '2h 5m');
  assert.equal(elapsed(90000000), '1d 1h');
});

test('relTime against a fixed now', () => {
  const now = Date.parse('2026-10-28T12:00:00Z');
  assert.equal(relTime('2026-10-28T11:59:55Z', now), 'just now');
  assert.equal(relTime('2026-10-28T11:59:30Z', now), '30s ago');
  assert.equal(relTime('2026-10-28T11:55:00Z', now), '5m ago');
  assert.equal(relTime('2026-10-28T09:00:00Z', now), '3h ago');
  assert.equal(relTime('2026-10-26T12:00:00Z', now), '2d ago');
  assert.equal(relTime('2026-08-01T12:00:00Z', now), 'Aug 1');
  assert.equal(relTime('not a date', now), '—');
});

test('absTime, clockTime and dayLabel', () => {
  assert.equal(absTime('2026-10-28T12:04:05.123Z'), '2026-10-28 12:04:05');
  assert.equal(clockTime('2026-10-28T12:04:05.123Z'), '12:04:05.123');
  assert.equal(clockTime('2026-10-28T12:04:05.123Z', false), '12:04:05');
  assert.equal(dayLabel('2026-10-28'), 'Oct 28');
  assert.equal(dayLabel('2026-01-03'), 'Jan 3');
});

test('codec labels', () => {
  assert.equal(kHz(96000), '96');
  assert.equal(kHz(44100), '44.1');
  assert.equal(kHz(192000), '192');
  assert.equal(codecLabel({ codec: 'alac', bit_depth: 24, sample_rate: 96000 }), 'ALAC 24/96');
  assert.equal(codecLabel({ codec: 'alac', bit_depth: 16, sample_rate: 44100 }), 'ALAC 16/44.1');
  assert.equal(codecLabel({ codec: 'aac' }), 'AAC');
  assert.equal(codecLabel({ codec: 'ec3' }), 'Dolby Atmos');
  assert.equal(codecLabel({}), '');
  assert.equal(isHiRes({ bit_depth: 24, sample_rate: 96000 }), true);
  assert.equal(isHiRes({ bit_depth: 24, sample_rate: 48000 }), false);
  assert.equal(isHiRes({ bit_depth: 16, sample_rate: 44100 }), false);
});

test('jobTitle falls back to the input URL slug before the input resolves', () => {
  assert.equal(jobTitle({ title: 'Midnights', input: 'https://music.apple.com/us/album/x/1' }), 'Midnights');
  assert.equal(jobTitle({ input: 'https://music.apple.com/us/album/1989-taylors-version/1708308989' }), '1989 taylors version');
  assert.equal(jobTitle({ input: 'not a url' }), 'not a url');
  assert.equal(jobSubtitle({ curator_name: 'Apple Music Pop' }), 'Apple Music Pop');
  assert.equal(jobSubtitle({ artist_name: 'Taylor Swift', curator_name: 'x' }), 'Taylor Swift');
});

test('shortId, maskAccount and safeHttpUrl', () => {
  assert.equal(shortId('job_0123456789abcdef0123456789abcdef'), '01234567');
  assert.equal(maskAccount('jane@example.com'), 'j•••@example.com');
  assert.equal(maskAccount('operator'), 'o•••r');
  assert.equal(safeHttpUrl('https://music.apple.com/us/album/1'), 'https://music.apple.com/us/album/1');
  assert.equal(safeHttpUrl('javascript:alert(1)'), '');
  assert.equal(safeHttpUrl(undefined), '');
});

test('contrastRatio matches published WCAG values', () => {
  assert.equal(Number(contrastRatio('ffffff', '000000').toFixed(2)), 21);
  // #767676 is the lightest gray that passes 4.5:1 on white; #777777 fails.
  assert.equal(Number(contrastRatio('767676', 'ffffff').toFixed(2)), 4.54);
  assert.equal(Number(contrastRatio('777777', 'ffffff').toFixed(2)), 4.48);
  assert.equal(contrastRatio('xyz', 'ffffff'), null);
});

test('artTextColor keeps Apple colors that pass 4.5:1 and falls back otherwise', () => {
  assert.equal(artTextColor('1a1a1a', 'f0f0f0'), '#f0f0f0');
  assert.equal(artTextColor('000000', '333333'), 'var(--on-art-light)');
  assert.equal(artTextColor('ffffff', '777777'), 'var(--on-art-dark)');
  assert.equal(artTextColor('ffffff', undefined), 'var(--on-art-dark)');
});

test('artPalette uses the motion asset palette only when asked', () => {
  const job = {
    artwork_bg_color: '598090',
    artwork_text_color1: '0a0a0a',
    artwork_text_color2: '1a1a1a', // 4.08:1 on 598090, below 4.5
    motion_artwork_bg_color: '05104b',
    motion_artwork_text_color1: 'f5f5f5',
    motion_artwork_text_color2: 'e0e0e0',
  };
  assert.deepEqual(artPalette(job), { bg: '#598090', text1: '#0a0a0a', text2: 'var(--on-art-dark)' });
  assert.deepEqual(artPalette(job, true), { bg: '#05104b', text1: '#f5f5f5', text2: '#e0e0e0' });
  assert.equal(artPalette({}), null);
  assert.equal(artPalette({ artwork_bg_color: 'red' }), null);
  assert.deepEqual(paletteStyle({ bg: '#000000', text1: 'a', text2: 'b' }), { '--art-bg': '#000000', '--art-text1': 'a', '--art-text2': 'b' });
  assert.equal(paletteStyle(null), undefined);
});
