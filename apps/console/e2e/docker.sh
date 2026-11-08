#!/bin/sh
# Runs the console's browser tests in the Playwright image, the one CI runs
# them in, screenshot comparisons included: their baselines are made there,
# since fonts and anti-aliasing differ from machine to machine. The server is
# built here (the image has no Go) and started in the container.
#
#   ./docker.sh                       the whole suite
#   ./docker.sh --update-snapshots    the same, rewriting the baselines
#   ./docker.sh tests/screens.spec.mjs -g overview
set -eu

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
version=$(node -p "require('$here/node_modules/@playwright/test/package.json').version")
image="mcr.microsoft.com/playwright:v$version-noble"

mkdir -p "$here/out/bin" "$here/out/gocover"
(cd "$root" && CGO_ENABLED=0 "${GO:-go}" build -cover -o "$here/out/bin/console-e2e" ./apps/console/e2e/server)

exec docker run --rm --init --ipc=host \
	--user "$(id -u):$(id -g)" -e HOME=/tmp -e CI="${CI:-}" \
	-e CONSOLE_E2E_PREBUILT=1 -e CONSOLE_E2E_SCREENSHOTS=1 \
	-v "$root:$root" -w "$here" \
	"$image" npx playwright test "$@"
