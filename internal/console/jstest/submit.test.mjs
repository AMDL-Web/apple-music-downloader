import test from 'node:test';
import assert from 'node:assert/strict';
import { scanLinks, removeLink } from '../static/js/lib/links.js';
import {
  linkIssue,
  submittable,
  chunk,
  resultView,
  pairResults,
  keptAfterSubmit,
  resultRows,
  applyFeedToResults,
  submitSummary,
  classifySubmitError,
  bookmarklet,
  needsMediaUserToken,
  requestOverrides,
  storedResults,
  parseStoredResults,
} from '../static/js/lib/submit.js';

const IGOR = 'https://music.apple.com/us/album/igor/1463409338';
const FOLKLORE = 'https://music.apple.com/us/album/folklore/1524801260';
const STATION = 'https://music.apple.com/us/station/taylor-swift-similar-artists/ra.159260351';
const PRIVATE = 'https://music.apple.com/us/playlist/mine/pl.u-abc123';
const VIDEO = 'https://music.apple.com/us/music-video/earfquake/1468318460';

test('scanLinks keeps other URLs so the page can flag them', () => {
  const text = `Two albums: ${IGOR}?l=en, ${FOLKLORE}.\nAnd https://open.spotify.com/album/4yP0hdKOZPNshxUOjY0cZj and ${IGOR}`;
  const entries = scanLinks(text);
  assert.deepEqual(entries.map((e) => e.url), [IGOR, FOLKLORE, 'https://open.spotify.com/album/4yP0hdKOZPNshxUOjY0cZj']);
  assert.equal(entries[0].raw, `${IGOR}?l=en`);
  assert.equal(entries[2].link, null);
  assert.deepEqual(scanLinks('no links here'), []);
  assert.deepEqual(scanLinks(null), []);
});

test('removeLink drops each occurrence and lines left empty', () => {
  const text = `${IGOR}?l=en\n<${FOLKLORE}>\nmy notes ${IGOR} here\n\nhttps://example.com/x`;
  assert.equal(removeLink(text, IGOR), `<${FOLKLORE}>\nmy notes  here\n\nhttps://example.com/x`);
  assert.equal(removeLink(text, FOLKLORE), `${IGOR}?l=en\nmy notes ${IGOR} here\n\nhttps://example.com/x`);
  assert.equal(removeLink(text, 'https://example.com/x'), `${IGOR}?l=en\n<${FOLKLORE}>\nmy notes ${IGOR} here\n`);
  assert.equal(removeLink(text, 'https://nowhere.example/'), text);
});

const entry = (url) => scanLinks(url)[0];

test('linkIssue flags what cannot be sent and what needs attention', () => {
  assert.equal(linkIssue(entry(IGOR)), null);
  assert.deepEqual(linkIssue(entry('https://open.spotify.com/album/x')), { code: 'not_apple', role: 'danger', blocking: true, message: 'Not an Apple Music link' });
  assert.equal(linkIssue(entry(VIDEO)).code, 'music_video');
  assert.equal(linkIssue(entry('https://music.apple.com/us/curator/apple-music/976439548')).code, 'unsupported');
  assert.equal(linkIssue(entry(STATION)).code, 'needs_token');
  assert.equal(linkIssue(entry(STATION)).blocking, false);
  assert.equal(linkIssue(entry(STATION), null, { hasToken: true }), null);
  assert.equal(linkIssue(entry(PRIVATE)), null, 'a private playlist downloads without a token; the token only finds its cover');
  const live = { state: 'error', error: { status: 400, code: 'invalid_request', message: 'station "Apple Music 1" is a live radio stream' } };
  assert.deepEqual(linkIssue(entry(STATION), live, { hasToken: true }), {
    code: 'refused',
    role: 'danger',
    blocking: true,
    message: 'Station "Apple Music 1" is a live radio stream',
  });
  const upstream = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'wrapper: dial failed: {"code":14}' } };
  assert.deepEqual(linkIssue(entry(IGOR), upstream), {
    code: 'preview_failed',
    role: 'warning',
    blocking: false,
    message: 'No preview: wrapper: dial failed',
    title: upstream.error.message,
  });
  assert.equal(linkIssue(entry(IGOR), { state: 'error', error: { code: 'aborted' } }), null, 'a cancelled lookup is no issue');
  const missing = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'read Apple Music catalog: catalog request failed: 404 Not Found: {}' } };
  assert.deepEqual(
    [linkIssue(entry(IGOR), missing).code, linkIssue(entry(IGOR), missing).blocking],
    ['not_found', true],
    "Apple's 404 keeps the link out of the submit",
  );
  // The catalog read of a playlist never carries the token, so Apple's 404
  // for a private playlist means it is not there, with or without one.
  for (const hasToken of [false, true]) {
    const issue = linkIssue(entry(PRIVATE), missing, { hasToken });
    assert.deepEqual([issue.code, issue.blocking], ['not_found', true]);
  }
  assert.equal(linkIssue(entry(PRIVATE), upstream).message, 'No preview: wrapper: dial failed');
  const says = { state: 'ready', data: { needs_media_user_token: true } };
  assert.equal(linkIssue(entry(IGOR), says).code, 'needs_token', 'the preview can ask for the token');
  assert.equal(linkIssue(entry(IGOR), says, { hasToken: true }), null);
});

