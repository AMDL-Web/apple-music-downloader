import assert from 'node:assert/strict';
import { test } from 'node:test';

import { againCall, againText, answerOverrides, codecChoices, downloadAsks, hasAsks, slotChoices } from '../static/js/lib/download-ask.js';

const both = ['lossless', 'atmos'];

test('the quality is asked only when the release and the server both have lossless and Dolby', () => {
  assert.deepEqual(codecChoices(both, ['alac', 'ec3', 'aac']), ['alac', 'ec3']);
  assert.deepEqual(codecChoices(['hi-res-lossless', 'atmos'], ['ec3', 'alac']), ['ec3', 'alac']);
  assert.deepEqual(codecChoices(both, ['ac3', 'alac', 'ec3']), ['ac3', 'alac', 'ec3']);
  assert.deepEqual(codecChoices(['lossless'], ['alac', 'ec3']), []);
  assert.deepEqual(codecChoices(['atmos'], ['alac', 'ec3']), []);
  assert.deepEqual(codecChoices(both, ['alac', 'aac']), []);
  assert.deepEqual(codecChoices(both, ['ec3', 'aac']), []);
  assert.deepEqual(codecChoices(undefined, undefined), []);
});

test('a quality chosen on the page asks nothing; Download again always confirms and offers another quality', () => {
  assert.deepEqual(downloadAsks({ traits: both, priority: ['alac', 'ec3'], follow: false }), { codecs: [], again: 0, slots: [] });
  const again = downloadAsks({ traits: both, priority: ['alac', 'ec3'], again: 12 });
  assert.deepEqual(again.codecs, [], 'Download again keeps the quality the record has');
  assert.equal(again.again, 12);
  assert.ok(again.slots.includes('ec3') && again.slots.includes('alac'));
  assert.equal(hasAsks(downloadAsks({ traits: [], priority: [] })), false, 'a plain download goes at once: the record knows what it has');
  assert.equal(hasAsks(downloadAsks({ traits: [], priority: [], again: 1 })), true);
});

test('another quality is offered only where the release has it', () => {
  assert.deepEqual(slotChoices(['lossy-stereo']), ['aac', 'aac-binaural', 'aac-downmix']);
  assert.deepEqual(slotChoices(['lossless', 'lossy-stereo']), ['alac', 'aac', 'aac-binaural', 'aac-downmix']);
  assert.ok(slotChoices([]).includes('ec3'), 'unknown traits offer every codec');
});

test('the answer leads quality_priority with the choice', () => {
  assert.deepEqual(answerOverrides({ codec: 'ec3', priority: ['alac', 'ec3', 'aac'] }), { quality_priority: ['ec3', 'alac', 'aac'] });
  assert.deepEqual(answerOverrides({}), {});
});

test('Download again resets the main slots; another quality is a download of that codec', () => {
  assert.deepEqual(againCall({}), { call: 'reset', body: { what: 'audio', slot: 'main' } });
  assert.deepEqual(againCall({ songs: ['7'] }), { call: 'reset', body: { what: 'audio', slot: 'main', songs: ['7'] } });
  assert.deepEqual(againCall({ slot: 'ec3', songs: ['7'] }), { call: 'download', body: { codec: 'ec3', songs: ['7'] } });
});

test('Download again says what it replaces in one line', () => {
  assert.equal(againText(1), 'Replaces the downloaded file.');
  assert.equal(againText(12), 'Replaces the 12 downloaded files.');
});
