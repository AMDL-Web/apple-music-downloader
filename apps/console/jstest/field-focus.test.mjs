import test from 'node:test';
import assert from 'node:assert/strict';

// focusField reads the DOM; a fake window/document and a manual frame queue stand in for it.
function setup(field) {
  const frames = [];
  const listeners = new Map();
  globalThis.window = {
    addEventListener: (type, fn) => listeners.set(type, fn),
    removeEventListener: (type) => listeners.delete(type),
  };
  globalThis.document = { querySelector: () => field };
  globalThis.requestAnimationFrame = (fn) => frames.push(fn);
  globalThis.cancelAnimationFrame = () => {};
  return { frames, listeners };
}

const { focusField } = await import('../static/js/ui/field-focus.js');

test('a field without an enabled control is scrolled to and the search ends', () => {
  const calls = [];
  const field = {
    scrollIntoView: (o) => calls.push(o),
    querySelector: (sel) => (sel === '.field-control' ? { querySelector: () => null } : null),
  };
  const { frames, listeners } = setup(field);
  focusField('download.max_attempts');
  assert.equal(frames.length, 1);
  frames.shift()();
  assert.deepEqual(calls, [{ block: 'center' }]);
  assert.equal(frames.length, 0, 'no further frame is requested');
  assert.equal(listeners.size, 0, 'the cancel listeners are removed');
});

test('an enabled control is focused without scrolling twice', () => {
  const calls = [];
  const focused = [];
  const control = { focus: (o) => focused.push(o) };
  const field = {
    scrollIntoView: (o) => calls.push(o),
    querySelector: () => ({ querySelector: () => control }),
  };
  const { frames } = setup(field);
  focusField('x.y');
  frames.shift()();
  assert.deepEqual(focused, [{ preventScroll: true }]);
  assert.equal(calls.length, 1);
  assert.equal(frames.length, 0);
});

test('a field not rendered yet keeps waiting', () => {
  const { frames } = setup(null);
  focusField('x.y');
  frames.shift()();
  assert.equal(frames.length, 1);
});
