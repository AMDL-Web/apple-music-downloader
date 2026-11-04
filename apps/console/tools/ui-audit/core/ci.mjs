// What a CI run needs on top of `run`: the shards of one matrix merged back into
// one result (the consistency and rhythm checks compare across widths and
// themes, so they must see every run), and a job summary with GitHub warnings.

/** One result from the results of several shards; the first one's scale and CSS scan stand for all. */
export function mergeResults(results) {
  if (!results.length) throw new Error('nothing to merge');
  const [first] = results;
  const union = (key) => [...new Set(results.flatMap((r) => r.options[key] || []))];
  return {
    ...first,
    startedAt: results.map((r) => r.startedAt).sort()[0],
    options: { ...first.options, widths: union('widths').sort((a, b) => b - a), themes: union('themes') },
    runs: results.flatMap((r) => r.runs),
    timings: results.reduce((sum, r) => {
      for (const [k, ms] of Object.entries(r.timings)) sum[k] = (sum[k] || 0) + ms;
      return sum;
    }, {}),
    // Shards run side by side: the slowest one is the wall time.
    ms: Math.max(...results.map((r) => r.ms)),
  };
}

/**
 * A markdown summary of a report and the GitHub annotation lines for it. A run
 * is only a warning: `warn` says whether anything is over the threshold (more
 * than maxHigh high findings, a step that failed, or a page error).
 */
export function summarize(rep, { maxHigh = 0, artifact = '', retention = 0 } = {}) {
  const high = rep.findings.filter((f) => f.severity === 'high');
  const warn = high.length > maxHigh || rep.stepErrors.length > 0 || rep.errors.length > 0;
  const lines = [];
  lines.push(`## Console ui-audit: ${rep.findings.length} findings, ${high.length} high`);
  lines.push('');
  lines.push(`${rep.runs} runs (${Object.keys(rep.options).length ? `widths ${rep.options.widths.join(', ')}; themes ${rep.options.themes.join(', ')}` : ''}) in ${Math.round(rep.ms / 1000)} s of wall time.`);
  if (artifact) lines.push(`Screenshots and the report: artifact \`${artifact}\`${retention ? `, kept ${retention} days` : ''}; open \`index.html\` in it.`);
  lines.push('');
  lines.push('| check | high | medium | low |');
  lines.push('| --- | ---: | ---: | ---: |');
  for (const [c, n] of Object.entries(rep.byCheck)) lines.push(`| ${c} | ${n.high} | ${n.medium} | ${n.low} |`);
  if (high.length) {
    lines.push('', '### High findings', '');
    for (const f of high.slice(0, 25)) lines.push(`- **${f.check}** (${f.occurrences.length}×) ${f.msg} — \`${f.sel}\``);
    if (high.length > 25) lines.push(`- … and ${high.length - 25} more in the report`);
  }
  const shifts = rep.cls.filter((c) => c.cls >= 0.1).sort((a, b) => b.cls - a.cls);
  if (shifts.length) {
    lines.push('', '### Layout shift of 0.1 or more', '');
    for (const c of shifts.slice(0, 15)) lines.push(`- ${c.scenario} ${c.variant} ${c.what}: ${c.cls}`);
  }
  if (rep.stepErrors.length) lines.push('', '### Steps that failed', '', ...rep.stepErrors.slice(0, 15).map((s) => `- ${s}`));
  if (rep.errors.length) lines.push('', '### Page errors', '', ...rep.errors.slice(0, 10).map((e) => `- ${e.error} (${e.runs} runs)`));
  const annotations = [];
  if (high.length > maxHigh) annotations.push(`::warning title=ui-audit::${high.length} high findings (limit ${maxHigh}); see the job summary and the artifact`);
  if (rep.stepErrors.length) annotations.push(`::warning title=ui-audit::${rep.stepErrors.length} audit steps failed; see the job summary`);
  if (rep.errors.length) annotations.push(`::warning title=ui-audit::${rep.errors.length} distinct page errors; see the job summary`);
  return { warn, markdown: `${lines.join('\n')}\n`, annotations };
}
