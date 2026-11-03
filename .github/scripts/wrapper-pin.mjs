// Finds the manager guest of the newest successful manager run on the lite
// branch of AMDL-Web's wrapper fork (internal/wrapperhost#GuestRepo) and the
// wrapper-lite-linux-x86_64 payload of the newest successful build-lite run
// there (the payload the guest's manager installs), as that repository's
// publish workflow put them up as release assets (guest-<commit12>,
// lite-<commit12>), downloads both, and writes them as the pin amdl wrapper
// install reads (internal/wrapperhost#Pin) once each sha256 matches the digest
// GitHub reports. The wrapper-pin workflow runs it weekly and commits the file
// to the wrapper-pins branch, so installs follow new guests between amdl
// releases.
//
//   node .github/scripts/wrapper-pin.mjs <out.json>
import { createHash } from 'node:crypto';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const repo = 'AMDL-Web/wrapper';
const api = `https://api.github.com/repos/${repo}`;

// tagOf is the release a run's artifact is published under.
export const tagOf = (prefix, run) => `${prefix}-${run.head_sha.slice(0, 12)}`;

// pickAsset chooses the asset called name.zip to pin from the release of a run
// of workflow: the run must have succeeded on lite.
function pickAsset(workflow, prefix, name, run, release) {
  if (run.head_branch !== 'lite' || run.conclusion !== 'success' || !/^[0-9a-f]{40}$/.test(run.head_sha ?? '')) {
    throw new Error(`expected a successful ${workflow} run on lite with a commit`);
  }
  if (release?.tag_name !== tagOf(prefix, run)) throw new Error(`run ${run.id} has no ${tagOf(prefix, run)} release`);
  const a = release.assets?.find((x) => x.name === `${name}.zip`);
  if (!a) throw new Error(`release ${release.tag_name} has no ${name}.zip`);
  const digest = /^sha256:([0-9a-f]{64})$/.exec(a.digest ?? '');
  if (!digest) throw new Error(`asset ${a.name} of ${release.tag_name} has no sha256 digest`);
  return { artifact: name, commit: run.head_sha, sha256: digest[1], size: a.size };
}

// pick chooses the manager guest to pin from a manager run and its release.
export const pick = (run, release) => pickAsset('manager', 'guest', 'manager-qemu-guest', run, release);

// pickLite chooses the wrapper-lite payload to pin from a build-lite run and
// its release. The guest is x86_64 whatever the host, so only that one is
// pinned.
export const pickLite = (run, release) => pickAsset('build-lite', 'lite', 'wrapper-lite-linux-x86_64', run, release);

// pinFrom combines the two picks into the file amdl reads.
export function pinFrom(managerRun, managerRelease, liteRun, liteRelease) {
  return { ...pick(managerRun, managerRelease), lite: pickLite(liteRun, liteRelease) };
}

// urlOf is where amdl downloads a pinned asset (internal/wrapperhost#Asset).
export const urlOf = (prefix, p) => `https://github.com/${repo}/releases/download/${prefix}-${p.commit.slice(0, 12)}/${p.artifact}.zip`;

async function json(url) {
  const headers = { Accept: 'application/vnd.github+json' };
  if (process.env.GH_TOKEN) headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
  const r = await fetch(url, { headers, signal: AbortSignal.timeout(60_000) });
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return r.json();
}

// sha256Of downloads url and hashes it without keeping it.
export async function sha256Of(url, timeoutMs = 10 * 60_000) {
  const r = await fetch(url, { signal: AbortSignal.timeout(timeoutMs) });
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  const h = createHash('sha256');
  let n = 0;
  for await (const chunk of r.body) {
    h.update(chunk);
    n += chunk.length;
  }
  return { sha256: h.digest('hex'), size: n };
}

async function newestSuccess(workflow, prefix) {
  const runs = await json(`${api}/actions/workflows/${workflow}/runs?branch=lite&event=push&status=success&per_page=1`);
  const run = runs.workflow_runs?.[0];
  if (!run) throw new Error(`no successful ${workflow} run on lite`);
  return { run, release: await json(`${api}/releases/tags/${tagOf(prefix, run)}`) };
}

async function verified(what, url, want) {
  const got = await sha256Of(url);
  if (got.sha256 !== want.sha256 || got.size !== want.size) {
    throw new Error(`${what}: downloaded ${got.size} bytes with sha256 ${got.sha256}, GitHub says ${want.size} with ${want.sha256}`);
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const out = process.argv[2];
  if (!out) {
    console.error('usage: node .github/scripts/wrapper-pin.mjs <out.json>');
    process.exit(2);
  }
  const m = await newestSuccess('manager.yml', 'guest');
  const l = await newestSuccess('build-lite.yml', 'lite');
  const pin = pinFrom(m.run, m.release, l.run, l.release);
  await verified(`guest ${pin.commit.slice(0, 12)}`, urlOf('guest', pin), pin);
  await verified(`wrapper-lite ${pin.lite.commit.slice(0, 12)}`, urlOf('lite', pin.lite), pin.lite);
  writeFileSync(out, JSON.stringify(pin, null, 2) + '\n');
  console.log(`pinned guest ${pin.commit.slice(0, 12)} and wrapper-lite ${pin.lite.commit.slice(0, 12)}`);
}