test('needsMediaUserToken: stations, private playlists (their cover) and previews that say so', () => {
  const none = () => null;
  assert.equal(needsMediaUserToken([], none), false);
  assert.equal(needsMediaUserToken([IGOR, FOLKLORE, 'https://example.com/x'], none), false);
  assert.equal(needsMediaUserToken([IGOR, STATION], none), true);
  assert.equal(needsMediaUserToken([PRIVATE], none), true);
  assert.equal(needsMediaUserToken(['https://music.apple.com/us/playlist/todays-hits/pl.f4d106fed2bd41149aaacabb233eb5eb'], none), false, 'a catalog playlist needs none');
  const says = (url) => (url === FOLKLORE ? { state: 'ready', data: { needs_media_user_token: true } } : { state: 'idle' });
  assert.equal(needsMediaUserToken([IGOR, FOLKLORE], says), true);
  assert.equal(needsMediaUserToken([IGOR, FOLKLORE], (url) => ({ state: 'error', error: { status: 502 } })), false);
});

test('requestOverrides sends the typed token only with links that need it', () => {
  const opts = { lyrics_format: 'ttml' };
  const none = () => null;
  assert.deepEqual(requestOverrides(opts, [IGOR, STATION], ' tok ', none), { lyrics_format: 'ttml', media_user_token: 'tok' });
  assert.deepEqual(requestOverrides(opts, [PRIVATE], 'tok', none), { lyrics_format: 'ttml', media_user_token: 'tok' });
  // The station was removed or sent in another request: the token stays home.
  assert.equal(requestOverrides(opts, [IGOR, FOLKLORE], 'tok', none), opts);
  assert.equal(requestOverrides(opts, [STATION], '   ', none), opts, 'an empty field leaves the server token in charge');
  assert.equal(requestOverrides(opts, [STATION], undefined, none), opts);
  assert.equal(opts.media_user_token, undefined, 'the options object is not changed');
});

test('submittable keeps links without a blocking issue, in order', () => {
  const entries = scanLinks(`${VIDEO} ${IGOR} https://example.com/a ${STATION} ${FOLKLORE}`);
  assert.deepEqual(submittable(entries, (e) => linkIssue(e)), [IGOR, STATION, FOLKLORE]);
  assert.deepEqual(chunk([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]]);
  assert.deepEqual(chunk([], 2), []);
});

const job = (id, status, updated) => ({ id, status, updated_at: updated, input: IGOR, type: 'album' });

