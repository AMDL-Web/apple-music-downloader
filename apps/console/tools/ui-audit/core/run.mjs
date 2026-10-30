// The runner: every scenario of a project config at every variant (engine ×
// width × theme), each check, crops of what it found, and one JSON result.
// It knows the project only through the config (core/config.mjs).

import { createRequire } from 'node:module';
import { mkdirSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createHash } from 'node:crypto';
import { probeSource } from './probe.mjs';
import { pageConfig } from './config.mjs';
import { readScale, scanCss } from './css-scan.mjs';
import { analyzeFrames, settledAt } from './lib/flicker.mjs';

/** playwright-core from $PLAYWRIGHT_CORE (a package directory) or the normal resolution. */
export async function loadPlaywright() {
  const req = createRequire(import.meta.url);
  const where = process.env.PLAYWRIGHT_CORE;
  try {
    return req(where || 'playwright-core');
  } catch (e) {
    throw new Error(`playwright-core not found (${where ? `PLAYWRIGHT_CORE=${where}` : 'set PLAYWRIGHT_CORE to its package directory'}): ${e.message}`);
  }
}

const SEVERITY = { high: 0, medium: 1, low: 2, info: 3 };
const short = (s) => createHash('sha1').update(s).digest('hex').slice(0, 10);

export function variantsOf(cfg, opts) {
  const out = [];
  for (const engine of opts.engines) for (const width of opts.widths || cfg.variants.widths) for (const theme of opts.themes || cfg.variants.themes) out.push({ engine, width, theme, id: `${engine}-${width}-${theme}` });
  return out;
}

function heightFor(cfg, width) {
  const h = cfg.variants.heights;
  return width < cfg.variants.phoneBelow ? h.phone : width < 1024 ? h.tablet : h.desktop;
}

export async function audit(cfg, opts) {
  const pw = await loadPlaywright();
  const out = opts.out;
  mkdirSync(join(out, 'shots'), { recursive: true });
  mkdirSync(join(out, 'frames'), { recursive: true });
  const t0 = Date.now();
  const scale = readScale(cfg.css.tokens, cfg.css.patterns);
  const pcfg = pageConfig(cfg, scale);
  const result = {
    project: cfg.name,
    startedAt: new Date(t0).toISOString(),
    options: { engines: opts.engines, widths: opts.widths || cfg.variants.widths, themes: opts.themes || cfg.variants.themes, only: opts.only || null, states: opts.states },
    scale,
    static: scanCss(cfg.css.files, { root: cfg.root, scale, allowValues: cfg.css.allowValues, skip: cfg.css.skip }),
    runs: [],
    timings: {},
  };
  const app = await cfg.start();
  const scenarios = cfg.scenarios.filter((s) => !opts.only || opts.only.some((o) => s.id === o || s.id.startsWith(`${o}/`) || s.id.startsWith(`${o}-`)));
  try {
    for (const engine of opts.engines) {
      const browser = engine === 'chrome' ? await pw.chromium.launch({ channel: 'chrome', headless: true }) : await pw.webkit.launch({ headless: true });
      try {
        for (const v of variantsOf(cfg, { ...opts, engines: [engine] })) {
          const vt = Date.now();
          const phone = v.width < cfg.variants.phoneBelow;
          const context = await browser.newContext({
            viewport: { width: v.width, height: heightFor(cfg, v.width) },
            deviceScaleFactor: opts.dpr || 1,
            colorScheme: v.theme === 'dark' ? 'dark' : 'light',
            hasTouch: phone && engine === 'chrome',
            reducedMotion: 'no-preference',
          });
          await context.addInitScript({ content: `${probeSource()}; window.__uia.bootRecord(${JSON.stringify({ skeleton: cfg.selectors.skeleton })});` });
          if (cfg.initScript) await context.addInitScript({ content: cfg.initScript(v) });
          if (cfg.routes) await cfg.routes(context, v);
          const page = await context.newPage();
          // A step that cannot find its control fails fast and is reported, not waited on.
          page.setDefaultTimeout(4000);
          const errors = [];
          page.on('pageerror', (e) => errors.push(String(e && e.message)));
          page.on('console', (m) => m.type() === 'error' && errors.push(m.text()));
          for (const sc of scenarios) {
            if (sc.widths && !sc.widths.includes(v.width)) continue;
            if (sc.themes && !sc.themes.includes(v.theme)) continue;
            if (sc.engines && !sc.engines.includes(engine)) continue;
            errors.length = 0;
            const run = await runScenario({ cfg, app, page, context, sc, v, pcfg, out, opts, phone, engine });
            run.errors = [...new Set(errors)].slice(0, 10);
            result.runs.push(run);
            if (opts.log) opts.log(`${v.id} ${sc.id}: ${run.skipped ? `skipped (${run.skipped})` : `${run.findings.length} findings, ${run.ms} ms`}`);
          }
          await context.close();
          result.timings[v.id] = Date.now() - vt;
        }
      } finally {
        await browser.close();
      }
    }
  } finally {
    await app.stop();
  }
  result.ms = Date.now() - t0;
  writeFileSync(join(out, 'result.json'), JSON.stringify(result, null, 1));
  return result;
}

