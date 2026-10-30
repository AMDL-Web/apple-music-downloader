// Finds the qemu-assets artifact of the newest successful build-lite run on
// upstream's lite branch, downloads it, and writes it as the pin amdl wrapper
// install reads (internal/wrapperhost#Pin) once its sha256 matches the digest
// GitHub reports. The wrapper-pin workflow runs it weekly and commits the file
// to the wrapper-pins branch, so installs keep working after the pin a
// release carries has expired.
//
//   node .github/scripts/wrapper-pin.mjs <out.json>
import { createHash } from 'node:crypto';
import { writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

const api = 'https://api.github.com/repos/WorldObservationLog/wrapper';

// pick chooses the artifact to pin from a run and its artifacts listing.
export function pick(run, artifacts) {
  const a = artifacts.find((x) => x.name === 'qemu-assets' && !x.expired);
  if (!a) throw new Error(`run ${run.id} has no live qemu-assets artifact`);
  const digest = /^sha256:([0-9a-f]{64})$/.exec(a.digest ?? '');
  if (!digest) throw new Error(`artifact ${a.id} has no sha256 digest`);
  return { commit: run.head_sha, run_id: run.id, artifact_id: a.id, sha256: digest[1], size: a.size_in_bytes, expires_at: a.expires_at };
}

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

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const out = process.argv[2];
  if (!out) {
    console.error('usage: node .github/scripts/wrapper-pin.mjs <out.json>');
    process.exit(2);
  }
  const runs = await json(`${api}/actions/workflows/build-lite.yml/runs?branch=lite&status=success&per_page=1`);
  const run = runs.workflow_runs?.[0];
  if (!run) throw new Error('no successful build-lite run on lite');
  const pin = pick(run, (await json(`${api}/actions/runs/${run.id}/artifacts?per_page=100`)).artifacts);
  const got = await sha256Of(`https://nightly.link/WorldObservationLog/wrapper/actions/artifacts/${pin.artifact_id}.zip`);
  if (got.sha256 !== pin.sha256 || got.size !== pin.size) {
    throw new Error(`artifact ${pin.artifact_id}: downloaded ${got.size} bytes with sha256 ${got.sha256}, GitHub says ${pin.size} with ${pin.sha256}`);
  }
  writeFileSync(out, JSON.stringify(pin, null, 2) + '\n');
  console.log(`pinned artifact ${pin.artifact_id} of run ${pin.run_id} (${pin.commit.slice(0, 12)}), expires ${pin.expires_at}`);
}
