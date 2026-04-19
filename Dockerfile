# syntax=docker/dockerfile:1

# ENABLE_EMBEDUI controls whether the web UI is built and embedded.
# Must be declared before first FROM to use in stage selector.
ARG ENABLE_EMBEDUI=false

# ── Stage 0: Build Web UI ──
# BuildKit skips this stage entirely when ENABLE_EMBEDUI=false
# because no downstream stage in the dependency graph references it.
FROM node:22-alpine AS web-builder
RUN corepack enable && corepack prepare pnpm@10.28.2 --activate
WORKDIR /app
# Copy .npmrc first so pnpm resolves musl native bindings (needed on Alpine).
# The lockfile already includes musl entries thanks to supportedArchitectures in .npmrc.
COPY ui/web/.npmrc ui/web/package.json ui/web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY ui/web/ .
RUN pnpm build

# ── Stage selector: pick web-builder output or empty dir ──
FROM web-builder AS embedui-true
FROM busybox AS embedui-false
RUN mkdir -p /app/dist
FROM embedui-${ENABLE_EMBEDUI} AS web-dist

# ── Stage 1: Build Go ──
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build args (re-declare after FROM; top-level ARG only visible in FROM lines)
ARG ENABLE_OTEL=false
ARG ENABLE_TSNET=false
ARG ENABLE_REDIS=false
ARG ENABLE_EMBEDUI=false
ARG VERSION=

# Copy web UI dist — from web-builder when ENABLE_EMBEDUI=true, empty dir otherwise.
COPY --from=web-dist /app/dist /src/internal/webui/dist

