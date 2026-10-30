// Turns a run result into one deduplicated finding list (plus the
// cross-page consistency and spacing-rhythm reports) and an HTML page with
// the crops; `compare` diffs two of them for before → after.

import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { modeOf, onScale } from './lib/geometry.mjs';

const SEV = ['high', 'medium', 'low', 'info'];
const rank = (s) => SEV.indexOf(s);

/** The part of a message that stays the same across variants: numbers become #. */
export function normalize(msg) {
  return String(msg)
    .replace(/-?\d+(\.\d+)?/g, '#')
    .replace(/"[^"]*"/g, '"…"')
    .replace(/\s+/g, ' ')
    .trim();
}

export function findingKey(f) {
  return `${f.check}|${f.sig}|${normalize(f.msg)}`;
}

export function aggregate(result) {
  const groups = new Map();
  for (const run of result.runs) {
    for (const f of run.findings || []) {
      const key = findingKey(f);
      if (!groups.has(key)) groups.set(key, { key, check: f.check, sig: f.sig, sel: f.sel, msg: f.msg, severity: f.severity, crop: null, occurrences: [] });
      const g = groups.get(key);
      if (rank(f.severity) < rank(g.severity)) {
        g.severity = f.severity;
        g.msg = f.msg;
      }
      if (!g.crop && f.crop) g.crop = { ...f.crop, variant: run.variant, scenario: run.scenario };
      g.occurrences.push({ scenario: run.scenario, variant: run.variant, msg: f.msg, data: f.data });
    }
  }
  for (const f of result.static || []) {
    const key = `${f.check}|${f.file}:${f.line}|${normalize(f.msg)}`;
    groups.set(key, { key, check: f.check, sig: `${f.file}:${f.line}`, sel: f.sel, msg: f.msg, severity: f.severity, file: f.file, line: f.line, occurrences: [{ scenario: 'static', variant: 'css', msg: f.msg, data: f.data }] });
  }
  for (const f of consistency(result)) groups.set(findingKey(f), f);
  for (const f of rhythm(result)) groups.set(findingKey(f), f);
  const findings = [...groups.values()].sort((a, b) => rank(a.severity) - rank(b.severity) || b.occurrences.length - a.occurrences.length || a.check.localeCompare(b.check));
  const byCheck = {};
  for (const f of findings) {
    byCheck[f.check] = byCheck[f.check] || { high: 0, medium: 0, low: 0, info: 0, total: 0 };
    byCheck[f.check][f.severity]++;
    byCheck[f.check].total++;
  }
  const cls = [];
  for (const run of result.runs) {
    if (run.metrics && run.metrics.loadCls !== undefined) cls.push({ scenario: run.scenario, variant: run.variant, what: 'load', cls: run.metrics.loadCls });
    for (const t of run.transitions || []) if (t.cls) cls.push({ scenario: run.scenario, variant: run.variant, what: t.id, cls: +t.cls.total.toFixed(4) });
  }
  const stepErrors = [];
  for (const r of result.runs) {
    if (r.setupError) stepErrors.push(`${r.variant} ${r.scenario} setup: ${r.setupError}`);
    for (const t of r.transitions || []) if (t.error) stepErrors.push(`${r.variant} ${r.scenario} ${t.id}: ${t.error}`);
  }
  return { project: result.project, startedAt: result.startedAt, ms: result.ms, options: result.options, runs: result.runs.length, stepErrors, skipped: result.runs.filter((r) => r.skipped).map((r) => `${r.variant} ${r.scenario}: ${r.skipped}`), errors: errorsOf(result), byCheck, cls, findings };
}

function errorsOf(result) {
  const out = new Map();
  for (const r of result.runs) for (const e of r.errors || []) out.set(e, (out.get(e) || 0) + 1);
  return [...out.entries()].map(([e, n]) => ({ error: e, runs: n }));
}

const widthClass = (variant) => {
  const w = Number(String(variant).split('-')[1]);
  return w < 640 ? 'phone' : w < 1024 ? 'tablet' : 'desktop';
};

