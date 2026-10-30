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

// A stand-in amdl: it answers `version` like the real one, records every other
// command line in calls.log beside itself, reports the service active while an
// "active" file sits there, and with failStart cannot start it.
const fakeAmdl = (v, failStart) => `#!/bin/sh
d=$(dirname "$0")
if [ "$1" = version ]; then printf '{"client":{"build_date":"","commit":"","version":"${v}"}}\\n'; exit 0; fi
echo "${v} $*" >> "$d/calls.log"
case "$1 $2" in
"service status") if [ -f "$d/active" ]; then echo '{"installed":true,"active":true}'; else echo '{"installed":true,"active":false}'; fi ;;
"service stop") rm -f "$d/active" ;;
"service start") ${failStart ? 'exit 1' : 'touch "$d/active"'} ;;
esac
`;

function release(v = version, failStart = false, rel = mkdtempSync(join(tmpdir(), 'amdl-release-'))) {
  const dir = rel;
  const name = `amdl_${v}_linux_amd64`;
  mkdirSync(join(dir, name));
  writeFileSync(join(dir, name, 'amdl'), fakeAmdl(v, failStart));
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
  run.upgrade = (rel2, v2) => spawnSync('sh', [script, '--version', v2, '--bin-dir', join(home, 'bin')], {
    encoding: 'utf8',
    env: { ...process.env, HOME: home, AMDL_INSTALL_BASE_URL: `file://${rel2}`, AMDL_INSTALL_OS: 'Linux', AMDL_INSTALL_ARCH: 'x86_64' },
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

test('a release without this machine\'s archive points at --version', () => {
  const { bin, first } = install(mkdtempSync(join(tmpdir(), 'amdl-release-')));
  assert.notEqual(first.status, 0);
  assert.match(first.stderr, /could not download amdl_v9\.9\.9_linux_amd64\.tar\.gz/);
  assert.match(first.stderr, /--version v9\.9\.9 names a release that ships linux\/amd64/);
  assert.equal(existsSync(bin), false);
});

test('--service registers the service with the wrapper address', () => {
  const { bin, first } = install(release(), '--service', '--wrapper', 'http://nas:30020');
  assert.equal(first.status, 0, first.stderr);
  assert.match(readFileSync(join(dirname(bin), 'calls.log'), 'utf8'), /^v9\.9\.9 service install --wrapper http:\/\/nas:30020$/m);
});

test('an upgrade restarts a running service on the new binary', () => {
  const { bin, run } = install(release());
  writeFileSync(join(dirname(bin), 'active'), '');
  const up = run.upgrade(release('v9.9.10'), 'v9.9.10');
  assert.equal(up.status, 0, up.stderr);
  const calls = readFileSync(join(dirname(bin), 'calls.log'), 'utf8');
  assert.match(calls, /^v9\.9\.9 service stop$/m);
  assert.match(calls, /^v9\.9\.10 service start$/m);
  assert.equal(existsSync(join(dirname(bin), 'active')), true);
});

test('an upgrade leaves a stopped service stopped', () => {
  const { bin, run } = install(release());
  const up = run.upgrade(release('v9.9.10'), 'v9.9.10');
  assert.equal(up.status, 0, up.stderr);
  assert.doesNotMatch(readFileSync(join(dirname(bin), 'calls.log'), 'utf8'), /service (stop|start)/);
});

test('a new binary that cannot start the service is rolled back', () => {
  const { bin, run } = install(release());
  writeFileSync(join(dirname(bin), 'active'), '');
  const up = run.upgrade(release('v9.9.10', true), 'v9.9.10');
  assert.notEqual(up.status, 0);
  assert.match(up.stderr, /previous binary is back/);
  assert.match(readFileSync(bin, 'utf8'), /"version":"v9.9.9"/);
  assert.match(readFileSync(join(dirname(bin), 'calls.log'), 'utf8'), /^v9\.9\.9 service start$/m);
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

test('install.ps1 points at -Version when the archive is missing', { skip: !has('pwsh') && 'pwsh is not installed' }, () => {
  // pwsh wraps an uncaught error to the console width, so print the message itself.
  const run = `try { & '${join(here, 'install.ps1')}' -Version ${version} -InstallDir '${mkdtempSync(join(tmpdir(), 'amdl-ps1-'))}'` +
    ` -BaseUrl '${mkdtempSync(join(tmpdir(), 'amdl-release-'))}' } catch { [Console]::Error.WriteLine($_.Exception.Message); exit 1 }`;
  const r = spawnSync('pwsh', ['-NoProfile', '-Command', run], { encoding: 'utf8' });
  assert.equal(r.status, 1, r.stderr);
  assert.match(r.stderr, /-Version v9\.9\.9 names a release that ships windows\/amd64/);
});
