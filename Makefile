SHELL := /bin/bash

GO ?= go
BUN ?= bun
AIR ?= air
GOLANGCI_LINT ?= $(GO) tool golangci-lint
# Checks .github/workflows; run from its own module (its YAML library clashes with ours).
ACTIONLINT ?= $(GO) run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

# Container CLI detection: prefers docker if present, falls back to Apple container CLI
CONTAINER_TOOL ?= $(shell command -v docker 2>/dev/null || command -v container 2>/dev/null || echo docker)
CONTAINER_NAME ?= moviestracker-app
CONTAINER_PORT ?= 8095

.PHONY: all help torrserver release dmg assets templ templ-check assets-check lint test security coverage ci build run dev clean \
	docker-build docker-smoke container-build container-run container-smoke container-stop

all: ci

help:
	@echo "Moviestracker commands:"
	@echo "  make assets          Build pinned CSS and JavaScript assets"
	@echo "  make templ           Generate Go code from Templ templates"
	@echo "  make lint            Verify generated code/assets and run static analysis"
	@echo "  make test            Run lint and the race-enabled test suite"
	@echo "  make security        Run govulncheck and gosec"
	@echo "  make coverage        Write Go coverage data to coverage.out"
	@echo "  make ci              Run the complete CI quality gate"
	@echo "  make build           Build the self-contained binary at bin/server"
	@echo "  make torrserver      Download the pinned TorrServer to bin/torrserver"
	@echo "  make release VERSION=v0.1.0  Build the Linux archives (and the DMG on a Mac) into dist/"
	@echo "  make dmg VERSION=v0.1.0      Build only Moviestracker.app and its DMG (macOS)"
	@echo "  make run             Run locally over HTTP with secure cookies disabled"
	@echo "  make dev             Run Air locally with secure cookies disabled"
	@echo "  make docker-build    Build container image using docker or container CLI"
	@echo "  make docker-smoke    Run read-only container health smoke test"
	@echo "  make container-build Build image using Apple container CLI"
	@echo "  make container-run   Run image on port 8095 with Apple container CLI"
	@echo "  make container-smoke Run smoke test using Apple container CLI"
	@echo "  make container-stop  Stop running Apple container instance"

node_modules: package.json bun.lock
	$(BUN) install --frozen-lockfile

assets: node_modules
	$(BUN) run assets

templ:
	$(GO) tool templ generate

templ-check:
	$(GO) tool templ generate -check

assets-check: node_modules
	$(BUN) run assets
	git diff --exit-code -- static/app.css static/player.js static/theme.js internal/views/icons_gen.go

