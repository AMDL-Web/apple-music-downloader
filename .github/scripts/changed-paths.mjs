// Tells ci.yml which optional jobs a change needs. It reads the paths a push or
// pull request touched, one per line, from standard input, and writes one
// `<job>=true|false` line per job to the file named by $GITHUB_OUTPUT (standard
// output without it). An empty list means the changed files are unknown (a
// scheduled or manual run, a comparison GitHub could not make), so every job
// runs: a job is skipped only when the paths show it cannot be affected.
//
//   gh api ... --jq '.files[].filename' | node .github/scripts/changed-paths.mjs
import { appendFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

// The paths that make a job run. The guest boot runs for the code that builds
// the QEMU command and what it imports, the pin, and its own files.
export const rules = {
  guestboot: [
    /^internal\/(wrapperhost|wrapper|ext4|sysproc)\//,
    /^go\.(mod|sum)$/,
    /^\.github\/scripts\/wrapper-pin/,
    /^\.github\/actions\/guestboot\//,
    /^\.github\/workflows\/(ci|guestboot|wrapper-pin)\.yml$/,
    /^\.github\/scripts\/changed-paths\./,
  ],
};

// classify returns, per job, whether any of files matches its rules.
export function classify(files) {
  const known = files.map((f) => f.trim()).filter(Boolean);
  return Object.fromEntries(Object.entries(rules).map(([job, res]) => [job, known.length === 0 || known.some((f) => res.some((re) => re.test(f)))]));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const out = Object.entries(classify(input.split('\n'))).map(([job, on]) => `${job}=${on}`).join('\n') + '\n';
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, out);
  process.stdout.write(out);
}
