import test from 'node:test';
import assert from 'node:assert/strict';
import { SSEParser } from '../static/js/lib/sse.js';

test('named events with ids', () => {
  const p = new SSEParser();
  const out = p.push('id: 42\nevent: download_upserted\ndata: {"event_id":42}\n\n');
  assert.deepEqual(out, [{ type: 'download_upserted', data: '{"event_id":42}', id: '42' }]);
});

test('events without a name are "message"', () => {
  assert.deepEqual(new SSEParser().push('data: x\n\n'), [{ type: 'message', data: 'x', id: null }]);
});

test('multi-line data joins with LF; comments and unknown fields are ignored', () => {
  const p = new SSEParser();
  const out = p.push(': keepalive\nretry: 5000\nfoo: bar\ndata: line one\ndata: line two\n\n');
  assert.deepEqual(out, [{ type: 'message', data: 'line one\nline two', id: null }]);
});

test('chunks may split lines, and CRLF and lone CR end lines', () => {
  const p = new SSEParser();
  assert.deepEqual(p.push('id: 1\r\nevent: lo'), []);
  assert.deepEqual(p.push('g\r\ndata: {"a"'), []);
  assert.deepEqual(p.push(':1}\r'), []);
  assert.deepEqual(p.push('\n\r'), [{ type: 'log', data: '{"a":1}', id: '1' }]);
  assert.deepEqual(p.push('data: b\r\r'), [{ type: 'message', data: 'b', id: null }]);
});

test('a block without data dispatches nothing', () => {
  const p = new SSEParser();
  assert.deepEqual(p.push('id: 7\nevent: x\n\n'), []);
  assert.deepEqual(p.push(': only a comment\n\n'), []);
});

test('a value keeps everything after the first colon and one leading space', () => {
  assert.deepEqual(new SSEParser().push('data:  two spaces: kept\n\n'), [{ type: 'message', data: ' two spaces: kept', id: null }]);
  assert.deepEqual(new SSEParser().push('data\n\n'), [{ type: 'message', data: '', id: null }]);
});

test('an id containing NUL is ignored', () => {
  assert.deepEqual(new SSEParser().push('id: 1\u0000\ndata: x\n\n'), [{ type: 'message', data: 'x', id: null }]);
});

test('several events in one chunk', () => {
  const out = new SSEParser().push('id: 1\nevent: log\ndata: a\n\nid: 2\nevent: log\ndata: b\n\n');
  assert.deepEqual(out.map((e) => e.id), ['1', '2']);
});
