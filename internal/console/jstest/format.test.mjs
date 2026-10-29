process.env.TZ = 'UTC';

import test from 'node:test';
import assert from 'node:assert/strict';
import {
  capitalized,
  punctuated,
  sentence,
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
  codecMeaning,
  codecLegend,
  legacyCatalogError,
  formatShares,
  isHiRes,
  jobTitle,
  jobSubtitle,
  isUnresolved,
  inputLabel,
  unresolvedHint,
  errorSummary,
  shortId,
  displayId,
  safeHttpUrl,
  contrastRatio,
  artTextColor,
  artPalette,
  paletteStyle,
  finishedLine,
  pageSummary,
  releaseYear,
  jobFacts,
  systemFacts,
  attrPairs,
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

test('shortId and safeHttpUrl', () => {
  assert.equal(shortId('job_0123456789abcdef0123456789abcdef'), '01234567');
  assert.equal(displayId('job_0123456789abcdef0123456789abcdef'), '01234567');
  assert.equal(displayId('job_doesnotexist'), 'job_doesnotexist', 'an id that cannot be real is shown whole');
  assert.equal(displayId(undefined), '');
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
    artwork_text_color3: '000000',
    motion_artwork_bg_color: '05104b',
    motion_artwork_text_color1: 'f5f5f5',
    motion_artwork_text_color2: 'e0e0e0',
    motion_artwork_text_color3: 'd0d0d0',
  };
  assert.deepEqual(artPalette(job), { bg: '#598090', text1: '#0a0a0a', text2: 'var(--on-art-dark)', text3: '#000000' });
  assert.deepEqual(artPalette(job, true), { bg: '#05104b', text1: '#f5f5f5', text2: '#e0e0e0', text3: '#d0d0d0' });
  assert.equal(artPalette({}), null);
  assert.equal(artPalette({ artwork_bg_color: 'red' }), null);
  assert.deepEqual(paletteStyle({ bg: '#000000', text1: 'a', text2: 'b', text3: 'c' }), {
    '--art-bg': '#000000',
    '--art-t1': 'a',
    '--art-t2': 'b',
    '--art-t3': 'c',
  });
  assert.equal(paletteStyle(null), undefined);
});

test('finishedLine shows the finish date and the duration when both stamps exist', () => {
  assert.equal(finishedLine({ started_at: '2026-10-28T12:00:00Z', finished_at: '2026-10-28T12:03:12Z' }).replace(/Oct \d+/, 'Oct D'), 'Downloaded Oct D · took 3m 12s');
  assert.equal(finishedLine({ finished_at: '2026-10-28T12:03:12Z' }).startsWith('Downloaded '), true);
  assert.equal(finishedLine({ started_at: '2026-10-28T12:00:00Z' }), '');
});

test('pageSummary reads the feed counts per page', () => {
  const counts = { total: 16, running: 2, queued: 0, failed: 1, completed: 13, cancelled: 0 };
  const now = new Date('2026-10-29T00:57:00Z');
  assert.equal(pageSummary('overview', counts, now), 'Oct 29 · 16 jobs');
  assert.equal(pageSummary('overview', { ...counts, total: 1 }, now), 'Oct 29 · 1 job');
  assert.equal(pageSummary('overview', null, now), 'Oct 29');
  assert.equal(pageSummary('queue', counts, now), '2 running · 0 queued · 1 failed');
  assert.equal(pageSummary('queue', null, now), '');
  assert.equal(pageSummary('showcase', counts, now), '13 completed');
  assert.equal(pageSummary('notfound', counts, now), '');
  for (const page of ['new', 'settings']) assert.ok(pageSummary(page, null, now), `${page} has a summary before the feed loads`);
});

test('jobFacts gives kicker and meta parts, empty when unknown', () => {
  const job = {
    type: 'album',
    genre: 'Pop',
    release_date: '2022-10-21',
    total_items: 20,
    started_at: '2026-10-28T16:57:07Z',
    finished_at: '2026-10-28T16:58:19Z',
  };
  assert.deepEqual(jobFacts(job), {
    type: 'Album',
    genre: 'Pop',
    year: '2022',
    tracks: '20 tracks',
    started: '16:57:07',
    finished: 'Oct 28 16:58',
    took: '1m 12s',
  });
  assert.equal(jobFacts({ total_items: 1 }).tracks, '1 track');
  assert.deepEqual(jobFacts(null), { type: '', genre: '', year: '', tracks: '', started: '', finished: '', took: '' });
  assert.equal(releaseYear({ release_date: 'soon' }), '');
});

