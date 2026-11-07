// Browser tests of the web console. The webServer builds apps/console/e2e/server
// (the embedded console over the in-memory demo backend) with coverage on and
// starts it on a free port; see README.md.
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { defineConfig, devices } from '@playwright/test';

const here = fileURLToPath(new URL('.', import.meta.url));
const root = fileURLToPath(new URL('../../../', import.meta.url));
const out = `${here}out`;

// The runner loads this file once and again in every worker; the port is
// picked once and handed down through the environment.
if (!process.env.CONSOLE_E2E_PORT) {
  const pick = "const s=require('net').createServer().listen(0,'127.0.0.1',()=>{console.log(s.address().port);s.close()})";
  process.env.CONSOLE_E2E_PORT = execFileSync(process.execPath, ['-e', pick], { encoding: 'utf8' }).trim();
}
const origin = `http://127.0.0.1:${process.env.CONSOLE_E2E_PORT}`;
const go = process.env.GO || 'go';
const bin = `${out}/bin/console-e2e`;
// docker.sh builds the server on the host and runs the tests in the
// Playwright image, which has no Go: it names the binary to start instead.
const build = process.env.CONSOLE_E2E_PREBUILT ? 'true' : `"${go}" build -cover -o "${bin}" ./apps/console/e2e/server`;

export default defineConfig({
  testDir: './tests',
  outputDir: `${out}/test-results`,
  // Screenshot baselines are made in the Playwright image only (docker.sh),
  // since fonts and anti-aliasing differ per machine; see tests/screens.spec.mjs.
  snapshotPathTemplate: '{testDir}/__screenshots__/{testFileName}/{arg}-{projectName}-{platform}{ext}',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI
    ? [['list'], ['github'], ['html', { outputFolder: `${out}/report`, open: 'never' }]]
    : [['list'], ['html', { outputFolder: `${out}/report`, open: 'never' }]],
  timeout: 30_000,
  expect: {
    timeout: 5_000,
    toHaveScreenshot: { maxDiffPixelRatio: 0.01, threshold: 0.2, animations: 'disabled', caret: 'hide', scale: 'css' },
  },
  use: {
    baseURL: origin,
    locale: 'en-US',
    timezoneId: 'UTC',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'], viewport: { width: 1280, height: 800 } } }],
  webServer: {
    cwd: root,
    // Each run starts its coverage afresh.
    command: `rm -rf "${out}/gocover" && mkdir -p "${out}/gocover" "${out}/bin" && ${build} && exec "${bin}" -addr 127.0.0.1:${process.env.CONSOLE_E2E_PORT}`,
    url: `${origin}/api/v1/health`,
    env: { GOCOVERDIR: `${out}/gocover` },
    timeout: 180_000,
    reuseExistingServer: false,
    stdout: 'ignore',
    stderr: 'pipe',
    // SIGTERM lets the server return from main, which writes its coverage.
    gracefulShutdown: { signal: 'SIGTERM', timeout: 5_000 },
  },
});
