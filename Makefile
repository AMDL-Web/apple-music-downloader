# Developer entry points. `make check` is the same gate CI runs
# (.github/workflows/ci.yml calls these targets step by step).
# Written for GNU Make 3.81, the version macOS ships.

.DEFAULT_GOAL := help

GO ?= go
# ci.yml runs `make vuln` and `make lint`, so these are the only version pins.
GOVULNCHECK_VERSION ?= v1.9.0
STATICCHECK_VERSION ?= v0.8.2
# Package directories, which leaves out dot-directories such as nested
# worktrees that `gofmt -l .` would descend into.
GO_DIRS = $(shell $(GO) list -e -f '{{.Dir}}' ./... | sed 's|^$(CURDIR)/||')
NODE_TESTS = $(wildcard .github/scripts/*.test.mjs .claude/hooks/*.test.mjs apps/console/jstest/*.test.mjs apps/site/*.test.mjs deploy/install/*.test.mjs deploy/packaging/*.test.mjs)

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

.PHONY: help build run dev fmt fmt-check vet tui-web test test-short cover race lint vuln node-test verify check console-e2e temaridiff guestboot upgradecheck docker dist dist-check desktop desktop-web site

help: ## List the targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{ printf "  %-13s %s\n", $$1, $$2 }'

build: ## Compile every package, then bin/amdl with the version stamped from git
	$(GO) build ./...
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/amdl ./apps/amdl

run: ## Run the server with configs/config.yaml (needs a wrapper for real downloads)
	$(GO) run -ldflags "$(LDFLAGS)" ./apps/amdl serve

dev: ## Run the server in simulate mode on 127.0.0.1:18080 with a throwaway config and data under data/dev/
	@mkdir -p $(DEV_DIR)
	@printf 'server:\n  listen: "%s"\ndatabase:\n  path: "%s"\ncatalog:\n  language: "en-US"\ndownload:\n  downloads_dir: "%s"\n  temp_dir: "%s"\nsimulate:\n  enabled: true\n' \
		$(DEV_LISTEN) $(DEV_DIR)/amdl.db $(DEV_DIR)/downloads $(DEV_DIR)/tmp > $(DEV_DIR)/config.yaml
	@test -f $(DEV_DIR)/hooks.yaml || cp configs/hooks.example.yaml $(DEV_DIR)/hooks.yaml
	AMDL_CONFIG=$(DEV_DIR)/config.yaml \
	AMDL_HOOKS_CONFIG=$(DEV_DIR)/hooks.yaml \
	$(GO) run -ldflags "$(LDFLAGS)" ./apps/amdl serve

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

vet: ## go vet every package, and the desktop app's browser build and guest boot test
	$(GO) vet ./...
	$(GO) vet -tags guestboot ./internal/wrapperhost/
	GOOS=js GOARCH=wasm $(GO) vet ./apps/desktop/...

tui-web: ## Build the terminal interface's browser demo into apps/amdl/build/web/
	$(GO) run ./apps/amdl/web/build -out apps/amdl/build/web -ldflags "-s -w $(LDFLAGS)"

# The test layers (CONTRIBUTING.md#tests): unit and integration tests are
# plain `go test`; internal/media/integration and the API consistency rig skip
# under -short; the end-to-end scripts in apps/amdl/testdata/script drive the
# real amdl binary through testscript and run with the rest.
test: ## Run every Go test: unit, integration and the amdl end-to-end scripts
	$(GO) test ./...

test-short: ## Run the Go tests without the slow integration ones (-short)
	$(GO) test -short ./...

# `make cover` counts every package's statements across every test package,
# so tests in internal/media/integration count for internal/media and the
# end-to-end scripts (whose amdl subprocesses write to the same coverage
# directory) count for the packages they reach. The summary is display only,
# with no threshold; one that cannot be built warns and never fails the run.
COVER_DIR = build/cover
COVER_PROFILE = $(COVER_DIR)/coverage.out

cover: ## Run the Go tests with coverage across packages and print a summary
	@rm -rf build/cover && mkdir -p build/cover/data
	$(GO) test -cover -covermode=set -coverpkg=./... ./... -args -test.gocoverdir=$(CURDIR)/$(COVER_DIR)/data
	$(GO) tool covdata textfmt -i=$(COVER_DIR)/data -o=$(COVER_PROFILE)
	@summary="$$(sh .github/scripts/cover-summary.sh $(COVER_PROFILE))" \
		&& echo "$$summary" \
		&& { [ -z "$$GITHUB_STEP_SUMMARY" ] || echo "$$summary" >> "$$GITHUB_STEP_SUMMARY"; } \
		|| echo "cover: no coverage summary" >&2

# The race detector leaves out the desktop ui and golden packages: their code
# runs on the one UI thread, so the detector finds little there, while it
# slows their frame-by-frame rendering about tenfold (most of the race job's
# time). The desktop's concurrent code, store and artwork, is still checked.
# A list that comes out empty fails, because an empty list would make
# `go test` run the current directory.
RACE_SKIP = ^amdl/apps/desktop/internal/(ui|golden)$$

race: ## Run the Go tests under the race detector (all but the desktop ui and golden packages)
	@pkgs="$$($(GO) list ./... | grep -Ev '$(RACE_SKIP)')" && test -n "$$pkgs" || { echo "race: no packages" >&2; exit 1; }; \
	echo "$(GO) test -race <every package but the desktop ui and golden>"; \
	$(GO) test -race $$pkgs

lint: ## Run staticcheck (checks in staticcheck.conf)
	$(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION) ./...

vuln: ## Scan dependencies with govulncheck
	$(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

node-test: ## Run the Node tests for release tooling, the guard hook, console JS, install scripts and the website build
	@command -v node >/dev/null 2>&1 || { echo "node-test: node is not installed; install Node.js 26 or newer (CI uses 26)" >&2; exit 1; }
	@if [ -z "$(strip $(NODE_TESTS))" ]; then echo "node-test: no *.test.mjs files found"; exit 1; fi
	node --test $(NODE_TESTS)

# The quick local gate: what to run before a commit.
verify: fmt-check vet lint test node-test ## Format, vet, staticcheck, the Go tests and the Node tests

check: fmt-check build vet lint tui-web test race vuln node-test ## The CI gate: everything above, in CI's order (CI runs test and race as parallel jobs)

# The console's browser tests (apps/console/e2e/README.md): Playwright against
# the real embedded console and the in-memory demo backend. Needs Node.js and
# Chromium (npx playwright install chromium); CI runs them as their own job in
# the Playwright image, where the screenshot baselines are made.
console-e2e: ## Run the console's browser tests (Playwright)
	cd apps/console/e2e && npm ci && GO=$(GO) npm test

temaridiff: ## Compare internal/fairplay with upstream Temari (own CI job; downloads Temari's cdylib)
	cd tools/temaridiff && $(GO) test -count=1 ./...

# TestGuestBoot downloads the pinned guest and payload (about 80 MB, kept in
# GUESTBOOT_CACHE_DIR when set) and boots them; without KVM it takes ten minutes
# or more. CI runs it as its own job (.github/actions/guestboot), so `make check`
# leaves it out. Its environment variables are listed in the test file.
guestboot: ## Boot the pinned wrapper guest in QEMU and wait for its manager (needs qemu and network; its own CI job)
	$(GO) test -tags guestboot -run '^TestGuestBoot$$' -count=1 -timeout 30m -v ./internal/wrapperhost/

# Starts the build on the files the newest release wrote (a server install, the
# embedded backend, a desktop install with a retired guest); see
# tools/upgradecheck/run.sh. Downloads that release with gh. CI runs it as its
# own job and the release process before every release candidate, so
# `make check` leaves it out.
# perl's alarm bounds the query the same on Linux and macOS, which has no timeout.
PREV ?= $(shell perl -e 'alarm shift; exec @ARGV' 60 gh release list -L 1 --exclude-drafts --json tagName -q '.[0].tagName' 2>/dev/null)

upgradecheck: build ## Start this build on the files the newest release wrote (needs gh; its own CI job)
	@test -n "$(PREV)" || { echo "upgradecheck: no release found (gh release list failed or took over 60 s); set PREV=<tag>" >&2; exit 1; }
	tools/upgradecheck/run.sh "$(PREV)" bin/amdl

docker: ## Build the server image locally (single arch, not pushed)
	docker build -f deploy/server/Dockerfile -t $(DOCKER_IMAGE) .

# The platforms the release attaches an amdl binary for. modernc.org/sqlite is
# pure Go, so CGO_ENABLED=0 cross-compiles each from any host.
DIST_TARGETS = linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

dist: ## Cross-compile amdl for every release platform into dist/, one archive each plus SHA256SUMS
	@rm -rf dist && mkdir -p dist
	@for t in $(DIST_TARGETS); do \
		os=$${t%/*}; arch=$${t#*/}; name=amdl_$(VERSION)_$${os}_$${arch}; ext=; \
		if [ "$$os" = windows ]; then ext=.exe; fi; \
		echo "dist: $$name"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "-s -w $(LDFLAGS)" -o dist/$$name/amdl$$ext ./apps/amdl || exit 1; \
		cp LICENSE README.md dist/$$name/ || exit 1; \
		if [ "$$os" = windows ]; then (cd dist && zip -qr $$name.zip $$name) || exit 1; \
		else tar -C dist -czf dist/$$name.tar.gz $$name || exit 1; fi; \
		rm -rf dist/$$name; \
	done
	@cd dist && { sha256sum *.tar.gz *.zip 2>/dev/null || shasum -a 256 *.tar.gz *.zip; } > SHA256SUMS

