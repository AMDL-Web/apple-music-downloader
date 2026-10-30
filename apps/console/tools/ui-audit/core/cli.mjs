#!/usr/bin/env node
// ui-audit command line. See ../README.md.
//
//   node core/cli.mjs run --config <config.mjs> --out <dir> [--engine chrome|webkit] [--widths 1280,375]
//                         [--themes light,dark] [--only page[,page/state]] [--no-states] [--quick]
//   node core/cli.mjs report <result.json> --out <dir>
//   node core/cli.mjs diff <before/report.json> <after/report.json> [--out <file.json>]

import { readFileSync, writeFileSync, mkdirSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { pathToFileURL } from 'node:url';
import { withDefaults } from './config.mjs';
import { audit } from './run.mjs';
import { aggregate, writeHtml, compare } from './report.mjs';

const USAGE = `usage:
  cli.mjs run --config <config.mjs> --out <dir> [--engine chrome|webkit] [--widths W,W] [--themes light,dark]
              [--only id,id] [--no-states] [--quick]
  cli.mjs report <result.json> --out <dir>
  cli.mjs diff <before/report.json> <after/report.json> [--out <file.json>]`;

function fail(msg) {
  process.stderr.write(`${msg ? `${msg}\n` : ''}${USAGE}\n`);
  process.exit(2);
}

export function parseArgs(argv) {
  const [cmd, ...rest] = argv;
  if (!['run', 'report', 'diff'].includes(cmd)) fail(cmd ? `unknown command ${cmd}` : '');
  const o = { cmd, positional: [], states: true };
  const needs = new Set(['--config', '--out', '--engine', '--widths', '--themes', '--only']);
  for (let i = 0; i < rest.length; i++) {
    const a = rest[i];
    if (needs.has(a)) {
      const v = rest[++i];
      if (v === undefined || v.startsWith('--')) fail(`${a} needs a value`);
      o[a.slice(2)] = v;
    } else if (a === '--no-states') o.states = false;
    else if (a === '--quick') o.quick = true;
    else if (a.startsWith('--')) fail(`unknown flag ${a}`);
    else o.positional.push(a);
  }
  if (o.engine && !['chrome', 'webkit'].includes(o.engine)) fail(`unknown engine ${o.engine}`);
  for (const k of ['widths']) {
    if (o[k]) {
      o[k] = o[k].split(',').map(Number);
      if (o[k].some((n) => !Number.isInteger(n) || n < 200)) fail(`bad ${k}`);
    }
  }
  if (o.themes) {
    o.themes = o.themes.split(',');
    if (o.themes.some((t) => t !== 'light' && t !== 'dark')) fail('themes are light and dark');
  }
  if (o.only) o.only = o.only.split(',');
  if (cmd === 'run' && (!o.config || !o.out)) fail('run needs --config and --out');
  if (cmd === 'report' && (o.positional.length !== 1 || !o.out)) fail('report needs <result.json> and --out');
  if (cmd === 'diff' && o.positional.length !== 2) fail('diff needs two report.json files');
  return o;
}

async function main() {
  const o = parseArgs(process.argv.slice(2));
  if (o.cmd === 'run') {
    const cfg = withDefaults((await import(pathToFileURL(resolve(o.config)).href)).default);
    const out = resolve(o.out);
    mkdirSync(out, { recursive: true });
    const widths = o.widths || (o.quick ? [1280, 375] : undefined);
    const themes = o.themes || (o.quick ? ['dark'] : undefined);
    const result = await audit(cfg, { out, engines: [o.engine || 'chrome'], widths, themes, only: o.only, states: o.states, log: (l) => process.stdout.write(`${l}\n`) });
    const rep = aggregate(result);
    writeHtml(rep, out);
    process.stdout.write(`\n${rep.findings.length} findings in ${Math.round(result.ms / 1000)} s → ${out}/index.html\n`);
    for (const [c, n] of Object.entries(rep.byCheck)) process.stdout.write(`  ${c.padEnd(13)} high ${n.high}  medium ${n.medium}  low ${n.low}\n`);
  } else if (o.cmd === 'report') {
    const result = JSON.parse(readFileSync(o.positional[0], 'utf8'));
    const rep = aggregate(result);
    mkdirSync(o.out, { recursive: true });
    writeHtml(rep, o.out);
    process.stdout.write(`${rep.findings.length} findings → ${o.out}/index.html\n`);
  } else {
    const [a, b] = o.positional.map((p) => JSON.parse(readFileSync(p, 'utf8')));
    const d = compare(a, b);
    for (const [c, n] of Object.entries(d.counts)) process.stdout.write(`${c.padEnd(13)} ${String(n.before).padStart(4)} → ${String(n.after).padStart(4)}   high ${n.beforeHigh} → ${n.afterHigh}\n`);
    process.stdout.write(`resolved ${d.resolved.length}, new ${d.added.length}\n`);
    if (o.out) {
      mkdirSync(dirname(resolve(o.out)), { recursive: true });
      writeFileSync(o.out, JSON.stringify(d, null, 1));
    }
  }
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((e) => {
    process.stderr.write(`${e && e.stack ? e.stack : e}\n`);
    process.exit(1);
  });
}