async function settle(page, ms) {
  return page.evaluate((o) => window.__uia.settle(o), { quietMs: ms || 350, timeoutMs: 8000 });
}

async function runScenario({ cfg, app, page, context, sc, v, pcfg, out, opts, phone, engine }) {
  const st = Date.now();
  const run = { scenario: sc.id, title: sc.title || sc.id, variant: v.id, engine, width: v.width, theme: v.theme, findings: [], transitions: [], metrics: {} };
  await page.goto('about:blank');
  await app.control.prepare(sc, v);
  const url = app.baseUrl + (sc.path || '');
  // Stored preferences go in before the measured load, and come out after.
  if (sc.storage) {
    await page.goto(app.baseUrl, { waitUntil: 'domcontentloaded' });
    await page.evaluate((st) => Object.entries(st).forEach(([k, val]) => localStorage.setItem(k, val)), sc.storage);
    await page.goto('about:blank');
  }
  await page.goto(url, { waitUntil: 'load' });
  let s = await settle(page);
  run.metrics.loadMs = s.ms;
  const ctx = makeCtx({ app, page, v, sc, pcfg });
  if (sc.require) {
    const ok = await page.locator(sc.require).count();
    if (!ok) {
      run.skipped = `missing ${sc.require}`;
      return run;
    }
  }
  // First paint: theme flash and loader flashes from navigation start.
  const boot = await page.evaluate(() => window.__uia._boot || []);
  const loadShifts = await page.evaluate(() => window.__uia.loadShifts());
  if (!sc.setup) {
    for (const f of analyzeFrames(boot, { skeletonMinMs: cfg.thresholds.skeletonMinMs, latencyMs: sc.latency ?? cfg.defaultLatency ?? 0 })) {
      if (f.kind === 'blank' || f.kind === 'focus-lost' || f.kind === 'scroll-jump') continue;
      run.findings.push({ check: 'flicker', sel: 'page load', sig: `load:${f.kind}`, msg: `on load: ${f.kind} — ${f.detail}`, data: f, severity: f.kind === 'theme-flash' ? 'high' : 'medium' });
    }
    if (loadShifts && loadShifts.length) {
      const cls = loadShifts.reduce((a, e) => a + e.value, 0);
      run.metrics.loadCls = +cls.toFixed(4);
      if (cls > 0.005) run.findings.push({ check: 'layout-shift', sel: 'page load', sig: 'load:cls', msg: `CLS ${cls.toFixed(4)} while the page loads`, data: { cls, sources: [...new Set(loadShifts.flatMap((e) => e.sources))].slice(0, 6) }, severity: cls > 0.05 ? 'high' : cls > 0.02 ? 'medium' : 'low' });
    }
  }
  if (sc.setup) {
    try {
      await sc.setup(ctx);
    } catch (e) {
      run.setupError = String(e.message || e).split('\n')[0];
    }
    s = await settle(page);
  }
  const scan = await page.evaluate((c) => window.__uia.scan(c), pcfg);
  run.findings.push(...scan.findings);
  run.metrics = { ...run.metrics, ...scan.metrics };
  run.components = scan.components;
  run.rhythm = scan.rhythm;
  const statesHere = opts.states && engine === 'chrome' && cfg.variants.stateWidths.includes(v.width) && !sc.noStates;
  if (statesHere) run.findings.push(...(await statePass(page, context, pcfg)));
  if (cfg.variants.stateWidths.includes(v.width) && phone && !sc.noStates) run.findings.push(...(await page.evaluate((c) => window.__uia.touchTargets(c), pcfg)));
  if (v.width === cfg.variants.stateWidths[0] && !sc.noStates) run.findings.push(...(await page.evaluate((c) => window.__uia.modifierClicks(c), pcfg)));
  await page.evaluate(() => window.scrollTo(0, 0));
  const shot = join('shots', `${short(run.variant + run.scenario)}.png`);
  await page.screenshot({ path: join(out, shot), fullPage: true });
  run.shot = shot;
  await crops(page, run.findings, run, out, 24);
  for (const tr of sc.transitions || []) {
    const t = await runTransition({ cfg, app, page, context, sc, tr, v, pcfg, out, ctx, engine });
    run.transitions.push(t);
    await crops(page, t.findings, run, out, 6);
    run.findings.push(...t.findings);
  }
  // A transition may store a preference (the theme toggle does): the next scenario starts clean.
  await page.evaluate(() => {
    try {
      localStorage.clear();
      sessionStorage.clear();
    } catch {
      // about:blank after a failed load has no storage.
    }
  });
  run.ms = Date.now() - st;
  return run;
}