/** The same component class drawn at different sizes in different places. */
export function consistency(result) {
  const groups = new Map();
  for (const run of result.runs) {
    for (const c of run.components || []) {
      const key = `${c.kind}|${c.sig}|${widthClass(run.variant)}`;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push({ ...c, scenario: run.scenario, variant: run.variant });
    }
  }
  const out = [];
  for (const [key, list] of groups) {
    const [kind, sig, wc] = key.split('|');
    for (const prop of ['h', 'fs', 'radius', 'pl']) {
      const vals = list.map((c) => c[prop]);
      const { mode, outliers } = modeOf(vals, 1);
      if (!outliers.length) continue;
      const odd = list.filter((c) => Math.abs(c[prop] - mode) > 1);
      const scenarios = new Set(odd.map((c) => c.scenario));
      // One scenario drawing it two ways in one place is layout (wrapping), not a variant.
      if (odd.length < 1) continue;
      const name = { h: 'height', fs: 'font size', radius: 'radius', pl: 'left padding' }[prop];
      out.push({
        key: `consistency|${sig}|${wc}|${prop}`,
        check: 'consistency',
        sig,
        sel: odd[0].sel,
        msg: `${kind} ${sig} (${wc}) has ${name} ${mode}px in ${list.length - odd.length} places and ${[...new Set(odd.map((c) => c[prop]))].join('/')}px in ${odd.length} (${[...scenarios].slice(0, 3).join(', ')})`,
        severity: prop === 'h' || prop === 'fs' ? 'medium' : 'low',
        occurrences: odd.slice(0, 8).map((c) => ({ scenario: c.scenario, variant: c.variant, msg: `${c.sel}: ${c[prop]}px "${c.text}"`, data: c })),
      });
    }
  }
  return out;
}

/** The same relationship spaced differently from page to page, uneven padding, off-scale gaps. */
export function rhythm(result) {
  const groups = new Map();
  const scale = (result.scale && result.scale.space) || [];
  for (const run of result.runs) {
    for (const m of run.rhythm || []) {
      const key = `${m.name}|${widthClass(run.variant)}`;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push({ ...m, scenario: run.scenario, variant: run.variant });
    }
  }
  const out = [];
  for (const [key, list] of groups) {
    const [name, wc] = key.split('|');
    const props = Object.keys(list[0].values);
    for (const p of props) {
      const vals = list.map((m) => m.values[p]).filter((x) => typeof x === 'number' && !Number.isNaN(x));
      if (!vals.length) continue;
      const { mode, outliers } = modeOf(vals, 1);
      if (outliers.length) {
        const odd = list.filter((m) => Math.abs(m.values[p] - mode) > 1);
        const pages = [...new Set(odd.map((m) => m.scenario))];
        out.push({ key: `rhythm|${key}|${p}`, check: 'rhythm', sig: odd[0].sig, sel: odd[0].sel, msg: `${name} ${p} is ${mode}px on most pages (${wc}) but ${[...new Set(odd.map((m) => m.values[p]))].slice(0, 4).join('/')}px on ${pages.slice(0, 4).join(', ')}${pages.length > 4 ? '…' : ''}`, severity: 'medium', occurrences: odd.slice(0, 10).map((m) => ({ scenario: m.scenario, variant: m.variant, msg: `${m.sel}: ${JSON.stringify(m.values)}`, data: m })) });
      }
      if (scale.length && !onScale(Math.abs(mode), scale, 0.6) && Math.abs(mode) > 1) out.push({ key: `rhythm-scale|${key}|${p}`, check: 'rhythm', sig: list[0].sig, sel: list[0].sel, msg: `${name} ${p} is ${mode}px (${wc}), off the spacing scale`, severity: 'low', occurrences: list.slice(0, 4).map((m) => ({ scenario: m.scenario, variant: m.variant, msg: `${m.sel}: ${JSON.stringify(m.values)}`, data: m })) });
    }
    if (props.includes('left') && props.includes('right')) {
      const uneven = list.filter((m) => Math.abs(m.values.left - m.values.right) > 1.5);
      if (uneven.length) out.push({ key: `rhythm-lr|${key}`, check: 'rhythm', sig: uneven[0].sig, sel: uneven[0].sel, msg: `${name}: left and right differ (${uneven[0].values.left} vs ${uneven[0].values.right}px) in ${uneven.length} of ${list.length} (${wc})`, severity: 'low', occurrences: uneven.slice(0, 8).map((m) => ({ scenario: m.scenario, variant: m.variant, msg: `${m.sel}: ${JSON.stringify(m.values)}`, data: m })) });
    }
    if (props.includes('top') && props.includes('bottom')) {
      const uneven = list.filter((m) => Math.abs(m.values.top - m.values.bottom) > 1.5);
      if (uneven.length) out.push({ key: `rhythm-tb|${key}`, check: 'rhythm', sig: uneven[0].sig, sel: uneven[0].sel, msg: `${name}: top and bottom differ (${uneven[0].values.top} vs ${uneven[0].values.bottom}px) in ${uneven.length} of ${list.length} (${wc})`, severity: 'low', occurrences: uneven.slice(0, 8).map((m) => ({ scenario: m.scenario, variant: m.variant, msg: `${m.sel}: ${JSON.stringify(m.values)}`, data: m })) });
    }
  }
  return out;
}

