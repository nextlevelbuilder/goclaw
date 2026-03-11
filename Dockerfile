# syntax=docker/dockerfile:1

# ── Stage 1: Build ──
FROM golang:1.26-bookworm AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build args
ARG ENABLE_OTEL=false
ARG ENABLE_TSNET=false
ARG ENABLE_REDIS=false
ARG VERSION=dev

# Build static binary (CGO disabled for scratch/alpine compatibility)
RUN set -eux; \
    TAGS=""; \
    if [ "$ENABLE_OTEL" = "true" ]; then TAGS="otel"; fi; \
    if [ "$ENABLE_TSNET" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,tsnet"; else TAGS="tsnet"; fi; \
    fi; \
    if [ "$ENABLE_REDIS" = "true" ]; then \
        if [ -n "$TAGS" ]; then TAGS="$TAGS,redis"; else TAGS="redis"; fi; \
    fi; \
    if [ -n "$TAGS" ]; then TAGS="-tags $TAGS"; fi; \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w -X github.com/nextlevelbuilder/goclaw/cmd.Version=${VERSION}" \
    ${TAGS} -o /out/goclaw .

# ── Stage 2: Runtime ──
FROM alpine:3.22

ARG ENABLE_SANDBOX=false
ARG ENABLE_PYTHON=false

# Install ca-certificates + wget (healthcheck) + optionally docker-cli (sandbox)
# + python3 with common skill packages + nodejs for JS-based skills
RUN set -eux; \
    apk add --no-cache ca-certificates wget; \
    if [ "$ENABLE_SANDBOX" = "true" ]; then \
        apk add --no-cache docker-cli; \
    fi; \
    if [ "$ENABLE_PYTHON" = "true" ]; then \
        apk add --no-cache python3 py3-pip nodejs npm pandoc github-cli; \
        pip3 install --break-system-packages \
            pypdf openpyxl pandas python-pptx markitdown; \
        npm install -g docx pptxgenjs; \
    fi

# Non-root user
RUN adduser -D -u 1000 -h /app goclaw
WORKDIR /app

# Copy binary, migrations, and bundled skills
COPY --from=builder /out/goclaw /app/goclaw
COPY --from=builder /src/migrations/ /app/migrations/
COPY --from=builder /src/skills/ /app/bundled-skills/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Create data directories (owned by goclaw user)
RUN mkdir -p /app/workspace /app/data /app/sessions /app/skills /app/tsnet-state /app/.goclaw \
    && chown -R goclaw:goclaw /app

# Default environment
ENV GOCLAW_CONFIG=/app/config.json \
    GOCLAW_WORKSPACE=/app/workspace \
    GOCLAW_DATA_DIR=/app/data \
    GOCLAW_SESSIONS_STORAGE=/app/sessions \
    GOCLAW_SKILLS_DIR=/app/skills \
    GOCLAW_MIGRATIONS_DIR=/app/migrations \
    GOCLAW_HOST=0.0.0.0 \
    GOCLAW_PORT=18790

USER goclaw

EXPOSE 18790

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:18790/health || exit 1

ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["serve"]