// A 202 BatchSubmitResponse with every per-URL outcome the spec lists.
const RESPONSE = {
  accepted: 1,
  rejected: 4,
  results: [
    { url: IGOR, status: 'accepted', job: job('job_1', 'queued', '2026-10-29T10:00:00Z') },
    { url: FOLKLORE, status: 'duplicate_active', existing_job_id: 'job_old', error: { code: 'duplicate_active', message: 'already queued or running' } },
    { url: STATION, status: 'queue_full', error: { code: 'queue_full', message: 'job queue is full' } },
    { url: 'https://music.apple.com/jp/album/x/1', status: 'invalid', error: { code: 'unsupported_storefront', message: 'storefront jp is not served', details: { storefront: 'jp', supported_storefronts: ['us', 'ca'] } } },
    { url: PRIVATE, status: 'error', error: { code: 'store_error', message: 'database is locked' } },
  ],
};
const SENT = [IGOR, FOLKLORE, STATION, 'https://music.apple.com/jp/album/x/1', PRIVATE];

test('resultView words every status and code', () => {
  const v = RESPONSE.results.map(resultView);
  assert.deepEqual(v.map((x) => [x.status, x.role, x.label]), [
    ['accepted', 'success', 'Queued'],
    ['duplicate_active', 'info', 'Already downloading'],
    ['queue_full', 'warning', 'Queue full'],
    ['invalid', 'danger', 'Storefront not served'],
    ['error', 'danger', 'Server error'],
  ]);
  assert.equal(v[1].existingJobId, 'job_old');
  assert.equal(v[1].message, '', 'the label already says it');
  assert.equal(v[1].retry, false);
  assert.equal(v[2].retry, true, 'queue_full can be retried');
  assert.equal(v[2].detail, '', 'the server words add nothing here');
  assert.deepEqual(v[3].storefronts, ['us', 'ca']);
  assert.equal(v[3].message, 'The decryptor does not serve this storefront');
  assert.equal(v[4].retry, true);
  assert.equal(v[4].detail, 'Database is locked.', 'a store error shows what the server said');
  const notReady = resultView({ status: 'invalid', error: { code: 'decryptor_unavailable', message: 'x' } });
  assert.deepEqual([notReady.role, notReady.label, notReady.retry], ['warning', 'Decryptor not ready', true]);
  const unknown = resultView({ status: 'invalid', error: { code: 'something_new', message: 'server words' } });
  assert.deepEqual([unknown.label, unknown.message, unknown.retry], ['Not accepted', 'Server words.', false]);
});

test('results pair by position, else by normalized URL; only the unfinished stay', () => {
  const pairs = pairResults(SENT, RESPONSE.results);
  assert.equal(pairs[2].result.status, 'queue_full');
  assert.deepEqual(keptAfterSubmit(pairs), [STATION, 'https://music.apple.com/jp/album/x/1', PRIVATE]);
  // The server normalizes differently and drops one result: match by URL.
  const shuffled = pairResults([FOLKLORE, IGOR, VIDEO], [{ url: `${IGOR}?l=en`, status: 'accepted' }, { url: FOLKLORE, status: 'duplicate_active' }]);
  assert.deepEqual(shuffled.map((p) => p.result && p.result.status), ['duplicate_active', 'accepted', null]);
  assert.deepEqual(keptAfterSubmit(shuffled), [VIDEO]);
});

test('feed updates reach only the jobs the results hold', () => {
  const rows = resultRows(pairResults(SENT, RESPONSE.results));
  assert.equal(rows[0].job.id, 'job_1');
  assert.equal(rows[1].job, null);
  const other = applyFeedToResults(rows, [{ type: 'download_upserted', job: job('job_other', 'running', '2026-10-29T10:00:05Z') }]);
  assert.equal(other, rows, 'a job the page did not submit is ignored');
  const running = applyFeedToResults(rows, [{ type: 'download_upserted', job: job('job_1', 'running', '2026-10-29T10:00:05Z') }]);
  assert.equal(running[0].job.status, 'running');
  const stale = applyFeedToResults(running, [{ type: 'download_upserted', job: job('job_1', 'queued', '2026-10-29T10:00:00Z') }]);
  assert.equal(stale[0].job.status, 'running', 'an older snapshot does not undo a newer one');
  const gone = applyFeedToResults(running, [{ type: 'download_deleted', job_id: 'job_1' }]);
  assert.equal(gone[0].deleted, true);
  assert.equal(submitSummary(rows), '1 queued · 1 already downloading · 3 not accepted');
});