/** before → after: per-check counts and which findings went away or appeared. */
export function compare(before, after) {
  const a = new Map(before.findings.map((f) => [f.key, f]));
  const b = new Map(after.findings.map((f) => [f.key, f]));
  const checks = new Set([...Object.keys(before.byCheck), ...Object.keys(after.byCheck)]);
  const counts = {};
  for (const c of checks) counts[c] = { before: (before.byCheck[c] || { total: 0 }).total, after: (after.byCheck[c] || { total: 0 }).total, beforeHigh: (before.byCheck[c] || { high: 0 }).high, afterHigh: (after.byCheck[c] || { high: 0 }).high };
  return {
    counts,
    resolved: [...a.values()].filter((f) => !b.has(f.key)),
    added: [...b.values()].filter((f) => !a.has(f.key)),
    cls: { before: before.cls, after: after.cls },
  };
}

const esc = (s) => String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' })[c]);

export function writeHtml(rep, out) {
  const rows = rep.findings
    .map((f) => {
      const crop = f.crop
        ? `<div class="crop" style="width:${f.crop.w}px;height:${f.crop.h}px;background-image:url('${esc(f.crop.file)}')"><div class="box" style="left:${f.crop.box.x}px;top:${f.crop.box.y}px;width:${f.crop.box.w}px;height:${f.crop.box.h}px"></div></div><div class="cap">${esc(f.crop.scenario)} · ${esc(f.crop.variant)}</div>`
        : '';
      const occ = f.occurrences.slice(0, 12).map((o) => `<li><b>${esc(o.scenario)}</b> ${esc(o.variant)} — ${esc(o.msg)}</li>`).join('');
      return `<article class="f ${f.severity}"><header><span class="sev">${f.severity}</span><span class="chk">${esc(f.check)}</span><span class="n">${f.occurrences.length}×</span></header><p class="msg">${esc(f.msg)}</p><code>${esc(f.sel)}</code>${crop}<details><summary>where</summary><ul>${occ}</ul></details></article>`;
    })
    .join('\n');
  const summary = Object.entries(rep.byCheck)
    .map(([c, n]) => `<tr><td>${esc(c)}</td><td>${n.high}</td><td>${n.medium}</td><td>${n.low}</td><td>${n.total}</td></tr>`)
    .join('');
  const html = `<!doctype html><meta charset="utf-8"><title>ui-audit · ${esc(rep.project)}</title>
<style>
body{font:14px/1.45 system-ui,sans-serif;margin:24px;background:#fafafa;color:#111}
table{border-collapse:collapse;margin:12px 0}td,th{border:1px solid #ddd;padding:4px 10px;text-align:right}td:first-child{text-align:left}
.f{background:#fff;border:1px solid #e3e3e3;border-radius:10px;padding:12px 14px;margin:10px 0;max-width:960px}
.f header{display:flex;gap:8px;align-items:center}.sev{font-weight:700;text-transform:uppercase;font-size:11px;padding:2px 6px;border-radius:4px;background:#eee}
.high .sev{background:#fde2e1;color:#a40e0e}.medium .sev{background:#fff1d6;color:#7a4b00}.low .sev{background:#e8f0fe;color:#174ea6}
.chk{font-weight:600}.n{color:#666;margin-left:auto}.msg{margin:6px 0}code{font-size:12px;color:#555;word-break:break-all}
.crop{position:relative;margin-top:8px;background-repeat:no-repeat;border:1px solid #ddd;max-width:100%}.box{position:absolute;outline:2px solid #e8175d;outline-offset:1px;border-radius:2px}
.cap{font-size:12px;color:#777}
</style>
<h1>ui-audit · ${esc(rep.project)}</h1>
<p>${rep.runs} runs in ${Math.round(rep.ms / 1000)} s · ${rep.findings.length} findings · ${esc(JSON.stringify(rep.options))}</p>
<table><tr><th>check</th><th>high</th><th>medium</th><th>low</th><th>total</th></tr>${summary}</table>
${rep.errors.length ? `<h2>Page errors</h2><ul>${rep.errors.map((e) => `<li>${esc(e.error)} (${e.runs})</li>`).join('')}</ul>` : ''}
${rep.stepErrors.length ? `<h2>Steps that failed</h2><ul>${rep.stepErrors.map((s) => `<li>${esc(s)}</li>`).join('')}</ul>` : ''}
${rep.skipped.length ? `<h2>Skipped</h2><ul>${rep.skipped.map((s) => `<li>${esc(s)}</li>`).join('')}</ul>` : ''}
${rows}`;
  writeFileSync(join(out, 'index.html'), html);
  writeFileSync(join(out, 'report.json'), JSON.stringify(rep, null, 1));
}
