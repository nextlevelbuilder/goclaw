VERSION ?= $(shell git describe --tags --abbrev=0 --match "v[0-9]*" 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=$(VERSION)
BINARY   = goclaw

.PHONY: build build-full build-tui run clean version up up-build down logs reset test vet check-web dev migrate setup ci desktop-dev desktop-build desktop-dmg test-hooks test-hooks-unit test-hooks-e2e test-hooks-chaos test-hooks-rbac test-hooks-tracing

# Build backend only (API-only, no embedded web UI)
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

# Build with embedded web UI (recommended for production)
build-full: check-web
	rm -rf internal/webui/dist && mkdir -p internal/webui/dist
	cp -r ui/web/dist/* internal/webui/dist/
	CGO_ENABLED=0 go build -tags embedui -ldflags="$(LDFLAGS)" -o $(BINARY) .

# Build with TUI (Bubble Tea enhanced CLI)
build-tui:
	CGO_ENABLED=0 go build -tags tui -ldflags="$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	rm -rf internal/webui/dist

version:
	@echo $(VERSION)

# ── Docker Compose ──
# Default: backend (with embedded web UI) + Postgres. No separate nginx needed.
# Add WITH_WEB_NGINX=1 for separate nginx on :3000 (custom SSL, reverse proxy).
COMPOSE_BASE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml
ifdef WITH_WEB_NGINX
COMPOSE_BASE += -f docker-compose.selfservice.yml
export ENABLE_EMBEDUI=false
endif
COMPOSE_EXTRA =
ifdef WITH_BROWSER
COMPOSE_EXTRA += -f docker-compose.browser.yml
endif
ifdef WITH_OTEL
COMPOSE_EXTRA += -f docker-compose.otel.yml
endif
ifdef WITH_SANDBOX
COMPOSE_EXTRA += -f docker-compose.sandbox.yml
endif
ifdef WITH_TAILSCALE
COMPOSE_EXTRA += -f docker-compose.tailscale.yml
endif
ifdef WITH_REDIS
COMPOSE_EXTRA += -f docker-compose.redis.yml
endif
ifdef WITH_CLAUDE_CLI
COMPOSE_EXTRA += -f docker-compose.claude-cli.yml
endif
COMPOSE = $(COMPOSE_BASE) $(COMPOSE_EXTRA)
UPGRADE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.upgrade.yml

version-file:
	@echo $(VERSION) > VERSION

# Pull latest published image (if changed) and start. Use for deploy/update flows.
up: version-file
	GOCLAW_VERSION=$(VERSION) $(COMPOSE) up -d --pull always
	$(UPGRADE) run --rm upgrade

# Build image from local source (with pulled base layers), then start. Use for dev changes.
up-build: version-file
	GOCLAW_VERSION=$(VERSION) $(COMPOSE) build --pull
	GOCLAW_VERSION=$(VERSION) $(COMPOSE) up -d
	$(UPGRADE) run --rm upgrade

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f goclaw

reset: version-file
	$(COMPOSE) down -v
	GOCLAW_VERSION=$(VERSION) $(COMPOSE) up -d --pull always

test:
	go test -race -timeout=5m ./...

# ── Layered Testing ──
# P0: Invariant tests - tenant isolation, permission enforcement (MUST pass)
test-invariants:
	go test -race -timeout=90s -tags integration ./tests/invariants/...

# P1: Contract tests - API schema validation (MUST pass)
test-contracts:
	go test -race -timeout=90s -tags integration ./tests/contracts/...

# P2: Scenario tests - end-to-end user journeys (warning only)
test-scenarios:
	go test -race -timeout=180s -tags integration ./tests/scenarios/...

# Critical tests (P0 + P1) - run before merge
test-critical: test-invariants test-contracts

# ── Agent Hooks targets (phase 4) ──
# Requires TEST_DATABASE_URL pointing at a pgvector:pg18 container on :5433
test-hooks-unit:
	go test -race ./internal/hooks/... ./internal/gateway/methods/

test-hooks-e2e:
	go test -race -timeout=180s -tags integration -run "TestHooksE2E" ./tests/integration/

test-hooks-chaos:
	go test -race -timeout=180s -tags integration -run "TestHooksChaos" ./tests/integration/

test-hooks-rbac:
	go test -race -timeout=90s -tags integration -run "TestHooksRBAC" ./tests/integration/

test-hooks-tracing:
	go test -race -timeout=90s -tags integration -run "TestHooksTracing" ./tests/integration/

# Full hook test suite (unit + integration)
test-hooks: test-hooks-unit test-hooks-e2e test-hooks-chaos test-hooks-rbac test-hooks-tracing

vet:
	go vet ./...

check-web:
	cd ui/web && pnpm install --frozen-lockfile && pnpm build

dev:
	cd ui/web && pnpm dev

migrate:
	$(COMPOSE) run --rm goclaw migrate up

setup:
	go mod download
	cd ui/web && pnpm install --frozen-lockfile

ci: build test vet check-web

# ── Desktop (Wails + SQLite) ──

desktop-dev:
	cd ui/desktop && wails dev -tags sqliteonly

desktop-build:
	cd ui/desktop && wails build -tags sqliteonly -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=$(VERSION)"

desktop-dmg: desktop-build
	@echo "Creating DMG..."
	rm -rf /tmp/goclaw-dmg-staging
	mkdir -p /tmp/goclaw-dmg-staging
	cp -R ui/desktop/build/bin/goclaw-lite.app /tmp/goclaw-dmg-staging/
	ln -s /Applications /tmp/goclaw-dmg-staging/Applications
	hdiutil create -volname "GoClaw Lite $(VERSION)" -srcfolder /tmp/goclaw-dmg-staging \
		-ov -format UDZO "goclaw-lite-$(VERSION)-darwin-$$(uname -m | sed 's/x86_64/amd64/').dmg"
	rm -rf /tmp/goclaw-dmg-staging
	@echo "DMG created: goclaw-lite-$(VERSION)-darwin-$$(uname -m | sed 's/x86_64/amd64/').dmg"
