import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { declarations, readScale, scanCss } from './css-scan.mjs';
import { aggregate, compare, normalize } from './report.mjs';
import { probeSource } from './probe.mjs';
import { readCsp, startMockServer } from '../console/mock-server.mjs';
import { jobId, JOB_KEYS } from '../console/fixtures.mjs';

const cli = fileURLToPath(new URL('./cli.mjs', import.meta.url));

test('the CLI exits 2 with usage on bad arguments', () => {
  for (const args of [[], ['frobnicate'], ['run', '--out', 'x'], ['run', '--config', 'c', '--out', 'x', '--engine', 'firefox'], ['run', '--config', 'c', '--out', 'x', '--bogus'], ['run', '--config', '--out', 'x'], ['diff', 'one.json']]) {
    const r = spawnSync(process.execPath, [cli, ...args], { encoding: 'utf8' });
    assert.equal(r.status, 2, `args ${JSON.stringify(args)}`);
    assert.match(r.stderr, /usage:/);
  }
});

test('the css scan reads the scale and finds literals and layout transitions with their line', () => {
  const dir = mkdtempSync(join(tmpdir(), 'uia-'));
  const tokens = join(dir, 'tokens.css');
  const page = join(dir, 'page.css');
  writeFileSync(tokens, ':root {\n  --s-1: 4px;\n  --s-2: 8px;\n  --r-card: 22px;\n  --r-sheet: calc(var(--r-card) + var(--s-1));\n  --fs-13: 13px;\n}\n');
  writeFileSync(page, '.a {\n  padding: 4px var(--s-2);\n  gap: 5px;\n  /* margin: 7px; */\n  border-radius: calc(var(--r-card) - 4px);\n  transition: height 200ms;\n}\n');
  assert.deepEqual(declarations('.x{a:1px;}\n.y{\n b: 2px;\n}')[1], { prop: 'b', value: '2px', line: 3 });
  const scale = readScale([tokens], { space: /^--s-/, radius: /^--r-/, font: /^--fs-/ });
  assert.deepEqual(scale.space, [0, 4, 8]);
  assert.deepEqual(scale.radius, [0, 22]);
  assert.deepEqual(scale.vars, { space: [], radius: ['--r-sheet'], font: [] });
  const found = scanCss([page], { root: dir, scale });
  const at = (line) => found.filter((f) => f.line === line).map((f) => f.check);
  assert.deepEqual(at(2), ['tokens']);
  assert.equal(found.find((f) => f.line === 2).data.onScale, true);
  assert.deepEqual(at(3), ['tokens']);
  assert.equal(found.find((f) => f.line === 3).data.onScale, false);
  assert.deepEqual(at(4), []);
  assert.deepEqual(at(5), []);
  assert.deepEqual(at(6), ['flicker']);
});

test('findings merge across variants by check, element and message shape', () => {
  assert.equal(normalize('icon 2.5px below "Queue"'), normalize('icon 1px below "Logs"'));
  const f = (variant, dy) => ({ scenario: 's', variant, findings: [{ check: 'alignment', sig: 'svg.icon', sel: 'x', msg: `icon ${dy}px below the centre`, severity: 'low' }] });
  const rep = aggregate({ runs: [f('chrome-1280-dark', 2), f('chrome-375-light', 3)], static: [] });
  assert.equal(rep.findings.length, 1);
  assert.equal(rep.findings[0].occurrences.length, 2);
  const after = aggregate({ runs: [], static: [] });
  const d = compare(rep, after);
  assert.equal(d.resolved.length, 1);
  assert.equal(d.counts.alignment.before, 1);
  assert.equal(d.counts.alignment.after, 0);
});

test('the probe compiles into one expression', () => {
  const src = probeSource();
  assert.doesNotThrow(() => new Function(src));
  assert.doesNotMatch(src, /^export /m);
});

test('the mock serves the CSP the Go server sends', () => {
  const go = readFileSync(fileURLToPath(new URL('../../../console.go', import.meta.url)), 'utf8');
  const csp = readCsp(go);
  assert.match(csp, /^default-src 'self';/);
  assert.match(csp, /frame-ancestors 'none'$/);
  assert.doesNotMatch(csp, /"/);
});

test('the mock answers the console contract and moves jobs one step per tick', async () => {
  const s = await startMockServer({ world: 'active' });
  try {
    const page = await fetch(s.url);
    assert.equal(page.status, 200);
    assert.equal(page.headers.get('content-security-policy'), readCsp());
    const list = await (await fetch(`${s.origin}/api/v1/downloads?status=running,queued&limit=50`)).json();
    assert.ok(list.downloads.every((j) => j.status === 'running' || j.status === 'queued'));
    assert.equal(list.status_counts.total, list.total + list.status_counts.completed + list.status_counts.failed + list.status_counts.cancelled);
    assert.equal(typeof list.event_cursor, 'number');
    const id = jobId(JOB_KEYS.running22);
    const before = await (await fetch(`${s.origin}/api/v1/downloads/${id}`)).json();
    assert.equal(before.items.length, 22);
    assert.ok(s.tick(1) > 0);
    const after = await (await fetch(`${s.origin}/api/v1/downloads/${id}`)).json();
    assert.notDeepEqual(after.items.map((i) => i.status), before.items.map((i) => i.status));
    // GET /config is {config: {<section>: {...}}, persisted}, one level deep:
    // Settings and New download read config.download straight off it.
    const cfg = await (await fetch(`${s.origin}/api/v1/config`)).json();
    assert.equal(typeof cfg.config.download.alac_max_bit_depth, 'number');
    assert.equal(cfg.config.config, undefined);
    const missing = await fetch(`${s.origin}/api/v1/downloads/job_${'0'.repeat(32)}`);
    assert.equal(missing.status, 404);
    assert.equal((await missing.json()).error.code, 'not_found');
    s.setFaults({ 'GET /api/v1/stats': { status: 500 } });
    assert.equal((await fetch(`${s.origin}/api/v1/stats`)).status, 500);
  } finally {
    await s.close();
  }
});