test('systemFacts picks workers, queue, downloads disk and uptime', () => {
  const sys = {
    uptime_seconds: 306,
    jobs: { workers: 3, active: 2, queue_depth: 0, queue_capacity: 256 },
    storage: [
      { name: 'temp_dir', free_bytes: 1, total_bytes: 2 },
      { name: 'downloads_dir', free_bytes: 23051792384, total_bytes: 494384795648 },
    ],
  };
  assert.deepEqual(systemFacts(sys), [
    ['workers', 'Workers', '2 of 3 busy'],
    ['queue', 'Queue', '0 of 256'],
    ['storage', 'Storage', '23.1 GB free of 494 GB'],
    ['uptime', 'Uptime', '5m 6s'],
  ]);
  assert.deepEqual(systemFacts({}), []);
  assert.deepEqual(systemFacts(null), []);
});

test('attrPairs sorts keys and leaves out the omitted ones', () => {
  assert.deepEqual(attrPairs({ job_type: 'album', component: 'jobs', n: 3, ok: true }, ['component']), ['job_type=album', 'n=3', 'ok=true']);
  assert.deepEqual(attrPairs(null), []);
});

test('formatShares turns the stats codec mix into shares and folds the tail', () => {
  const two = formatShares([
    { codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: 30 },
    { codec: 'alac', bit_depth: 16, sample_rate: 44100, tracks: 10 },
  ]);
  assert.deepEqual(two.map((f) => [f.key, f.label, f.tracks, f.share]), [
    ['alac-24-96000', 'ALAC 24/96', 30, 0.75],
    ['alac-16-44100', 'ALAC 16/44.1', 10, 0.25],
  ]);
  const many = formatShares(
    [
      { codec: 'alac', bit_depth: 24, sample_rate: 96000, tracks: 4 },
      { codec: 'alac', bit_depth: 24, sample_rate: 48000, tracks: 3 },
      { codec: 'aac', bit_depth: 0, sample_rate: 0, tracks: 2 },
      { codec: 'ec3', bit_depth: 0, sample_rate: 0, tracks: 1 },
    ],
    3,
  );
  assert.deepEqual(many.map((f) => [f.key, f.tracks]), [['alac-24-96000', 4], ['alac-24-48000', 3], ['other', 3]]);
  assert.deepEqual(formatShares([]), []);
  assert.deepEqual(formatShares(undefined), []);
  assert.deepEqual(formatShares([{ codec: 'alac', tracks: 0 }]), []);
});

test('server messages read as row text or as a sentence', () => {
  assert.equal(capitalized('station "X" is a live radio stream'), 'Station "X" is a live radio stream');
  assert.equal(capitalized('  '), '');
  assert.equal(sentence('database is locked'), 'Database is locked.');
  assert.equal(sentence('Already a sentence!'), 'Already a sentence!');
  assert.equal(sentence(undefined), '');
  assert.equal(punctuated('download.cover_size must be {width}x{height}'), 'download.cover_size must be {width}x{height}.');
});

test('an unresolved job is shown by its link, not by a guessed title', () => {
  assert.equal(isUnresolved({ input: 'https://music.apple.com/us/album/x/1' }), true);
  assert.equal(isUnresolved({ title: 'Blonde' }), false);
  assert.equal(isUnresolved(null), false);
  assert.equal(inputLabel('https://music.apple.com/us/album/after-hours/1499378108?i=3&l=en'), 'music.apple.com/us/album/after-hours/1499378108');
  assert.equal(inputLabel('https://music.apple.com/us/album/x/'), 'music.apple.com/us/album/x');
  assert.equal(inputLabel('not a url'), 'not a url');
  assert.equal(unresolvedHint({ status: 'queued' }), 'Not resolved yet');
  assert.equal(unresolvedHint({ status: 'failed' }), "Couldn't be resolved");
  assert.equal(unresolvedHint({ status: 'cancelled' }), 'Cancelled before it resolved');
});

