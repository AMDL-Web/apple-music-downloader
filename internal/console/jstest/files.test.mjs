import test from 'node:test';
import assert from 'node:assert/strict';
import { parentDir, outputFolders } from '../static/js/lib/files.js';

const at = (output_path) => ({ id: output_path, status: 'completed', output_path });

test('an album written to one folder gives that folder', () => {
  const items = [
    at('/music/Taylor Swift/folklore [ALAC]/01. the 1.m4a'),
    at('/music/Taylor Swift/folklore [ALAC]/02. cardigan.m4a'),
  ];
  assert.deepEqual(outputFolders(items), ['/music/Taylor Swift/folklore [ALAC]']);
});

test('tracks in different folders give each folder once, in order of appearance', () => {
  const items = [at('/music/B/b1.m4a'), at('/music/A/a1.m4a'), at('/music/B/b2.m4a'), at('/music/C/c1.m4a')];
  assert.deepEqual(outputFolders(items), ['/music/B', '/music/A', '/music/C']);
});

test('items without a path give no folder', () => {
  assert.deepEqual(outputFolders([]), []);
  assert.deepEqual(outputFolders(undefined), []);
  assert.deepEqual(outputFolders([{ id: 'a', status: 'failed' }, { id: 'b', output_path: '' }, { id: 'c', output_path: null }]), []);
});

test('a folder is read from the paths that exist, not from a track still missing its file', () => {
  const items = [at('/music/A/a1.m4a'), { id: 'x', status: 'queued' }, { id: 'y', status: 'failed' }];
  assert.deepEqual(outputFolders(items), ['/music/A']);
});

test('Windows paths with mixed separators and letter case are one folder', () => {
  const items = [at('C:\\Music\\Artist\\Album\\01.m4a'), at('C:\\Music/Artist\\Album/02.m4a'), at('c:\\music\\artist\\album\\03.m4a')];
  assert.deepEqual(outputFolders(items), ['C:\\Music\\Artist\\Album']);
  assert.deepEqual(outputFolders([at('\\\\nas\\media\\A\\1.m4a'), at('\\\\nas\\media\\B\\2.m4a')]), ['\\\\nas\\media\\A', '\\\\nas\\media\\B']);
});

test('on a POSIX path a backslash is part of the name, so AC\\DC is one folder', () => {
  assert.deepEqual(outputFolders([at('/music/AC\\DC/Back in Black/01.m4a'), at('/music/AC\\DC/Back in Black/02.m4a')]), ['/music/AC\\DC/Back in Black']);
  assert.deepEqual(outputFolders([at('/music/AC\\DC/1.m4a'), at('/music/AC/DC/2.m4a')]), ['/music/AC\\DC', '/music/AC/DC']);
});

test('POSIX folders that differ in case stay different', () => {
  assert.deepEqual(outputFolders([at('/music/abba/1.m4a'), at('/music/ABBA/2.m4a')]), ['/music/abba', '/music/ABBA']);
});

test('parentDir handles roots, bare names and drive roots', () => {
  assert.equal(parentDir('/song.m4a'), '/');
  assert.equal(parentDir('song.m4a'), '');
  assert.equal(parentDir('C:\\song.m4a'), 'C:\\');
  assert.equal(parentDir('C:\\a\\song.m4a'), 'C:\\a');
  assert.equal(parentDir('/a/b/song.m4a'), '/a/b');
});
