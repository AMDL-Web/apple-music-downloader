// Runs install.sh against a fake release on disk, and lints it with
// shellcheck and install.ps1 with pwsh when those are installed (CI has both).
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const script = join(here, 'install.sh');
const version = 'v9.9.9';
const name = `amdl_${version}_linux_amd64`;

// A stand-in amdl: it answers `version` like the real one and records every
// other command line in calls.log beside itself.
const fakeAmdl = (v) => `#!/bin/sh
if [ "$1" = version ]; then printf '{"client":{"build_date":"","commit":"","version":"${v}"}}\\n'; exit 0; fi
echo "$*" >> "$(dirname "$0")/calls.log"
if [ "$1 $2" = "service status" ]; then echo '{"installed":false}'; fi
`;

function release(v = version) {
  const dir = mkdtempSync(join(tmpdir(), 'amdl-release-'));
  mkdirSync(join(dir, name));
  writeFileSync(join(dir, name, 'amdl'), fakeAmdl(v));
  chmodSync(join(dir, name, 'amdl'), 0o755);
  const tar = spawnSync('tar', ['-czf', `${name}.tar.gz`, name], { cwd: dir });
  assert.equal(tar.status, 0, String(tar.stderr));
  const sum = createHash('sha256').update(readFileSync(join(dir, `${name}.tar.gz`))).digest('hex');
  writeFileSync(join(dir, 'SHA256SUMS'), `${sum}  ${name}.tar.gz\n`);
  return dir;
}

function install(rel, ...args) {
  const home = mkdtempSync(join(tmpdir(), 'amdl-home-'));
  const run = (...more) => spawnSync('sh', [script, '--version', version, '--bin-dir', join(home, 'bin'), ...more], {
    encoding: 'utf8',
    env: { ...process.env, HOME: home, AMDL_INSTALL_BASE_URL: `file://${rel}`, AMDL_INSTALL_OS: 'Linux', AMDL_INSTALL_ARCH: 'x86_64' },
  });
  return { home, bin: join(home, 'bin', 'amdl'), first: run(...args), run };
}

test('installs the verified binary and skips the same version next time', () => {
  const { bin, first, run } = install(release());
  assert.equal(first.status, 0, first.stderr);
  assert.match(readFileSync(bin, 'utf8'), /"version":"v9.9.9"/);
  const again = run();
  assert.equal(again.status, 0, again.stderr);
  assert.match(again.stderr, /already installed/);
  assert.match(run('--force').stderr, /installed .*\(v9.9.9\)/);
});

test('refuses an archive whose checksum does not match', () => {
  const rel = release();
  writeFileSync(join(rel, 'SHA256SUMS'), `${'0'.repeat(64)}  ${name}.tar.gz\n`);
  const { bin, first } = install(rel);
  assert.notEqual(first.status, 0);
  assert.match(first.stderr, /checksum mismatch/);
  assert.equal(existsSync(bin), false);
});

test('--service registers the service with the wrapper address', () => {
  const { bin, first } = install(release(), '--service', '--wrapper', 'http://nas:30020');
  assert.equal(first.status, 0, first.stderr);
  assert.match(readFileSync(join(dirname(bin), 'calls.log'), 'utf8'), /^service install --wrapper http:\/\/nas:30020$/m);
});

test('--dry-run downloads nothing', () => {
  const { bin, first } = install('/nonexistent', '--dry-run', '--service');
  assert.equal(first.status, 0, first.stderr);
  assert.match(first.stderr, /would install amdl v9.9.9 \(linux\/amd64\) from file:\/\/\/nonexistent\/amdl_v9.9.9_linux_amd64.tar.gz/);
  assert.match(first.stderr, /would run: amdl service install/);
  assert.equal(existsSync(bin), false);
});

const has = (cmd) => spawnSync(cmd, ['--version'], { stdio: 'ignore' }).error === undefined;

test('shellcheck finds nothing in install.sh', { skip: !has('shellcheck') && 'shellcheck is not installed' }, () => {
  const r = spawnSync('shellcheck', ['-s', 'sh', script], { encoding: 'utf8' });
  assert.equal(r.status, 0, r.stdout);
});

test('install.ps1 parses and dry-runs', { skip: !has('pwsh') && 'pwsh is not installed' }, () => {
  const ps1 = join(here, 'install.ps1');
  const r = spawnSync('pwsh', ['-NoProfile', '-File', ps1, '-Version', version, '-InstallDir', join(tmpdir(), 'amdl-ps1'),
    '-BaseUrl', '/nonexistent', '-DryRun', '-Service', '-Wrapper', 'http://nas:30020'], { encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stderr, /would install amdl v9.9.9 \(windows\/amd64\)/);
  assert.match(r.stderr, /would run: amdl service install --wrapper http:\/\/nas:30020/);
});
