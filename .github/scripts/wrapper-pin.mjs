// Finds the manager-qemu-guest artifact of the newest successful manager run on
// the lite branch of AMDL-Web's wrapper fork (internal/wrapperhost#GuestRepo)
// and the wrapper-lite-linux-x86_64 artifact of the newest successful
// build-lite run there (the payload the guest's manager installs), downloads
// both, and writes them as the pin amdl wrapper install reads
// (internal/wrapperhost#Pin) once each sha256 matches the digest GitHub
// reports. The wrapper-pin workflow runs it weekly and commits the file to the
// wrapper-pins branch, so installs keep working after the pin a release
// carries has expired.
//
//   node .github/scripts/wrapper-pin.mjs <out.json>
import { createHash } from 'node:crypto';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const repo = 'AMDL-Web/wrapper';
const api = `https://api.github.com/repos/${repo}`;

// pick chooses the artifact called name to pin from a run of workflow and its
// artifacts listing: the run must have succeeded on lite.
function pickArtifact(workflow, name, run, artifacts) {
  if (run.head_branch !== 'lite' || run.conclusion !== 'success' || !Number.isSafeInteger(run.id) || run.id <= 0 || !run.head_sha) {
    throw new Error(`expected a successful ${workflow} run on lite with a pinned id and commit`);
  }
  const a = artifacts.find((x) => x.name === name && !x.expired);
  if (!a) throw new Error(`run ${run.id} has no live ${name} artifact`);
  const digest = /^sha256:([0-9a-f]{64})$/.exec(a.digest ?? '');
  if (!digest) throw new Error(`artifact ${a.id} has no sha256 digest`);
  return { a, sha256: digest[1] };
}

// pick chooses the manager guest to pin from a manager run.
export function pick(run, artifacts) {
  const { a, sha256 } = pickArtifact('manager', 'manager-qemu-guest', run, artifacts);
  return { artifact: 'manager-qemu-guest', commit: run.head_sha, run_id: run.id, artifact_id: a.id, sha256, size: a.size_in_bytes, expires_at: a.expires_at };
}

// pickLite chooses the wrapper-lite payload to pin from a build-lite run. The
// guest is x86_64 whatever the host, so only that artifact is pinned.
export function pickLite(run, artifacts) {
  const { a, sha256 } = pickArtifact('build-lite', 'wrapper-lite-linux-x86_64', run, artifacts);
  return { artifact: a.name, commit: run.head_sha, run_id: run.id, sha256, size: a.size_in_bytes, expires_at: a.expires_at };
}

// pinFrom combines the two picks into the file amdl reads.
export function pinFrom(managerRun, managerArtifacts, liteRun, liteArtifacts) {
  return { ...pick(managerRun, managerArtifacts), lite: pickLite(liteRun, liteArtifacts) };
}

// liteURL is the URL the guest's manager downloads the payload from
// (internal/wrapperhost#LitePin.URL).
export const liteURL = (lite) => `https://nightly.link/${repo}/actions/runs/${lite.run_id}/${lite.artifact}.zip`;

async function json(url) {
  const headers = { Accept: 'application/vnd.github+json' };
  if (process.env.GH_TOKEN) headers.Authorization = `Bearer ${process.env.GH_TOKEN}`;
  const r = await fetch(url, { headers, signal: AbortSignal.timeout(60_000) });
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return r.json();
}

// sha256Of downloads url and hashes it without keeping it.
export async function sha256Of(url, timeoutMs = 30 * 60_000) {
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

async function newestSuccess(workflow) {
  const runs = await json(`${api}/actions/workflows/${workflow}/runs?branch=lite&status=success&per_page=1`);
  const run = runs.workflow_runs?.[0];
  if (!run) throw new Error(`no successful ${workflow} run on lite`);
  return { run, artifacts: (await json(`${api}/actions/runs/${run.id}/artifacts?per_page=100`)).artifacts };
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
  const m = await newestSuccess('manager.yml');
  const l = await newestSuccess('build-lite.yml');
  const pin = pinFrom(m.run, m.artifacts, l.run, l.artifacts);
  await verified(`guest artifact ${pin.artifact_id}`, `https://nightly.link/${repo}/actions/artifacts/${pin.artifact_id}.zip`, pin);
  await verified(`wrapper-lite run ${pin.lite.run_id}`, liteURL(pin.lite), pin.lite);
  writeFileSync(out, JSON.stringify(pin, null, 2) + '\n');
  console.log(`pinned guest artifact ${pin.artifact_id} of run ${pin.run_id} (${pin.commit.slice(0, 12)}), expires ${pin.expires_at}; ` +
    `wrapper-lite run ${pin.lite.run_id} (${pin.lite.commit.slice(0, 12)}), expires ${pin.lite.expires_at}`);
}
