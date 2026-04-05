#!/usr/bin/env bash
# GoClaw Lite (Desktop) installer — downloads the latest .app from GitHub Releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nextlevelbuilder/goclaw/main/scripts/install-lite.sh | bash
#   curl -fsSL ... | bash -s -- --version lite-v0.1.0
#
# macOS only. Windows users: download .zip from GitHub Releases.

set -euo pipefail

REPO="nextlevelbuilder/goclaw"
INSTALL_DIR="/Applications"
VERSION=""

# ── Parse args ──
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --help|-h)
      echo "Usage: install-lite.sh [--version lite-v1.0.0]"
      echo "  Downloads and installs GoClaw Lite desktop app to /Applications/"
      exit 0
      ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Detect OS/arch ──
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
esac

if [[ "$OS" != "darwin" ]]; then
  echo "❌ This installer is for macOS only."
  echo ""
  echo "For Windows: download .zip from https://github.com/$REPO/releases"
  echo "For Linux:   not yet supported"
  exit 1
fi

# ── Resolve version ──
if [[ -z "$VERSION" ]]; then
  echo "→ Fetching latest desktop release..."
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=100" \
    | grep '"tag_name": "lite-v' \
    | head -1 \
    | sed 's/.*"tag_name": "\(lite-v[^"]*\)".*/\1/' || true)

  if [[ -z "$VERSION" ]]; then
    echo "❌ No desktop release found. Check https://github.com/$REPO/releases"
    exit 1
  fi
fi

SEMVER="${VERSION#lite-v}"
echo "→ Installing GoClaw Lite v${SEMVER} (${ARCH})..."

# ── Download ──
ASSET="goclaw-lite-${SEMVER}-darwin-${ARCH}.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "→ Downloading $URL..."
if ! curl -fSL --progress-bar "$URL" -o "$TMPDIR/$ASSET"; then
  echo "❌ Download failed. Check the version and try again."
  echo "   Available releases: https://github.com/$REPO/releases"
  exit 1
fi

# ── Extract ──
echo "→ Extracting..."
tar xzf "$TMPDIR/$ASSET" -C "$TMPDIR"

if [[ ! -d "$TMPDIR/goclaw-lite.app" ]]; then
  echo "❌ Archive does not contain goclaw-lite.app"
  exit 1
fi

# ── Install ──
TARGET="$INSTALL_DIR/goclaw-lite.app"
if [[ -d "$TARGET" ]]; then
  echo "→ Removing existing installation..."
  rm -rf "$TARGET"
fi

echo "→ Installing to $TARGET..."
cp -R "$TMPDIR/goclaw-lite.app" "$TARGET"

# Remove quarantine attribute (unsigned app)
xattr -rd com.apple.quarantine "$TARGET" 2>/dev/null || true

echo ""
echo "✅ GoClaw Lite v${SEMVER} installed to $TARGET"
echo ""
echo "→ Launching GoClaw Lite..."
open "$TARGET"
