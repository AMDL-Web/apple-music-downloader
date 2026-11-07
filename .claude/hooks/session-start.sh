#!/bin/bash
# Cloud sessions start from a fresh container: fetch modules and build once so
# the first go test or make target does not pay for it. Local sessions skip this.
set -euo pipefail

if [ "${CLAUDE_CODE_REMOTE:-}" != "true" ]; then
  exit 0
fi

cd "$CLAUDE_PROJECT_DIR"
go mod download
go build ./...