test('classifySubmitError maps each failure of POST downloads', () => {
  const none = classifySubmitError({ status: 422, code: 'no_urls_accepted', message: 'no URL accepted', details: RESPONSE });
  assert.equal(none.kind, 'results');
  assert.equal(none.retryable, false);
  assert.equal(classifySubmitError({ status: 500, code: 'store_error', details: RESPONSE }).retryable, true);
  assert.deepEqual(classifySubmitError({ status: 422, code: 'invalid_overrides', message: 'hooks: unknown entry "x"' }), { kind: 'overrides', message: 'hooks: unknown entry "x"' });
  assert.equal(classifySubmitError({ status: 413, code: 'request_too_large', message: 'body too large' }).kind, 'error');
  assert.match(classifySubmitError({ status: 400, code: 'invalid_request', message: 'urls is empty' }).message, /urls is empty/);
  assert.equal(classifySubmitError({ status: 0, code: 'network_error', message: 'Cannot reach the server.' }).message, 'Cannot reach the server.');
});

test('bookmarklet sends the current page to this console under any prefix', () => {
  assert.equal(
    bookmarklet('http://127.0.0.1:18161/console/#/new?url=x'),
    "javascript:location.href='http://127.0.0.1:18161/console/#/new?url='+encodeURIComponent(location.href)",
  );
  assert.equal(bookmarklet("https://nas.local/amdl/console/it's/"), "javascript:location.href='https://nas.local/amdl/console/it%27s/#/new?url='+encodeURIComponent(location.href)");
});

test('linkIssue blocks a storefront Apple does not have and a link without one', () => {
  const zz = { ...entry('https://music.apple.com/zz/album/igor/1463409338') };
  const bad = {
    state: 'error',
    error: { status: 502, code: 'upstream_error', message: 'read Apple Music catalog: catalog request failed: 400 Bad Request: {"errors":[{"title":"Unknown Storefront"}]}' },
  };
  const issue = linkIssue(zz, bad);
  assert.deepEqual([issue.code, issue.blocking, issue.message], ['bad_storefront', true, 'Apple Music has no storefront “ZZ”']);
  const entries = [entry(IGOR), zz];
  const issueOf = (e) => linkIssue(e, e === zz ? bad : null);
  assert.deepEqual(submittable(entries, issueOf), [IGOR], 'flagged, so never also counted to download');
  const bare = entry('https://music.apple.com/album/igor/1463409338');
  assert.deepEqual([linkIssue(bare).code, linkIssue(bare).blocking], ['no_storefront', true]);
});

test('submitSummary follows the live status of each queued job', () => {
  const rows = resultRows([
    { url: IGOR, result: { url: IGOR, status: 'accepted', job: job('a', 'queued', '2026-10-29T00:00:00Z') } },
    { url: FOLKLORE, result: { url: FOLKLORE, status: 'accepted', job: job('b', 'queued', '2026-10-29T00:00:00Z') } },
  ]);
  assert.equal(submitSummary(rows), '2 queued');
  const later = applyFeedToResults(rows, [
    { type: 'download_upserted', job: job('a', 'running', '2026-10-29T00:00:05Z') },
    { type: 'download_deleted', job_id: 'b' },
  ]);
  assert.equal(submitSummary(later), '1 running · 1 deleted');
  const done = applyFeedToResults(later, [{ type: 'download_upserted', job: job('a', 'completed', '2026-10-29T00:01:00Z') }]);
  assert.equal(submitSummary(done), '1 completed · 1 deleted');
});

test('stored results round-trip without the media-user-token', () => {
  const rows = resultRows([{ url: IGOR, result: { url: IGOR, status: 'accepted', job: job('a', 'queued', '2026-10-29T00:00:00Z') } }]);
  const text = storedResults({ rows, overrides: { max_attempts: 5, media_user_token: 'secret' } });
  assert.ok(!text.includes('secret'));
  assert.deepEqual(parseStoredResults(text), { rows, overrides: { max_attempts: 5 } });
  assert.equal(storedResults({ rows: [], overrides: {} }), '');
  for (const bad of [null, '', '{', '{"rows":[{"url":1}]}', '[]']) assert.equal(parseStoredResults(bad), null);
});
