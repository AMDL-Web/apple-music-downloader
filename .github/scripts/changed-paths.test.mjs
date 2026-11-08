import assert from 'node:assert/strict';
import { test } from 'node:test';
import { classify } from './changed-paths.mjs';

test('the guest boot runs for the wrapper host, the pin and its own files', () => {
  for (const f of ['internal/wrapperhost/qemu.go', 'internal/wrapperhost/pin.go', '.github/scripts/wrapper-pin.mjs',
    '.github/scripts/wrapper-pin.test.mjs', '.github/actions/guestboot/action.yml', '.github/workflows/guestboot.yml',
    'internal/ext4/ext4.go', 'internal/config/config.go', 'internal/client/types.go', 'internal/domain/ids.go',
    'internal/limits/limits.go', 'internal/fairplay/rounds_gen.go', 'go.mod']) {
    assert.equal(classify([f]).guestboot, true, f);
  }
});

test('the guest boot skips a change that cannot reach it', () => {
  assert.equal(classify(['README.md', 'docs/cli.md', 'apps/console/web/app.js', 'internal/jobs/jobs.go', 'internal/configs/x.go']).guestboot, false);
});

test('the console browser tests run for the console, the API spec and the CI files', () => {
  for (const f of ['apps/console/static/css/queue.css', 'apps/console/e2e/queue.spec.mjs',
    'internal/api/openapi.yaml', '.github/workflows/ci.yml', '.github/scripts/changed-paths.mjs']) {
    assert.equal(classify([f]).console, true, f);
  }
  assert.equal(classify(['README.md', 'docs/api.md', 'apps/site/build.mjs', 'internal/jobs/jobs.go', 'internal/api/events.go']).console, false);
  assert.equal(classify(['internal/jobs/jobs.go', 'apps/console/static/js/api.js']).console, true);
  for (const f of ['internal/demo/api.go', 'internal/domain/event.go', 'go.sum']) {
    assert.equal(classify([f]).console, true, f);
  }
});

test('one matching path among many is enough', () => {
  assert.equal(classify(['README.md', 'internal/wrapperhost/guest.go', 'docs/api.md']).guestboot, true);
});

test('an unknown change list runs every job', () => {
  assert.deepEqual(classify([]), { go: true, guestboot: true, console: true });
  assert.deepEqual(classify(['', '\n']), { go: true, guestboot: true, console: true });
});

test('documentation, agent rules, the website and the console skip the Go jobs', () => {
  for (const f of ['docs/api.md', 'README.md', 'README.zh-CN.md', 'GLOSSARY.md', 'AGENTS.md', 'CLAUDE.md',
    'internal/db/AGENTS.md', '.claude/hooks/guard-commands.mjs', 'apps/site/build.mjs', 'apps/console/static/app.js',
    '.github/release-notes/v2.0.0.md', 'LICENSE']) {
    assert.equal(classify([f]).go, false, f);
  }
  assert.equal(classify(['docs/api.md', 'apps/console/static/app.js', 'apps/site/content/en.json']).go, false);
});

// Go tests read these light files (tools/repocheck the console's status.js,
// album-progress.js and docs/api.md), so a change to only them skips the Go
// jobs and the light job runs those packages instead; tools/repocheck
// TestLightPathsReadByGoTestsAreCovered checks the packages against ci.yml.
test('light files that Go tests read are light, not Go changes', () => {
  for (const f of ['apps/console/static/js/lib/status.js', 'apps/console/static/js/lib/album-progress.js', 'docs/api.md']) {
    assert.equal(classify([f]).go, false, f);
  }
  assert.equal(classify(['docs/api.md', 'internal/api/events.go']).go, true);
});

test('anything else, embedded Markdown included, runs the Go jobs', () => {
  for (const f of ['internal/jobs/jobs.go', 'apps/amdl/skill/SKILL.md', 'configs/config.example.yaml', 'Makefile', 'go.mod',
    '.github/workflows/ci.yml', 'deploy/server/Dockerfile', 'tools/upgradecheck/run.sh', 'apps/desktop/internal/ui/x.go']) {
    assert.equal(classify([f]).go, true, f);
  }
  assert.equal(classify(['docs/api.md', 'internal/jobs/jobs.go']).go, true);
});
