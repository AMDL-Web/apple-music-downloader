# Developer entry points. `make check` is the same gate CI runs
# (.github/workflows/ci.yml calls these targets step by step).
# Written for GNU Make 3.81, the version macOS ships.

.DEFAULT_GOAL := help

GO ?= go
# ci.yml runs `make vuln`, so this is the only govulncheck version pin.
GOVULNCHECK_VERSION ?= v1.6.0
# Package directories, which leaves out dot-directories such as nested
# worktrees under .claude/worktrees/ that `gofmt -l .` would descend into.
GO_DIRS = $(shell $(GO) list -e -f '{{.Dir}}' ./... | sed 's|^$(CURDIR)/||')
NODE_TESTS = $(wildcard .github/scripts/*.test.mjs scripts/*.test.mjs internal/console/jstest/*.test.mjs)

# Build identity stamped into internal/buildinfo (GET /api/v1/system). Each
# falls back quietly when git or the .git directory is missing, such as in a
# source tarball: version reads dev and the commit stays empty.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS = -X amdl/internal/buildinfo.Version=$(VERSION) -X amdl/internal/buildinfo.Commit=$(COMMIT) -X amdl/internal/buildinfo.BuildDate=$(BUILD_DATE)

DEV_DIR := data/dev
DEV_LISTEN := 127.0.0.1:18080
DOCKER_IMAGE ?= amdl-backend:dev

.PHONY: help build run dev fmt fmt-check vet test race vuln node-test check temaridiff docker

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{ printf "  %-10s %s\n", $$1, $$2 }'

build: ## Compile every package, then the ./amdl-api binary with the version stamped from git
	$(GO) build ./...
	$(GO) build -ldflags "$(LDFLAGS)" -o amdl-api ./cmd/amdl-api

run: ## Run the API with configs/config.yaml (needs wrapper-manager for real downloads)
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/amdl-api

dev: ## Run the API in simulate mode on 127.0.0.1:18080 with a throwaway config and data under data/dev/
	@mkdir -p $(DEV_DIR)
	@printf 'catalog:\n  language: "en-US"\ndownload:\n  downloads_dir: "%s"\n  temp_dir: "%s"\n  album_path_format: "albums/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}"\n  artist_path_format: "artists/{ArtistName}/{AlbumName}/{SongNumber:02d}. {SongName}"\nsimulate:\n  enabled: true\n' \
		$(DEV_DIR)/downloads $(DEV_DIR)/tmp > $(DEV_DIR)/config.yaml
	@test -f $(DEV_DIR)/hooks.yaml || cp configs/hooks.yaml $(DEV_DIR)/hooks.yaml
	AMDL_CONFIG=$(DEV_DIR)/config.yaml \
	AMDL_HOOKS_CONFIG=$(DEV_DIR)/hooks.yaml \
	AMDL_SERVER_LISTEN=$(DEV_LISTEN) \
	AMDL_DATABASE_PATH=$(DEV_DIR)/amdl.db \
	$(GO) run -ldflags "$(LDFLAGS)" ./cmd/amdl-api

fmt: ## Rewrite Go files with gofmt
	@test -n "$(strip $(GO_DIRS))" || { echo "fmt: go list found no packages" >&2; exit 1; }
	gofmt -w $(GO_DIRS)

fmt-check: ## Fail if any Go file is not gofmt-formatted
	@test -n "$(strip $(GO_DIRS))" || { echo "fmt-check: go list found no packages" >&2; exit 1; }
	@out="$$(gofmt -l $(GO_DIRS))"; \
	if [ -n "$$out" ]; then \
		echo "These files are not gofmt-formatted; run make fmt:" >&2; \
		echo "$$out" >&2; \
		exit 1; \
	fi

vet: ## go vet every package
	$(GO) vet ./...

test: ## Run the test suite
	$(GO) test ./...

race: ## Run the test suite under the race detector
	$(GO) test -race ./...

vuln: ## Scan dependencies with govulncheck
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

node-test: ## Run the Node tests for release tooling, guard scripts and console JS
	@command -v node >/dev/null 2>&1 || { echo "node-test: node is not installed; install Node.js 24 or newer (CI uses 24)" >&2; exit 1; }
	@if [ -z "$(strip $(NODE_TESTS))" ]; then echo "node-test: no *.test.mjs files found"; exit 1; fi
	node --test $(NODE_TESTS)

check: fmt-check build vet test race vuln node-test ## The CI gate: everything above, in CI's order

temaridiff: ## Compare internal/fairplay with upstream Temari (own CI job; downloads Temari's cdylib)
	cd tools/temaridiff && $(GO) test -count=1 ./...

docker: ## Build the container image locally (single arch, not pushed)
	docker build -t $(DOCKER_IMAGE) .
