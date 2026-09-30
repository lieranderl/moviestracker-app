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

.PHONY: all help torrserver dmg winapp assets templ templ-check assets-check lint test security coverage ci build run dev web web-dev firestore test-store clean \
	docker-build docker-smoke container-run container-stop

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
	@echo "  make dmg VERSION=v0.1.0      Build only Moviestracker.app and its DMG (macOS)"
	@echo "  make winapp VERSION=v0.1.0   Build the Windows installer (Windows, Inno Setup 7)"
	@echo "  make run             Run locally over HTTP with secure cookies disabled"
	@echo "  make dev             Run Air locally with secure cookies disabled"
	@echo "  make web             Run the cloud web app (cmd/web) on http://localhost:8080"
	@echo "  make web-dev         Run the cloud web app with Air (rebuilds on changes)"
	@echo "  make firestore       Start the Firestore emulator on port 8086 (docker or container)"
	@echo "  make test-store      Check the user store against that emulator"
	@echo "  make docker-build    Build the Linux image (docker, or Apple's container CLI)"
	@echo "  make docker-smoke    Build and test the image end to end with compose.yaml (Docker)"
	@echo "  make container-run   Run the image on port 8095 with Apple's container CLI"
	@echo "  make container-stop  Stop it"

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
	@if command -v shellcheck >/dev/null; then shellcheck scripts/*.sh scripts/ci/*.sh macos/install-gstreamer.sh; else echo "shellcheck not installed: shell scripts not checked"; fi
	$(ACTIONLINT)
	@if command -v uvx >/dev/null; then uvx zizmor@1.30.1 --offline --quiet .github; else echo "uv not installed: workflows not audited with zizmor (CI does)"; fi
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
	$(CONTAINER_TOOL) build --build-arg VERSION=$(or $(VERSION),dev) -t moviestracker:local .

# The image end to end, as CI runs it: scripts/ci/docker-e2e.sh (Docker and
# Compose, port 8095 free).
docker-smoke:
	scripts/ci/docker-e2e.sh

# Apple's container CLI mounts new volumes owned by root: hand the volume to
# the image's user (uid 1000) first. Docker does that by itself.
container-run: docker-build
	@name=$(CONTAINER_NAME); \
	env_arg=$$(test -f .env && echo "--env-file .env" || true); \
	container rm -f $$name >/dev/null 2>&1 || true; \
	container volume create $$name-data >/dev/null 2>&1 || true; \
	container run --rm --user 0 --entrypoint chown -v $$name-data:/data moviestracker:local 1000:1000 /data; \
	container run -d --name $$name --read-only --tmpfs /tmp $$env_arg \
		-v $$name-data:/data -p 127.0.0.1:$(CONTAINER_PORT):8095 moviestracker:local; \
	echo "Moviestracker running at http://127.0.0.1:$(CONTAINER_PORT) (setup code: container logs $$name)"

container-stop:
	container stop $(CONTAINER_NAME) 2>/dev/null || true
	container rm -f $(CONTAINER_NAME) 2>/dev/null || true

# Development keeps its accounts and keys apart from a real install, and runs
# the TorrServer from `make torrserver` when there is one.
DEV_DATA_DIR ?= .devdata
DEV_ENV = MT_DATA_DIR=$(DEV_DATA_DIR) $(if $(wildcard bin/torrserver),MT_TORRSERVER_BIN=$(CURDIR)/bin/torrserver)

torrserver: bin/torrserver


# Only the Mac app and its DMG:  make dmg VERSION=v0.1.0
dmg:
	@test -n "$(VERSION)" || (echo "usage: make dmg VERSION=v0.1.0" && exit 1)
	scripts/macapp.sh $(VERSION)

# The Windows installer (on Windows, with Inno Setup 7):  make winapp VERSION=v0.1.0
winapp:
	@test -n "$(VERSION)" || (echo "usage: make winapp VERSION=v0.1.0" && exit 1)
	scripts/winapp.sh $(VERSION)

bin/torrserver: scripts/torrserver.lock
	scripts/fetch-torrserver.sh $@

run: templ assets
	$(DEV_ENV) $(GO) run ./cmd/server

dev: assets
	$(DEV_ENV) $(AIR)

# The cloud web app (cmd/web) on http://localhost:8080 (PORT changes it),
# with the settings in .env.web (see .env.web.example) when it exists.
WEB_ENV = set -a; if [ -f .env.web ]; then . ./.env.web; fi; set +a;

web: templ assets
	$(WEB_ENV) $(GO) run ./cmd/web

# The Firestore emulator for the web app's user store (.env.web points the
# app at it; the image is pinned like CI's).
FIRESTORE_EMULATOR_IMAGE ?= gcr.io/google.com/cloudsdktool/google-cloud-cli:587.0.0-emulators@sha256:8d8573471c489a409f03891d2c24d177fa531276184f7002b9a2d853d1b3ed54

firestore:
	$(CONTAINER_TOOL) run -d --rm --name moviestracker-firestore -p 8086:8086 $(FIRESTORE_EMULATOR_IMAGE) \
		gcloud emulators firestore start --host-port=0.0.0.0:8086
	@echo "Firestore emulator on localhost:8086; stop it with: $(notdir $(CONTAINER_TOOL)) stop moviestracker-firestore"

test-store:
	FIRESTORE_EMULATOR_HOST=localhost:8086 $(GO) test -count=1 ./internal/store/

web-dev: assets
	$(WEB_ENV) $(AIR) --build.cmd "bun run assets && go tool templ generate && go build -o ./tmp/web ./cmd/web" --build.bin "./tmp/web"

clean:
	rm -rf bin tmp dist build-errors.log coverage.out
