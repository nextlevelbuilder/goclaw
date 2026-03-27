VERSION ?= $(shell git describe --tags --abbrev=0 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=$(VERSION)
BINARY   = goclaw

.PHONY: build run clean version up down logs reset test vet check-web dev migrate setup ci desktop-dev desktop-build desktop-dmg

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

version:
	@echo $(VERSION)

COMPOSE_BASE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml
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
COMPOSE = $(COMPOSE_BASE) $(COMPOSE_EXTRA)
UPGRADE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.upgrade.yml

version-file:
	@echo $(VERSION) > VERSION

up: version-file
	$(COMPOSE) up -d --build
	$(UPGRADE) run --rm upgrade

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f goclaw

reset: version-file
	$(COMPOSE) down -v
	$(COMPOSE) up -d --build

test:
	go test -race ./...

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
