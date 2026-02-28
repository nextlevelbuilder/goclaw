# syntax=docker/dockerfile:1

# ── Stage 1: Build ──
FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build args
ARG ENABLE_OTEL=false
ARG ENABLE_TSNET=false
ARG VERSION=dev

# Build static binary (CGO disabled for scratch/alpine compatibility)
RUN set -eux; \
    TAGS=""; \
    if [ "$ENABLE_OTEL" = "true" ]; then TAGS="otel"; fi; \
    if [ "$ENABLE_TSNET" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,tsnet"; else TAGS="tsnet"; fi; \
    fi; \
    if [ -n "$TAGS" ]; then TAGS="-tags $TAGS"; fi; \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=${VERSION}" \
    ${TAGS} -o /out/goclaw .

# ── Stage 2: Runtime ──
FROM alpine:3.22

ARG ENABLE_SANDBOX=false

# Install ca-certificates + wget (healthcheck) + optionally docker-cli (sandbox)
RUN set -eux; \
    apk add --no-cache ca-certificates wget; \
    if [ "$ENABLE_SANDBOX" = "true" ]; then \
        apk add --no-cache docker-cli; \
    fi

# Non-root user
RUN adduser -D -u 1000 -h /app goclaw
WORKDIR /app

# Copy binary, migrations, and entrypoint to system paths (outside /app data volume)
COPY --from=builder /out/goclaw /usr/local/bin/goclaw
COPY --from=builder /src/migrations/ /usr/share/goclaw/migrations/
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Create data directories (owned by goclaw user) — /app is purely persistent data
RUN mkdir -p /app/workspace /app/data /app/sessions /app/skills /app/tsnet-state /app/.goclaw \
    && chown -R goclaw:goclaw /app

# Default environment
ENV GOCLAW_CONFIG=/app/config.json \
    GOCLAW_WORKSPACE=/app/workspace \
    GOCLAW_DATA_DIR=/app/data \
    GOCLAW_SESSIONS_STORAGE=/app/sessions \
    GOCLAW_SKILLS_DIR=/app/skills \
    GOCLAW_MIGRATIONS_DIR=/usr/share/goclaw/migrations \
    GOCLAW_HOST=0.0.0.0 \
    GOCLAW_PORT=18790

USER goclaw

EXPOSE 18790

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:18790/health || exit 1

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["serve"]