lint: templ-check assets-check
	@if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh scripts/ci/*.sh packaging/install.sh macos/install-gstreamer.sh; else echo "shellcheck not installed: shell scripts not checked"; fi
	$(ACTIONLINT)
	$(GO) vet ./...
	$(GO) tool staticcheck ./...
	$(GOLANGCI_LINT) run ./...
	@# The Windows tray app and the Windows halves of the engine: checked for
	@# Windows by the tools built for this machine (go tool -n prints their path).
	GOOS=windows $(GO) vet ./...
	GOOS=windows $$($(GO) tool -n staticcheck) ./...
	GOOS=windows $$($(GO) tool -n golangci-lint) run ./...
	@test -z "$$(gofmt -l cmd internal static)" || (echo "Unformatted Go files:" && gofmt -l cmd internal static && exit 1)

test: lint
	$(GO) test -race -count=1 ./...

security:
	$(GO) tool govulncheck ./...
	$(GO) tool gosec ./...
	GOOS=windows $$($(GO) tool -n govulncheck) ./...
	GOOS=windows $$($(GO) tool -n gosec) -quiet ./cmd/tray/... ./internal/tray/... ./internal/engine/...

coverage:
	$(GO) test -coverprofile=coverage.out ./...

build: lint
	mkdir -p bin
	$(GO) build -trimpath -o bin/server ./cmd/server

ci: assets-check lint test security build

docker-build:
	$(CONTAINER_TOOL) build -t moviestracker:local .

docker-smoke: docker-build
	@name=moviestracker-smoke; \
	tool=$$(basename "$(CONTAINER_TOOL)"); \
	$$tool rm -f $$name >/dev/null 2>&1 || true; \
	trap '$$tool rm -f $$name >/dev/null 2>&1 || true' EXIT; \
	if [ "$$tool" = "container" ]; then \
		$$tool run -d --name $$name --read-only --cap-drop ALL \
			--tmpfs /data -e MT_LISTEN=:$(CONTAINER_PORT) -p 127.0.0.1:$(CONTAINER_PORT):$(CONTAINER_PORT) \
			moviestracker:local >/dev/null; \
		port=$(CONTAINER_PORT); \
	else \
		$$tool run -d --name $$name --read-only --cap-drop=ALL \
			--security-opt=no-new-privileges --tmpfs /data \
			-p 127.0.0.1::8095 moviestracker:local >/dev/null; \
		port=$$($$tool port $$name 8095/tcp | sed 's/.*://'); \
	fi; \
	for attempt in $$(seq 1 30); do \
		body=$$(curl -fsS "http://127.0.0.1:$$port/healthz" 2>/dev/null) && \
			test "$$body" = "ok" && exit 0; \
		sleep 1; \
	done; \
	$$tool logs $$name; \
	exit 1

container-build:
	container build -t moviestracker:local .

container-run:
	@name=$(CONTAINER_NAME); \
	env_arg=$$(test -f .env && echo "--env-file .env" || true); \
	container rm -f $$name >/dev/null 2>&1 || true; \
	container run -d --name $$name --read-only --cap-drop ALL \
		$$env_arg \
		--tmpfs /data -e MT_LISTEN=:$(CONTAINER_PORT) -p 127.0.0.1:$(CONTAINER_PORT):$(CONTAINER_PORT) \
		moviestracker:local; \
	echo "Container running at http://127.0.0.1:$(CONTAINER_PORT)"

container-smoke: container-build
	@name=moviestracker-smoke; \
	container rm -f $$name >/dev/null 2>&1 || true; \
	trap 'container rm -f $$name >/dev/null 2>&1 || true' EXIT; \
	container run -d --name $$name --read-only --cap-drop ALL \
		--tmpfs /data -e MT_LISTEN=:$(CONTAINER_PORT) -p 127.0.0.1:$(CONTAINER_PORT):$(CONTAINER_PORT) \
		moviestracker:local >/dev/null; \
	for attempt in $$(seq 1 30); do \
		body=$$(curl -fsS "http://127.0.0.1:$(CONTAINER_PORT)/healthz" 2>/dev/null) && \
			test "$$body" = "ok" && echo "Health check passed: $$body" && exit 0; \
		sleep 1; \
	done; \
	container logs $$name; \
	exit 1

container-stop:
	container rm -f $(CONTAINER_NAME) 2>/dev/null || true

# Development keeps its accounts and keys apart from a real install, and runs
# the TorrServer from `make torrserver` when there is one.
DEV_DATA_DIR ?= .devdata
DEV_ENV = MT_DATA_DIR=$(DEV_DATA_DIR) $(if $(wildcard bin/torrserver),MT_TORRSERVER_BIN=$(CURDIR)/bin/torrserver)

torrserver: bin/torrserver

# A release in dist/: Linux archives (amd64, arm64) and, on a Mac, the
# Moviestracker DMG, with checksums.txt:
#   make release VERSION=v0.1.0
release:
	@test -n "$(VERSION)" || (echo "usage: make release VERSION=v0.1.0" && exit 1)
	scripts/release.sh $(VERSION)

# Only the Mac app and its DMG:  make dmg VERSION=v0.1.0
dmg:
	@test -n "$(VERSION)" || (echo "usage: make dmg VERSION=v0.1.0" && exit 1)
	scripts/macapp.sh $(VERSION)

bin/torrserver: scripts/torrserver.lock
	scripts/fetch-torrserver.sh $@

run: templ assets
	$(DEV_ENV) $(GO) run ./cmd/server

dev: assets
	$(DEV_ENV) $(AIR)

clean:
	rm -rf bin tmp dist build-errors.log coverage.out
