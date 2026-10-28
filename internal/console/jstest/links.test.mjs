import test from 'node:test';
import assert from 'node:assert/strict';
import { parseLink, extractLinks, LINK_KINDS } from '../static/js/lib/links.js';

const IGOR = 'https://music.apple.com/us/album/igor/1463409338';
const MIDNIGHTS = 'https://music.apple.com/us/album/midnights/1650841512';
const FOLKLORE = 'https://music.apple.com/us/album/folklore/1524801260';

test('parseLink reads each kind the backend accepts', () => {
  assert.deepEqual(parseLink(IGOR), {
    url: IGOR,
    kind: 'album',
    storefront: 'us',
    id: '1463409338',
    trackId: null,
    private: false,
    needsToken: false,
  });
  assert.equal(parseLink('https://music.apple.com/jp/song/one-last-kiss/1542953970').kind, 'song');
  assert.equal(parseLink('https://music.apple.com/us/artist/tyler-the-creator/420368335').id, '420368335');
  assert.equal(parseLink('https://music.apple.com/us/music-video/earfquake/1468318460').kind, 'music-video');
  const playlist = parseLink('https://music.apple.com/us/playlist/todays-hits/pl.f4d106fed2bd41149aaacabb233eb5eb');
  assert.equal(playlist.kind, 'playlist');
  assert.equal(playlist.private, false);
  assert.equal(playlist.needsToken, false);
  assert.deepEqual(LINK_KINDS, ['album', 'song', 'playlist', 'artist', 'station', 'music-video']);
});

test('an album link with ?i= keeps kind album and carries the track id', () => {
  const p = parseLink('https://music.apple.com/cn/album/example/123456789?i=987654321&l=en');
  assert.equal(p.kind, 'album');
  assert.equal(p.id, '123456789');
  assert.equal(p.trackId, '987654321');
  assert.equal(p.url, 'https://music.apple.com/cn/album/example/123456789?i=987654321', 'only ?i= survives normalization');
  assert.equal(parseLink('https://music.apple.com/us/song/x/1?i=2').trackId, null, '?i= means a track only on album links');
});

test('private playlists and stations need a media-user-token', () => {
  const priv = parseLink('https://music.apple.com/us/playlist/my-mix/pl.u-abcDEF123');
  assert.equal(priv.private, true);
  assert.equal(priv.needsToken, true);
  const station = parseLink('https://music.apple.com/us/station/my-station/ra.u-1234');
  assert.equal(station.kind, 'station');
  assert.equal(station.private, false);
  assert.equal(station.needsToken, true);
});

test('normalization: scheme, host case, storefront case, fragment, trailing slash', () => {
  assert.equal(parseLink('music.apple.com/US/album/igor/1463409338/').url, IGOR);
  assert.equal(parseLink('http://MUSIC.apple.com/us/album/igor/1463409338#tracks').url, IGOR);
  assert.equal(parseLink(`${IGOR}?ls=1&app=music`).url, IGOR);
  assert.equal(parseLink('https://geo.music.apple.com/us/album/igor/1463409338').url, IGOR, 'geo. reads as music.apple.com');
  assert.equal(parseLink('https://beta.music.apple.com/us/album/igor/1463409338').url, 'https://beta.music.apple.com/us/album/igor/1463409338');
  assert.equal(parseLink('https://classical.music.apple.com/us/album/x/1').kind, 'album');
});

test('links the backend would refuse are kind unknown', () => {
  assert.equal(parseLink('https://music.apple.com/us/browse').kind, 'unknown', 'too short');
  assert.equal(parseLink('https://music.apple.com/usa/album/x/1').kind, 'unknown', 'storefront must be two letters');
  assert.equal(parseLink('https://music.apple.com/us/curator/apple-music/976439548').kind, 'unknown', 'unsupported type');
  assert.equal(parseLink('https://classical.music.apple.com/us/station/x/ra.1').kind, 'unknown', 'Classical has no stations');
  assert.equal(parseLink('https://music.apple.com/library/albums/l.abc').storefront, '');
});

test('parseLink returns null for anything that is not an Apple Music link', () => {
  for (const s of ['', '   ', 'hello', 'https://example.com/us/album/x/1', 'https://amp-api.music.apple.com/v1/catalog/us/albums/1', 'https://notmusic.apple.com/us/album/x/1', null, 42]) {
    assert.equal(parseLink(s), null, String(s));
  }
});

test('extractLinks finds links in lists, prose, Markdown and HTML', () => {
  assert.deepEqual(extractLinks(`${IGOR}\n${MIDNIGHTS}\r\n  ${FOLKLORE}  `), [IGOR, MIDNIGHTS, FOLKLORE]);
  assert.deepEqual(extractLinks(`${IGOR}, ${MIDNIGHTS},${FOLKLORE}`), [IGOR, MIDNIGHTS, FOLKLORE]);
  assert.deepEqual(extractLinks(`Try <${IGOR}> and [Midnights](${MIDNIGHTS}).`), [IGOR, MIDNIGHTS]);
  assert.deepEqual(extractLinks(`Listen: ${FOLKLORE}. Also **${IGOR}**!`), [FOLKLORE, IGOR]);
  assert.deepEqual(extractLinks(`<a href="${IGOR}?at=1">IGOR</a>`), [IGOR]);
  assert.deepEqual(extractLinks(`(${IGOR})`), [IGOR]);
});

test('extractLinks deduplicates normalized links and keeps the first order', () => {
  assert.deepEqual(extractLinks(`${MIDNIGHTS} ${IGOR}?l=en ${IGOR} music.apple.com/us/album/midnights/1650841512`), [MIDNIGHTS, IGOR]);
  const track = 'https://music.apple.com/us/album/igor/1463409338?i=1463409342';
  assert.deepEqual(extractLinks(`${IGOR} ${track}`), [IGOR, track], 'a track of an album is a different link');
});

test('extractLinks ignores other hosts and look-alikes', () => {
  assert.deepEqual(extractLinks(''), []);
  assert.deepEqual(extractLinks(null), []);
  assert.deepEqual(extractLinks('https://example.com/us/album/x/1 and https://open.spotify.com/album/1'), []);
  assert.deepEqual(extractLinks('https://notmusic.apple.com/us/album/x/1 user@music.apple.com/us/album/x/1'), []);
  assert.deepEqual(extractLinks('see https://music.apple.com/'), [], 'a bare host is not a link');
});

test('extractLinks keeps links the backend would refuse, for the page to explain', () => {
  assert.deepEqual(extractLinks('https://music.apple.com/us/browse'), ['https://music.apple.com/us/browse']);
});

test('percent-encoded and non-ASCII slugs survive', () => {
  const encoded = 'https://music.apple.com/jp/album/%E3%82%A2/1';
  assert.equal(parseLink(encoded).url, encoded);
  assert.deepEqual(extractLinks(`https://music.apple.com/jp/album/ア/1`), [encoded]);
});
