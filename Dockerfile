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
    ${TAGS} -o /out/goclaw . && \
    CGO_ENABLED=0 GOOS=linux \
    go build -ldflags="-s -w" -o /out/pkg-helper ./cmd/pkg-helper

# ── Stage 2: Runtime ──
FROM alpine:3.22

ARG ENABLE_SANDBOX=false
ARG ENABLE_PYTHON=false
ARG ENABLE_NODE=false
ARG ENABLE_FULL_SKILLS=false

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
            pypdf openpyxl pandas python-pptx markitdown defusedxml lxml \
            pdfplumber pdf2image anthropic; \
        npm install -g --cache /tmp/npm-cache docx pptxgenjs; \
        rm -rf /tmp/npm-cache /root/.cache /var/cache/apk/*; \
    else \
        if [ "$ENABLE_PYTHON" = "true" ]; then \
            apk add --no-cache python3 py3-pip; \
            pip3 install --no-cache-dir --break-system-packages edge-tts; \
        fi; \
        if [ "$ENABLE_NODE" = "true" ]; then \
            apk add --no-cache nodejs npm; \
        fi; \
    fi

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

# Create data directories (owned by goclaw user).
# Binaries and entrypoint stay root-owned (readable by all).
RUN mkdir -p /app/workspace /app/data /app/skills /app/tsnet-state /app/.goclaw \
    && chown -R goclaw:goclaw /app/workspace /app/data /app/skills /app/tsnet-state /app/.goclaw \
    && chown goclaw:goclaw /app/bundled-skills

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
