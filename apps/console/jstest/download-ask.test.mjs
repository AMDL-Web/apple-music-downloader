import assert from 'node:assert/strict';
import { test } from 'node:test';

import { againText, answerOverrides, codecChoices, downloadAsks, hasAsks } from '../static/js/lib/download-ask.js';

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

test('a quality chosen on the page asks nothing; Download again always confirms', () => {
  assert.deepEqual(downloadAsks({ traits: both, priority: ['alac', 'ec3'], follow: false }), { codecs: [], again: 0 });
  assert.deepEqual(downloadAsks({ traits: both, priority: ['alac', 'ec3'], again: 12 }), { codecs: ['alac', 'ec3'], again: 12 });
  assert.equal(hasAsks(downloadAsks({ traits: [], priority: [] })), false, 'a plain download goes at once: the server skips files on disk');
  assert.equal(hasAsks(downloadAsks({ traits: [], priority: [], again: 1 })), true);
});

test('the answer leads quality_priority with the choice; only Download again forces', () => {
  assert.deepEqual(answerOverrides({ codec: 'ec3', priority: ['alac', 'ec3', 'aac'] }), { quality_priority: ['ec3', 'alac', 'aac'] });
  assert.deepEqual(answerOverrides({}), {}, "a plain download leaves the server's force_overwrite alone");
  assert.deepEqual(answerOverrides({ again: 3 }), { force_overwrite: true });
});

test('Download again says what it replaces in one line', () => {
  assert.equal(againText(1), 'Replaces the file on disk.');
  assert.equal(againText(12), 'Replaces the 12 files on disk.');
});