function makeCtx({ app, page, v, sc, pcfg }) {
  return {
    page,
    variant: v,
    scenario: sc,
    control: app.control,
    settle: (ms) => settle(page, ms),
    /** Same-document navigation, as an in-app link does it. */
    hash: (h) => page.evaluate((x) => (location.hash = x), h),
    wait: (ms) => page.waitForTimeout(ms),
  };
}

async function statePass(page, context, pcfg) {
  const out = [];
  const cdp = await context.newCDPSession(page);
  try {
    await cdp.send('DOM.enable');
    await cdp.send('CSS.enable');
    await cdp.send('Animation.enable');
    await cdp.send('Animation.setPlaybackRate', { playbackRate: 1000 });
    await cdp.send('DOM.getDocument', { depth: 0 });
    const targets = await page.evaluate((c) => window.__uia.stateTargets(c), pcfg);
    for (const t of targets) {
      const rest = await page.evaluate((i) => window.__uia.restPaint(i), t.i);
      const { result } = await cdp.send('Runtime.evaluate', { expression: `window.__uia.targets[${t.i}]` });
      if (!result.objectId) continue;
      const { nodeId } = await cdp.send('DOM.requestNode', { objectId: result.objectId });
      const ancestors = [];
      for (let k = 1; k <= 3; k++) {
        const { result: ar } = await cdp.send('Runtime.evaluate', { expression: `window.__uia.ancestor(${t.i}, ${k})` });
        if (!ar.objectId) break;
        ancestors.push((await cdp.send('DOM.requestNode', { objectId: ar.objectId })).nodeId);
      }
      for (const state of ['hover', 'focus-visible', 'active']) {
        await cdp.send('CSS.forcePseudoState', { nodeId, forcedPseudoClasses: state === 'focus-visible' ? ['focus', 'focus-visible'] : state === 'active' ? ['hover', 'active'] : [state] });
        for (const a of ancestors) await cdp.send('CSS.forcePseudoState', { nodeId: a, forcedPseudoClasses: state === 'focus-visible' ? ['focus-within'] : state === 'hover' || state === 'active' ? ['hover'] : [] });
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        out.push(...(await page.evaluate(([i, s, r, c]) => window.__uia.stateCheck(i, s, r, c), [t.i, state, rest, pcfg])));
      }
      await cdp.send('CSS.forcePseudoState', { nodeId, forcedPseudoClasses: [] });
      for (const a of ancestors) await cdp.send('CSS.forcePseudoState', { nodeId: a, forcedPseudoClasses: [] });
    }
  } finally {
    await cdp.send('Animation.setPlaybackRate', { playbackRate: 1 }).catch(() => {});
    await cdp.detach().catch(() => {});
  }
  return out;
}

