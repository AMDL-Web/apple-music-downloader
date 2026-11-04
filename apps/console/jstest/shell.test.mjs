import test from 'node:test';
import assert from 'node:assert/strict';
import { TabBar } from '../static/js/views/shell.js';

const tabs = (page, library = true) =>
  TabBar({ route: { page }, library })
    .props.children.flat(Infinity)
    .filter(Boolean)
    .map((t) => ({ page: t.props['data-page'], current: t.props['aria-current'] }));

test('the phone tab bar has five tabs with New download in the middle', () => {
  assert.deepEqual(
    tabs('overview').map((t) => t.page),
    ['overview', 'queue', 'new', 'library', 'settings'],
  );
});

test('the Library tab is there only while an account copies its Apple library', () => {
  assert.deepEqual(
    tabs('overview', false).map((t) => t.page),
    ['overview', 'queue', 'new', 'settings'],
  );
});

test('each tab is current only on its own page', () => {
  for (const page of ['overview', 'queue', 'new', 'library', 'settings']) {
    const current = tabs(page).filter((t) => t.current === 'page');
    assert.deepEqual(current.map((t) => t.page), [page]);
  }
  assert.equal(tabs('job').filter((t) => t.current).length, 0);
});
