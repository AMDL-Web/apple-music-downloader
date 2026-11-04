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

// What a change to only these needs no Go build, vet, test, race, release build,
// image or upgrade check: documentation and rules (the repository checks in
// tools/repocheck still run), the website (its Node tests run) and the web
// console (its Go and Node tests run). Markdown is code only where it is
// embedded (apps/amdl/skill), so the list names the documents, not *.md.
export const light = [
  /^docs\//,
  /^\.github\/release-notes\//,
  /^\.claude\//,
  /^apps\/(site|console)\//,
  /^(README|README\.zh-CN|CONTRIBUTING|GLOSSARY|HUMAN|REVIEW)\.md$/,
  /^LICENSE$/,
  /(^|\/)(AGENTS|CLAUDE)\.md$/,
];

// The jobs. `any` runs a job when a changed path matches one of its patterns;
// `unless` when a changed path matches none (so a change to only light files
// skips it). The guest boot runs for the code that builds the QEMU command and
// every internal package it imports (tools/repocheck checks the list against
// `go list -deps`), the pin, and its own files. The console audit runs for the
// console, the API spec it is written against (the mock backend answers from
// it), and its own files; apps/console includes the audit tool.
export const rules = {
  go: { unless: light },
  guestboot: {
    any: [
      /^internal\/(wrapperhost|client|config|domain|ext4|fairplay|limits|sysproc|wrapper)\//,
      /^go\.(mod|sum)$/,
      /^\.github\/scripts\/wrapper-pin/,
      /^\.github\/actions\/guestboot\//,
      /^\.github\/workflows\/(ci|guestboot|wrapper-pin)\.yml$/,
      /^\.github\/scripts\/changed-paths\./,
    ],
  },
  uiaudit: {
    any: [
      /^apps\/console\//,
      /^internal\/api\/openapi\.yaml$/,
      /^\.github\/workflows\/ci\.yml$/,
      /^\.github\/scripts\/changed-paths\./,
    ],
  },
};

// classify returns, per job, whether any of files matches its rules.
export function classify(files) {
  const known = files.map((f) => f.trim()).filter(Boolean);
  const hit = (res) => (f) => res.some((re) => re.test(f));
  return Object.fromEntries(Object.entries(rules).map(([job, rule]) => [job,
    known.length === 0 || (rule.any ? known.some(hit(rule.any)) : known.some((f) => !hit(rule.unless)(f)))]));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  let input = '';
  for await (const chunk of process.stdin) input += chunk;
  const out = Object.entries(classify(input.split('\n'))).map(([job, on]) => `${job}=${on}`).join('\n') + '\n';
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, out);
  process.stdout.write(out);
}
