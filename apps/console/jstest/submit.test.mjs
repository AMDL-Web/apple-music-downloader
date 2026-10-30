import test from 'node:test';
import assert from 'node:assert/strict';
import { scanLinks, removeLink } from '../static/js/lib/links.js';
import { linkIssue, servedRegions, servedNote, submittable, TOKEN_SETTINGS_HASH, downloadLabel, countLine } from '../static/js/lib/submit-check.js';
import {
  chunk,
  resultView,
  pairResults,
  keptAfterSubmit,
  resultRows,
  applyFeedToResults,
  submitSummary,
  classifySubmitError,
  bookmarklet,
  storedResults,
  parseStoredResults,
  seedJob,
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
  assert.deepEqual(linkIssue(entry(STATION)), {
    code: 'needs_token',
    role: 'warning',
    blocking: false,
    message: 'Stations need a media-user-token',
    href: TOKEN_SETTINGS_HASH,
    action: 'Set it in Settings',
  });
  assert.equal(TOKEN_SETTINGS_HASH, '#/settings?section=account&field=catalog.media_user_token', 'the hint opens Settings at the token field');
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
  const missing = { state: 'error', error: { status: 404, code: 'catalog_not_found', message: 'read Apple Music catalog: Apple Music has no album 1463409338 in storefront us (404)' } };
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

test('submittable keeps links without a blocking issue, in order', () => {
  const entries = scanLinks(`${VIDEO} ${IGOR} https://example.com/a ${STATION} ${FOLKLORE}`);
  assert.deepEqual(submittable(entries, (e) => linkIssue(e)), [IGOR, STATION, FOLKLORE]);
  assert.deepEqual(chunk([1, 2, 3, 4, 5], 2), [[1, 2], [3, 4], [5]]);
  assert.deepEqual(chunk([], 2), []);
});

const JP = 'https://music.apple.com/jp/album/1635388716';

test('linkIssue flags a storefront the wrapper does not serve, and only when the regions are known', () => {
  const issue = linkIssue(entry(JP), null, { regions: ['cn'] });
  assert.deepEqual(issue, {
    code: 'storefront_not_served',
    role: 'danger',
    blocking: true,
    message: "Storefront not served. The wrapper's Apple Music account is in another storefront. It serves CN.",
  });
  const view = resultView({ status: 'invalid', error: { code: 'unsupported_storefront', message: 'x', details: { supported_storefronts: ['cn'] } } });
  assert.ok(issue.message.startsWith(`${view.label}. ${view.message}.`), 'reads like the submit result');
  assert.ok(issue.message.endsWith(servedNote(view.storefronts)));
  assert.equal(linkIssue(entry(JP), null, { regions: ['cn', 'jp'] }), null, 'a served storefront is fine');
  assert.equal(linkIssue(entry('https://music.apple.com/JP/album/x/1'), null, { regions: ['jp'] }), null, 'case does not matter');
  for (const regions of [null, undefined, []]) assert.equal(linkIssue(entry(JP), null, { regions }), null, 'unknown regions flag nothing');
  const links = scanLinks(`${JP} ${IGOR}`);
  assert.deepEqual(submittable(links, (e) => linkIssue(e, null, { regions: ['us'] })), [IGOR], 'the unserved link leaves the submit set');
  const noSf = scanLinks('https://music.apple.com/album/x/1', 'jp')[0];
  assert.equal(linkIssue(noSf, null, { regions: ['cn'] }).code, 'storefront_not_served', 'a borrowed storefront counts too');
});

test('servedRegions reads the wrapper status, or null when it cannot say', () => {
  const ok = { data: { ready: true, logged_in: true, regions: ['CN', 'us'] } };
  assert.deepEqual(servedRegions(ok), ['cn', 'us']);
  assert.equal(servedRegions(ok, true), null, 'simulate skips the backend check');
  assert.equal(servedRegions(undefined), null, 'not loaded');
  assert.equal(servedRegions({ error: { status: 0 } }), null, 'unreachable');
  assert.equal(servedRegions({ data: { ready: false, regions: ['cn'] } }), null);
  assert.equal(servedRegions({ data: { ready: true, logged_in: false, regions: ['cn'] } }), null);
  assert.equal(servedRegions({ data: { ready: true, logged_in: true, regions: [] } }), null);
  assert.equal(servedRegions({ data: { ready: true, logged_in: true } }), null);
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
  assert.equal(v[3].message, "The wrapper's Apple Music account is in another storefront");
  assert.equal(v[4].retry, true);
  assert.equal(v[4].detail, 'Database is locked.', 'a store error shows what the server said');
  const notReady = resultView({ status: 'invalid', error: { code: 'decryptor_unavailable', message: 'the wrapper is not ready' } });
  assert.deepEqual([notReady.role, notReady.label, notReady.retry], ['warning', 'Wrapper not ready', true]);
  const down = resultView({
    status: 'invalid',
    error: { code: 'decryptor_unavailable', message: 'cannot reach the wrapper: wrapper /status: Get "http://127.0.0.1:12399/status": dial tcp 127.0.0.1:12399: connect: connection refused' },
  });
  assert.deepEqual([down.role, down.label, down.retry], ['danger', 'Wrapper unreachable', true], 'a wrapper that does not answer is not "not ready"');
  assert.match(down.message, /wrapper\.address/);
  assert.equal(down.detail, 'Connection refused (127.0.0.1:12399)');
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
  assert.equal(submitSummary(rows), 'Started 1 download · 1 already downloading · 3 not accepted');
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
  const reworded = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'Apple Music does not know the storefront zz' } };
  assert.equal(linkIssue(zz, reworded).code, 'bad_storefront', 'a reworded server message still blocks');
  // The backend's catalog errors name the storefront in every case
  // (internal/applemusic catalogRequestError), so a missing id in a real
  // storefront must stay not_found.
  const current = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'read Apple Music catalog: Apple Music request for album 1463409338 in storefront zz failed: 400 Bad Request' } };
  assert.equal(linkIssue(zz, current).code, 'bad_storefront');
  const us = entry(IGOR);
  const gone = { state: 'error', error: { status: 404, code: 'catalog_not_found', message: 'read Apple Music catalog: Apple Music has no album 1463409338 in storefront us (404)' } };
  assert.equal(linkIssue(us, gone).code, 'not_found');
  const stray = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'read Apple Music catalog: request failed: 404 Not Found' } };
  assert.equal(linkIssue(us, stray).code, 'preview_failed', 'a 404 in the text of another error is not a missing id');
  const busy = { state: 'error', error: { status: 502, code: 'upstream_error', message: 'read Apple Music catalog: Apple Music request for album 1463409338 in storefront us failed: 503 Service Unavailable' } };
  assert.equal(linkIssue(us, busy).code, 'preview_failed', 'an outage in a real storefront is a warning, not a bad storefront');
  const bare = entry('https://music.apple.com/album/igor/1463409338');
  assert.deepEqual([linkIssue(bare).code, linkIssue(bare).blocking], ['no_storefront', true]);
});

