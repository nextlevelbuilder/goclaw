VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  = -s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=$(VERSION)
BINARY   = goclaw

.PHONY: build run clean version net up down logs reset test vet check-web dev migrate setup ci

build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)

version:
	@echo $(VERSION)

COMPOSE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.selfservice.yml
UPGRADE = docker compose -f docker-compose.yml -f docker-compose.postgres.yml -f docker-compose.upgrade.yml

net:
	docker network inspect shared >/dev/null 2>&1 || docker network create shared

up: net
	$(COMPOSE) up -d --build
	$(UPGRADE) run --rm upgrade

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f goclaw

reset: net
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
