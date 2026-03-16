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
FROM debian:bookworm-slim

ARG ENABLE_SANDBOX=false
ARG ENABLE_DOCKER_BUILD=false
ARG ENABLE_PYTHON=false
ARG ENABLE_NODE=false
ARG ENABLE_KUBECTL=false
ARG ENABLE_CLAUDE_CODE=false
ARG ENABLE_FULL_SKILLS=false

# Install base packages + optional runtimes.
# Debian bookworm-slim provides glibc required by Claude Code native binary.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates wget curl; \
    if [ "$ENABLE_SANDBOX" = "true" ] || [ "$ENABLE_DOCKER_BUILD" = "true" ]; then \
    apt-get install -y --no-install-recommends docker.io; \
    fi; \
    if [ "$ENABLE_FULL_SKILLS" = "true" ]; then \
    apt-get install -y --no-install-recommends python3 python3-pip nodejs npm pandoc gh sudo bash; \
    curl -fsSL https://dl.k8s.io/release/v1.33.0/bin/linux/amd64/kubectl -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl; \
    curl -fsSL https://get.helm.sh/helm-v3.18.0-linux-amd64.tar.gz | tar xz -C /usr/local/bin --strip-components=1 linux-amd64/helm; \
    echo "goclaw ALL=(root) NOPASSWD: /usr/bin/apt-get" > /etc/sudoers.d/goclaw; \
    pip3 install --no-cache-dir --break-system-packages \
    pypdf openpyxl pandas python-pptx markitdown defusedxml lxml; \
    npm install -g --cache /tmp/npm-cache docx pptxgenjs; \
    rm -rf /tmp/npm-cache /root/.cache; \
    else \
    if [ "$ENABLE_PYTHON" = "true" ]; then \
    apt-get install -y --no-install-recommends python3 python3-pip sudo; \
    echo "goclaw ALL=(root) NOPASSWD: /usr/bin/apt-get" > /etc/sudoers.d/goclaw; \
    pip3 install --no-cache-dir --break-system-packages \
        anthropic mcp pdf2image pdfplumber pypdf openpyxl lxml defusedxml; \
    fi; \
    if [ "$ENABLE_NODE" = "true" ]; then \
    apt-get install -y --no-install-recommends nodejs npm; \
    fi; \
    if [ "$ENABLE_KUBECTL" = "true" ]; then \
    curl -fsSL https://dl.k8s.io/release/v1.33.0/bin/linux/amd64/kubectl -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl; \
    curl -fsSL https://get.helm.sh/helm-v3.18.0-linux-amd64.tar.gz | tar xz -C /usr/local/bin --strip-components=1 linux-amd64/helm; \
    fi; \
    fi; \
    rm -rf /var/lib/apt/lists/*

# Install Claude Code: native binary (CLI) + ACP adapter (JSON-RPC 2.0 stdio).
# - Native binary: installed via official installer for claude-cli provider
# - ACP adapter (@zed-industries/claude-agent-acp): wraps Claude Agent SDK for ACP provider
RUN set -eux; \
    if [ "$ENABLE_CLAUDE_CODE" = "true" ]; then \
    curl -fsSL https://claude.ai/install.sh | bash; \
    cp -L /root/.local/bin/claude /usr/local/bin/claude; \
    chmod +x /usr/local/bin/claude; \
    claude --version; \
    rm -rf /root/.claude /root/.local; \
    npm install -g --cache /tmp/npm-cache @zed-industries/claude-agent-acp; \
    rm -rf /tmp/npm-cache; \
    fi

# Non-root user.
# Add to docker group (GID 999) when Docker build is enabled so the goclaw user
# can access /var/run/docker.sock mounted from the host (Docker-outside-of-Docker).
RUN useradd -m -d /app -u 1000 -s /bin/bash goclaw; \
    if [ "$ENABLE_DOCKER_BUILD" = "true" ] || [ "$ENABLE_SANDBOX" = "true" ]; then \
    groupadd -g 999 docker 2>/dev/null || true; \
    usermod -aG docker goclaw; \
    fi
WORKDIR /app

# Copy binary, migrations, and bundled skills
COPY --from=builder /out/goclaw /app/goclaw
COPY --from=builder /src/migrations/ /app/migrations/
COPY --from=builder /src/skills/ /app/bundled-skills/
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# Copy Claude Code auth credentials.
# /etc/claude/ holds seed credentials outside /app so volume mounts don't
# overwrite them. The entrypoint copies into /app/.claude on first run.
COPY editor/claude-code/.claude/ /app/.claude/
COPY editor/claude-code/.claude/ /etc/claude/
COPY editor/claude-code/.claude.json /app/.claude.json

# Create data directories (owned by goclaw user)
RUN mkdir -p /app/workspace /app/data /app/skills /app/tsnet-state /app/.goclaw \
    && chown -R goclaw:goclaw /app

# Default environment
ENV GOCLAW_CONFIG=/app/config.json \
    GOCLAW_WORKSPACE=/app/workspace \
    GOCLAW_DATA_DIR=/app/data \
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