RUN set -eux; \
    if [ -z "$VERSION" ] && [ -f VERSION ]; then VERSION=$(cat VERSION); fi; \
    if [ -z "$VERSION" ]; then VERSION="dev"; fi; \
    TAGS=""; \
    if [ "$ENABLE_EMBEDUI" = "true" ]; then TAGS="embedui"; fi; \
    if [ "$ENABLE_OTEL" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,otel"; else TAGS="otel"; fi; \
    fi; \
    if [ "$ENABLE_TSNET" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,tsnet"; else TAGS="tsnet"; fi; \
    fi; \
    if [ "$ENABLE_REDIS" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,redis"; else TAGS="redis"; fi; \
    fi; \
    if [ -n "$TAGS" ]; then TAGS="-tags $TAGS"; fi; \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=${VERSION}" \
    ${TAGS} -o /out/goclaw . && \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -o /out/pkg-helper ./cmd/pkg-helper

# ── Stage 2: Runtime ──
FROM alpine:3.23

ARG ENABLE_SANDBOX=false
ARG ENABLE_PYTHON=false
ARG ENABLE_NODE=false
ARG ENABLE_FULL_SKILLS=false
ARG ENABLE_CLAUDE_CLI=false

# Copy pinned Python deps (cleaned up after install).
# requirements-base.txt: shared deps for ENABLE_PYTHON and ENABLE_FULL_SKILLS.
# requirements-skills.txt: additional deps only for ENABLE_FULL_SKILLS.
COPY docker/requirements-base.txt docker/requirements-skills.txt /tmp/

# Install ca-certificates + wget (healthcheck) + optional runtimes.
# ENABLE_FULL_SKILLS=true pre-installs all skill deps (larger image, no on-demand install needed).
# Otherwise, skill packages are installed on-demand via the admin UI.
RUN set -eux; \
    apk add --no-cache ca-certificates wget su-exec; \
    if [ "$ENABLE_SANDBOX" = "true" ]; then \
        apk add --no-cache docker-cli; \
    fi; \
    if [ "$ENABLE_FULL_SKILLS" = "true" ]; then \
        apk add --no-cache python3 py3-pip nodejs npm pandoc github-cli poppler-utils bash; \
        pip3 install --no-cache-dir --break-system-packages \
            -r /tmp/requirements-base.txt -r /tmp/requirements-skills.txt; \
        npm install -g --cache /tmp/npm-cache docx@^9.6.1 pptxgenjs@^4.0.1; \
        rm -rf /tmp/npm-cache /root/.cache /var/cache/apk/*; \
    else \
        if [ "$ENABLE_PYTHON" = "true" ]; then \
            apk add --no-cache python3 py3-pip; \
            pip3 install --no-cache-dir --break-system-packages \
                -r /tmp/requirements-base.txt; \
        fi; \
        if [ "$ENABLE_NODE" = "true" ] || [ "$ENABLE_CLAUDE_CLI" = "true" ]; then \
            apk add --no-cache nodejs npm; \
        fi; \
    fi; \
    if [ "$ENABLE_CLAUDE_CLI" = "true" ]; then \
        npm install -g --cache /tmp/npm-cache @anthropic-ai/claude-code@^2.1.91; \
        rm -rf /tmp/npm-cache; \
    fi; \
    rm -f /tmp/requirements-base.txt /tmp/requirements-skills.txt

# Non-root user
RUN adduser -D -u 1000 -h /app goclaw
WORKDIR /app

# Copy binary, migrations, and bundled skills
COPY --from=builder /out/goclaw /app/goclaw
COPY --from=builder /out/pkg-helper /app/pkg-helper
COPY --from=builder /src/migrations/ /app/migrations/
COPY --from=builder /src/skills/ /app/bundled-skills/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh

# Fix Windows git clone issues:
# 1. CRLF line endings in shell scripts (Windows git adds \r)
# 2. Broken symlinks: On Windows (core.symlinks=false), git creates text files
#    or skips symlinks entirely. Skills like docx/pptx/xlsx need _shared/office
#    module in their scripts/ dir (originally symlinked as scripts/office -> ../../_shared/office).
RUN set -eux; \
    sed -i 's/\r$//' /app/docker-entrypoint.sh; \
    cd /app/bundled-skills; \
    for skill in docx pptx xlsx; do \
        if [ -d "${skill}/scripts" ] && [ ! -d "${skill}/scripts/office" ]; then \
            rm -f "${skill}/scripts/office"; \
            cp -r _shared/office "${skill}/scripts/office"; \
        fi; \
    done

RUN chmod +x /app/docker-entrypoint.sh && \
    chmod 755 /app/pkg-helper && chown root:root /app/pkg-helper

# Create data directories.
# .runtime has split ownership: root owns the dir (so pkg-helper can write apk-packages),
# while pip/npm subdirs are goclaw-owned (runtime installs by the app process).
# Symlink .claude → data volume so Claude CLI credentials persist across container recreates.
RUN mkdir -p /app/workspace /app/data/.runtime/pip /app/data/.runtime/npm-global/lib \
        /app/data/.runtime/pip-cache /app/data/.runtime/bin /app/data/.claude /app/skills \
        /app/tsnet-state /app/.goclaw \
    && ln -s /app/data/.claude /app/.claude \
    && touch /app/data/.runtime/apk-packages \
    && chown -R goclaw:goclaw /app/workspace /app/skills /app/tsnet-state /app/.goclaw \
    && chown goclaw:goclaw /app/bundled-skills /app/data \
    && chown root:goclaw /app/data/.runtime /app/data/.runtime/apk-packages \
    && chmod 0750 /app/data/.runtime \
    && chmod 0640 /app/data/.runtime/apk-packages \
    && chown -R goclaw:goclaw /app/data/.runtime/pip /app/data/.runtime/npm-global /app/data/.runtime/pip-cache /app/data/.runtime/bin /app/data/.claude \
    && chmod 0755 /app/data/.runtime/bin

# Default environment
ENV GOCLAW_CONFIG=/app/config.json \
    GOCLAW_WORKSPACE=/app/workspace \
    GOCLAW_DATA_DIR=/app/data \
    GOCLAW_SKILLS_DIR=/app/skills \
    GOCLAW_MIGRATIONS_DIR=/app/migrations \
    GOCLAW_HOST=0.0.0.0 \
    GOCLAW_PORT=18790

# Entrypoint runs as root to install persisted packages and start pkg-helper,
# then drops to goclaw user via su-exec before starting the app.

EXPOSE 18790

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:18790/health || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["serve"]