test('submitSummary says started, then how started jobs finished', () => {
  const rows = resultRows([
    { url: IGOR, result: { url: IGOR, status: 'accepted', job: job('a', 'queued', '2026-10-29T00:00:00Z') } },
    { url: FOLKLORE, result: { url: FOLKLORE, status: 'accepted', job: job('b', 'queued', '2026-10-29T00:00:00Z') } },
  ]);
  assert.equal(submitSummary(rows), 'Started 2 downloads');
  const later = applyFeedToResults(rows, [
    { type: 'download_upserted', job: job('a', 'running', '2026-10-29T00:00:05Z') },
    { type: 'download_deleted', job_id: 'b' },
  ]);
  assert.equal(submitSummary(later), 'Started 2 downloads · 1 deleted', 'running is what started means');
  const done = applyFeedToResults(later, [{ type: 'download_upserted', job: job('a', 'completed', '2026-10-29T00:01:00Z') }]);
  assert.equal(submitSummary(done), 'Started 2 downloads · 1 completed · 1 deleted');
});

test('stored results round-trip with only the known overrides', () => {
  const rows = resultRows([{ url: IGOR, result: { url: IGOR, status: 'accepted', job: job('a', 'queued', '2026-10-29T00:00:00Z') } }]);
  const text = storedResults({ rows, overrides: { force_overwrite: true, media_user_token: 'secret' } });
  assert.ok(!text.includes('secret'));
  assert.deepEqual(parseStoredResults(text), { rows, overrides: { force_overwrite: true } });
  assert.equal(storedResults({ rows: [], overrides: {} }), '');
  for (const bad of [null, '', '{', '{"rows":[{"url":1}]}', '[]']) assert.equal(parseStoredResults(bad), null);
});

test('Download names what it sends, and the count line what the links are', () => {
  assert.equal(downloadLabel([], false), 'Paste a link to start');
  assert.equal(downloadLabel([], true), 'Download');
  assert.equal(downloadLabel([], true, true), 'Select a link to download');
  assert.equal(downloadLabel(['album']), 'Download 1 album');
  assert.equal(downloadLabel(['song', 'song']), 'Download 2 songs');
  assert.equal(downloadLabel(['album', 'song', 'playlist']), 'Download 3 items');
  assert.equal(countLine({}), '');
  assert.equal(countLine({ links: 4, fresh: 1, blocked: 3 }), "4 links · 3 can't be downloaded · 1 new", 'what stops comes first, where a cut line keeps it');
  assert.equal(countLine({ links: 2, fresh: 1, library: 1 }), '2 links · 1 new · 1 in library');
  assert.equal(countLine({ links: 3, fresh: 1, queued: 1, warned: 1 }), '3 links · 1 needs a look · 1 new · 1 in the queue');
  assert.doesNotMatch(countLine({ links: 5, blocked: 2, warned: 2 }), /flagged/);
  assert.equal(countLine({ links: 3, merged: 2, fresh: 3 }), '3 links · 2 merged · 3 new', 'repeats folded into one link are counted after the links');
});

test('a submitted row shows the preview until its job resolves', () => {
  const preview = { title: 'IGOR', artist_name: 'Tyler, The Creator', artwork_url: 'https://x/{w}x{h}bb.jpg', artwork_bg_color: 'f1a8bf', track_count: 12, type: 'album' };
  const rows = resultRows([{ url: IGOR, result: { url: IGOR, status: 'accepted', job: job('a', 'queued', '2026-10-29T00:00:00Z') } }], () => preview);
  assert.deepEqual(rows[0].preview, { title: 'IGOR', artist_name: 'Tyler, The Creator', artwork_url: 'https://x/{w}x{h}bb.jpg', artwork_bg_color: 'f1a8bf' });
  const seeded = seedJob(rows[0].job, rows[0].preview);
  assert.equal(seeded.title, 'IGOR');
  assert.equal(seeded.status, 'queued');
  assert.equal(seedJob({ ...rows[0].job, title: 'IGOR (Deluxe)' }, rows[0].preview).title, 'IGOR (Deluxe)', 'the job wins once it resolves');
  assert.equal(resultRows([{ url: IGOR, result: null }])[0].preview, null);
  const text = storedResults({ rows, overrides: {} });
  assert.equal(parseStoredResults(text).rows[0].preview.title, 'IGOR');
});