async function runTransition({ cfg, app, page, context, sc, tr, v, pcfg, out, ctx, engine }) {
  const res = { id: tr.id, title: tr.title || tr.id, findings: [], frames: null };
  if (tr.before) {
    try {
      await tr.before(ctx);
    } catch (e) {
      res.error = String(e.message || e).split('\n')[0];
    }
    await settle(page);
  }
  await page.evaluate((c) => {
    window.__uia.shiftStart(c);
    window.__uia.framesStart(c);
    window.__uia.tickStart(c);
  }, pcfg);
  let cast = null;
  if (engine === 'chrome') cast = await startCast(context, page);
  const t0 = Date.now();
  try {
    await tr.run(ctx);
  } catch (e) {
    res.error = String(e.message || e).split('\n')[0];
  }
  await settle(page, tr.quietMs);
  if (tr.hold) await page.waitForTimeout(tr.hold);
  const ms = Date.now() - t0;
  const frames = await page.evaluate(() => window.__uia.framesStop());
  const shift = await page.evaluate((c) => window.__uia.shiftStop(c), pcfg);
  const ticks = await page.evaluate(() => window.__uia.tickStop());
  const shots = cast ? await cast.stop() : [];
  res.ms = ms;
  res.cls = shift && shift.cls;
  res.settleMs = Math.round(settledAt(frames));
  const label = `${tr.title || tr.id}`;
  if (shift) {
    // A transition meant to change content (a filter, a search) scores its CLS, but low: the elements that moved are judged one by one.
    if (shift.cls.total > 0.001) res.findings.push({ check: 'layout-shift', sel: label, sig: `cls:${sc.id}:${tr.id}`, msg: `CLS ${shift.cls.total.toFixed(4)} during "${label}"`, data: { cls: shift.cls, sources: [...new Set(shift.entries.flatMap((e) => e.sources.map((s) => s.sel)))].slice(0, 6) }, severity: tr.expectContent ? 'low' : shift.cls.total > 0.05 ? 'high' : shift.cls.total > 0.01 ? 'medium' : 'low' });
    const allowMove = tr.allowMove || [];
    for (const m of shift.moved) {
      if (allowMove.some((q) => m.sel.includes(q))) continue;
      const d = Math.max(Math.abs(m.dy), Math.abs(m.dx));
      if (d < 1.5) continue;
      // A transition that is meant to change content (expectContent) moves what follows the change: low.
      const content = m.causeKind === 'inserted' || m.causeKind === 'removed' || (tr.expectContent && m.causeKind === 'grew' && m.dy >= 0);
      res.findings.push({ check: 'layout-shift', sel: m.sel, sig: m.sig, rect: m.rect, msg: `moved ${m.dy ? `${m.dy > 0 ? 'down' : 'up'} ${Math.abs(m.dy)}px` : ''}${m.dy && m.dx ? ', ' : ''}${m.dx ? `${m.dx > 0 ? 'right' : 'left'} ${Math.abs(m.dx)}px` : ''} during "${label}"${m.cause ? ` (${m.cause})` : ''}`, data: m, severity: content ? 'low' : d >= 8 ? 'high' : d >= 3 ? 'medium' : 'low' });
    }
  }
  for (const f of analyzeFrames(frames, { skeletonMinMs: cfg.thresholds.skeletonMinMs, latencyMs: tr.latency ?? sc.latency ?? cfg.defaultLatency ?? 0, expectScroll: tr.expectScroll ?? null, expectThemeChange: !!tr.expectThemeChange, ignoreFocus: !!tr.ignoreFocus })) {
    res.findings.push({ check: 'flicker', sel: label, sig: `flicker:${f.kind}:${sc.id}:${tr.id}`, msg: `${f.kind} during "${label}": ${f.detail || ''}${f.ms !== undefined ? ` (${f.ms} ms)` : ''}`, data: f, severity: f.kind === 'blank' || f.kind === 'theme-flash' || f.kind === 'focus-lost' ? 'high' : 'medium' });
  }
  res.findings.push(...ticks);
  if (shots.length && res.findings.some((f) => f.check === 'flicker' || f.severity === 'high') ) {
    const dir = join('frames', short(`${v.id}${sc.id}${tr.id}`));
    mkdirSync(join(out, dir), { recursive: true });
    const keep = pick(shots, 16);
    res.frames = keep.map((s, i) => {
      const file = join(dir, `${String(i).padStart(2, '0')}.jpg`);
      writeFileSync(join(out, file), Buffer.from(s.data, 'base64'));
      return { file, t: Math.round(s.t) };
    });
  }
  res.framesCount = frames.length;
  if (tr.after) await tr.after(ctx);
  return res;
}