test('errorSummary keeps the first line and drops an embedded JSON body', () => {
  assert.equal(errorSummary('Apple Music has no album 1 in storefront us (404)'), 'Apple Music has no album 1 in storefront us (404)');
  assert.equal(errorSummary('Apple Music has nothing at this link.'), 'Apple Music has nothing at this link.');
  assert.equal(errorSummary('first line\nsecond line'), 'first line');
  assert.equal(errorSummary('{"error":"x"}'), '{"error":"x"}');
  assert.equal(errorSummary(''), '');
  assert.equal(errorSummary(undefined), '');
  const long = errorSummary('x'.repeat(300));
  assert.equal(long.length, 140);
  assert.ok(long.endsWith('…'));
});

test('legacyCatalogError puts the stored catalog failures into plain words', () => {
  const nf = 'catalog request failed: 404 Not Found: {"errors":[{"id":"Q","title":"Resource Not Found"}]}';
  assert.equal(legacyCatalogError(nf), 'Apple Music has nothing at this link (404)');
  assert.equal(legacyCatalogError('read Apple Music catalog: catalog request failed: 404 Not Found'), 'Apple Music has nothing at this link (404)');
  assert.equal(legacyCatalogError('catalog request failed: 403 Forbidden'), 'Apple Music refused access (403)');
  assert.equal(legacyCatalogError('catalog request failed: 401 Unauthorized'), 'Apple Music refused access (401)');
  assert.equal(legacyCatalogError('catalog request failed: 429 Too Many Requests'), 'Apple Music is limiting requests, try again later (429)');
  assert.equal(legacyCatalogError('catalog request failed: 503 Service Unavailable'), 'Apple Music had a server problem (503)');
  assert.equal(legacyCatalogError('catalog request failed: 400 Bad Request'), 'Apple Music rejected the request (400)');
  // Anything else, including the server's current wording, is not touched.
  assert.equal(legacyCatalogError('Apple Music has no album 1 in storefront us (404)'), '');
  assert.equal(legacyCatalogError('disk full'), '');
  assert.equal(legacyCatalogError(undefined), '');
});

test('errorSummary words a stored legacy catalog error and the caller keeps the full text', () => {
  const raw = 'catalog request failed: 404 Not Found: {"errors":[{"id":"Q","title":"Resource Not Found"}]}';
  assert.equal(errorSummary(raw), 'Apple Music has nothing at this link (404)');
  assert.equal(errorSummary('catalog request failed: 502 Bad Gateway\nretry later'), 'Apple Music had a server problem (502)');
  assert.notEqual(errorSummary(raw), raw.trim(), 'the drawer offers "Full error" when the summary differs');
});

test('codecMeaning spells a chip out in the words of the Qualities sheet', () => {
  assert.equal(codecMeaning({ codec: 'alac', bit_depth: 24, sample_rate: 44100 }), 'Lossless 24-bit / 44.1\u00a0kHz');
  assert.equal(codecMeaning({ codec: 'alac', bit_depth: 16, sample_rate: 44100 }), 'Lossless 16-bit / 44.1\u00a0kHz');
  assert.equal(codecMeaning({ codec: 'aac', bit_depth: 16, sample_rate: 48000 }), '16-bit / 48\u00a0kHz');
  assert.equal(codecMeaning({ codec: 'aac' }), '');
  assert.equal(codecMeaning(null), '');
});

test('codecLegend lists each distinct numbered chip once, in first-seen order', () => {
  const alac = (rate) => ({ codec: 'alac', bit_depth: 24, sample_rate: rate });
  assert.deepEqual(codecLegend([alac(44100), { codec: 'aac' }, alac(44100), alac(48000)]), [
    { label: 'ALAC 24/44.1', text: 'Lossless 24-bit / 44.1\u00a0kHz' },
    { label: 'ALAC 24/48', text: 'Lossless 24-bit / 48\u00a0kHz' },
  ]);
  assert.deepEqual(codecLegend([{ codec: 'aac' }]), []);
  assert.deepEqual(codecLegend(undefined), []);
});
