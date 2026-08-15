#!/usr/bin/env bash
# GoClaw installer — downloads the latest binary from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nextlevelbuilder/goclaw/main/scripts/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v1.30.0
#   curl -fsSL ... | bash -s -- --dir /opt/goclaw
#
# Supported: Linux (amd64/arm64), macOS (amd64/arm64)

set -euo pipefail

REPO="nextlevelbuilder/goclaw"
INSTALL_DIR="${GOCLAW_INSTALL_DIR:-/usr/local/bin}"
MIGRATIONS_DIR="/usr/local/share/goclaw/migrations"
VERSION=""

# ── Parse args ──
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --dir)     INSTALL_DIR="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: install.sh [--version v1.x.x] [--dir /path]"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Detect OS/arch ──
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

# ── Resolve version ──
if [ -z "$VERSION" ]; then
  echo "Fetching latest release..."
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"
fi
echo "Installing GoClaw ${VERSION} (${OS}/${ARCH})..."

# ── Download ──
ASSET="goclaw-${VERSION#v}-${OS}-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "Downloading ${URL}..."
curl -fsSL -o "${TMP}/${ASSET}" "$URL"

# ── Extract & install ──
tar -xzf "${TMP}/${ASSET}" -C "$TMP"

# Check write permission, use sudo if needed
if [ -w "$INSTALL_DIR" ]; then
  cp "${TMP}/goclaw" "${INSTALL_DIR}/goclaw"
  chmod +x "${INSTALL_DIR}/goclaw"
  mkdir -p "${MIGRATIONS_DIR}"
  cp -r "${TMP}/migrations/"* "${MIGRATIONS_DIR}/"
else
  echo "Installing to ${INSTALL_DIR} (requires sudo)..."
  sudo cp "${TMP}/goclaw" "${INSTALL_DIR}/goclaw"
  sudo chmod +x "${INSTALL_DIR}/goclaw"
  sudo mkdir -p "${MIGRATIONS_DIR}"
  sudo cp -r "${TMP}/migrations/"* "${MIGRATIONS_DIR}/"
fi

echo ""
echo "GoClaw ${VERSION} installed to ${INSTALL_DIR}/goclaw"
echo "Migrations installed to ${MIGRATIONS_DIR}"
echo ""
echo "The binary includes an embedded web dashboard — no separate nginx needed."
echo ""
echo "Next steps:"
echo "  1. Set up PostgreSQL (pgvector):"
echo "     docker run -d --name goclaw-pg -p 5432:5432 -e POSTGRES_PASSWORD=goclaw pgvector/pgvector:pg18"
echo ""
echo "  2. Set environment variables:"
echo "     export GOCLAW_POSTGRES_DSN='postgres://postgres:goclaw@localhost:5432/postgres?sslmode=disable'"
echo "     export GOCLAW_MIGRATIONS_DIR='${MIGRATIONS_DIR}'"
echo ""
echo "  3. Start the onboard wizard (runs migrations automatically):"
echo "     goclaw onboard"
echo ""
echo "  4. Start the gateway:"
echo "     source .env.local && goclaw"
echo ""
echo "  Web dashboard: http://localhost:18790"
echo "  Health check:  curl http://localhost:18790/health"
echo ""
echo "  To update later: goclaw update --apply"