function pick(list, n) {
  if (list.length <= n) return list;
  const out = [];
  for (let i = 0; i < n; i++) out.push(list[Math.round((i * (list.length - 1)) / (n - 1))]);
  return out;
}

async function startCast(context, page) {
  const cdp = await context.newCDPSession(page);
  const shots = [];
  let t0 = null;
  cdp.on('Page.screencastFrame', (f) => {
    const t = f.metadata.timestamp * 1000;
    if (t0 === null) t0 = t;
    shots.push({ data: f.data, t: t - t0 });
    cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {});
  });
  await cdp.send('Page.startScreencast', { format: 'jpeg', quality: 60, maxWidth: 720, maxHeight: 900, everyNthFrame: 1 });
  return {
    async stop() {
      await cdp.send('Page.stopScreencast').catch(() => {});
      await cdp.detach().catch(() => {});
      return shots;
    },
  };
}

/** One crop per finding with a box, the box drawn later by the report. */
async function crops(page, findings, run, out, budget) {
  const size = await page.evaluate(() => ({ w: document.documentElement.scrollWidth, h: document.documentElement.scrollHeight }));
  const withRect = findings.filter((f) => f.rect && f.rect.width > 0 && !f.crop).sort((a, b) => SEVERITY[a.severity] - SEVERITY[b.severity]);
  const done = new Map();
  let n = 0;
  for (const f of withRect) {
    const key = `${f.sig}|${f.check}`;
    if (done.has(key)) {
      f.crop = done.get(key);
      continue;
    }
    if (n >= budget) break;
    const pad = 28;
    const x = Math.max(0, Math.floor(f.rect.left - pad));
    const y = Math.max(0, Math.floor(f.rect.top - pad));
    const w = Math.min(size.w - x, Math.ceil(Math.max(f.rect.width + 2 * pad, 160)), 900);
    const h = Math.min(size.h - y, Math.ceil(Math.max(f.rect.height + 2 * pad, 80)), 600);
    if (w <= 0 || h <= 0) continue;
    const file = join('shots', `${short(run.variant + run.scenario + key)}.png`);
    try {
      await page.screenshot({ path: join(out, file), clip: { x, y, width: w, height: h }, fullPage: true });
      f.crop = { file, x, y, w, h, box: { x: f.rect.left - x, y: f.rect.top - y, w: f.rect.width, h: f.rect.height } };
      done.set(key, f.crop);
      n++;
    } catch {
      // A clip outside the page after a transition changed it: no crop.
    }
  }
}