# What a release attaches: five archives, SHA256SUMS, amdl.rb and amdl.json.
DIST_FILES = 8

dist-check: ## Build and render the release files as a release does, for a made-up version, and count them
	@$(MAKE) --no-print-directory dist VERSION=v0.0.0-ci
	@node deploy/packaging/render.mjs v0.0.0-ci dist/SHA256SUMS dist
	@n=$$(ls dist | wc -l); if [ $$n -ne $(DIST_FILES) ]; then echo "dist-check: $$n files in dist/, want $(DIST_FILES)"; ls dist; exit 1; fi

# The desktop app's targets, as GOOS/GOARCH pairs or web. MyGo's CLI (the
# go.mod tool) reads apps/desktop/mygo.config.ts, which needs Node.js, and
# writes apps/desktop/build/<os>-<arch>/. Disk images need macOS and the
# Windows installer NSIS; elsewhere the CLI skips them with a note.
DESKTOP_PLATFORMS ?= $(shell $(GO) env GOOS)/$(shell $(GO) env GOARCH)
DESKTOP_MYGO = cd apps/desktop && DESKTOP_VERSION=$(patsubst v%,%,$(VERSION)) MYGO_LDFLAGS="$(LDFLAGS)" $(GO) tool mygo build

desktop: ## Package the desktop app for DESKTOP_PLATFORMS (default: this machine) into apps/desktop/build/
	$(DESKTOP_MYGO) -platform $(DESKTOP_PLATFORMS)

desktop-web: ## Build the desktop app's browser demo into apps/desktop/build/web/
	$(DESKTOP_MYGO) -platform web

# The website as GitHub Pages publishes it (.github/workflows/pages.yml): the
# pages, and the Open Graph card, which needs Chrome (apps/site/og.mjs).
SITE_OUT ?= apps/site/dist
SITE_BASE ?= https://amdl.lyjw.me/

site: ## Build the website into SITE_OUT (default apps/site/dist/)
	node apps/site/build.mjs --base $(SITE_BASE) --out $(SITE_OUT)
	node apps/site/og.mjs --out $(SITE_OUT)
