import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { motionVariants, pickMotionVariant, motionFile } from '../static/js/lib/motion.js';

// Captured from Apple's CDN: the motion cover of 月に向かって撃て - EP (cn
// album 1858184006), its master playlist and the 360x360 H.264 rendition.
const BASE = 'https://mvod.itunes.apple.com/itunes-assets/HLSMusic221/v4/cc/b4/e1/ccb4e1d9-8e27-b1fc-3ac3-cd04ae7cf90c/';
const master = readFileSync(new URL('./fixtures/motion-master.m3u8', import.meta.url), 'utf8');
const media = readFileSync(new URL('./fixtures/motion-media.m3u8', import.meta.url), 'utf8');

test('motionVariants lists the video renditions, not the trick-play ones', () => {
  const v = motionVariants(master, `${BASE}P1246091480_default.m3u8`);
  assert.equal(v.length, 29);
  assert.ok(v.every((x) => x.url.startsWith(BASE) && x.url.endsWith('.m3u8')));
  assert.ok(v.every((x) => !x.url.includes('iframes')));
  assert.ok(v.some((x) => x.codecs.startsWith('hvc1')), 'HEVC renditions are listed too');
});

test('pickMotionVariant takes the smallest H.264 rendition that is sharp enough', () => {
  const v = motionVariants(master, `${BASE}P1246091480_default.m3u8`);
  const small = pickMotionVariant(v, 224);
  assert.equal(small.width, 360);
  assert.match(small.codecs, /^avc1\./);
  assert.match(small.url, /_360x360\.m3u8$/);
  assert.equal(pickMotionVariant(v, 700).width, 768);
  const huge = pickMotionVariant(v, 4000);
  assert.equal(huge.width, 1080, 'the largest when none reaches the box');
  assert.match(huge.codecs, /^avc1\./);
  assert.equal(pickMotionVariant(v.filter((x) => x.codecs.startsWith('hvc1')), 224), null, 'HEVC only: nothing to play');
  assert.equal(pickMotionVariant([], 224), null);
});

test('motionFile names the one MP4 the byte ranges cut', () => {
  assert.equal(motionFile(media, `${BASE}P1246091480_Anull_video_gr203_sdr_360x360.m3u8`), `${BASE}P1246091480_Anull_video_gr203_sdr_360x360-.mp4`);
  const split = '#EXTM3U\n#EXT-X-MAP:URI="init.mp4"\n#EXTINF:4,\nseg1.m4s\n#EXTINF:4,\nseg2.m4s\n#EXT-X-ENDLIST\n';
  assert.equal(motionFile(split, BASE), '', 'separate segment files have no single file to play');
  assert.equal(motionFile('#EXTM3U\n#EXTINF:4,\na.ts\n', BASE), '');
  assert.equal(motionFile('', BASE), '');
});
