---
name: shot-scraper
description: Use this skill when the user wants to take screenshots of websites or web pages. Supports full-page capture, element selection via CSS selectors, custom viewport sizes, JavaScript execution before capture, PDF export, and multi-URL batch screenshots. Activate when user mentions screenshot, capture, webpage image, or web page snapshot.
dependencies:
  - pip:shot-scraper
  - pip:playwright
---

# Shot-Scraper Skill

## Overview

`shot-scraper` is a CLI tool for taking automated screenshots of websites using Playwright (Chromium).

## Installation

```bash
pip install shot-scraper
shot-scraper install  # downloads Chromium browser
```

## Sharing Mode

After taking a screenshot, share it via one of two modes. Check environment variables to determine which mode to use:

### Mode 1: GoClaw Signed URL (default)

Use when `GOCLAW_GATEWAY_TOKEN` and `GOCLAW_SITE_URL` are set. No extra config needed.

```bash
# 1. Take screenshot — save to workspace
FILENAME="screenshot-$(date +%s).png"
shot-scraper https://example.com -o "$GOCLAW_WORKSPACE_DIR/screenshots/$FILENAME" --full-page

# 2. Sign the file path to get a public token
SIGN_RESPONSE=$(curl -s -X POST http://localhost:${GOCLAW_PORT:-18790}/v1/files/sign \
  -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"path\": \"$GOCLAW_WORKSPACE_DIR/screenshots/$FILENAME\"}")

# 3. Extract URL path and build full public link
URL_PATH=$(echo "$SIGN_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['url'])")
PUBLIC_URL="${GOCLAW_SITE_URL}${URL_PATH}"

echo "Download: $PUBLIC_URL"
```

### Mode 2: S3 Upload

Use when S3 environment variables are configured. Supports AWS S3, MinIO, Cloudflare R2, DigitalOcean Spaces, or any S3-compatible service.

**Required env vars:**
- `SCREENSHOT_S3_BUCKET` — bucket name
- `SCREENSHOT_S3_REGION` — region (default: us-east-1)
- `AWS_ACCESS_KEY_ID` — access key
- `AWS_SECRET_ACCESS_KEY` — secret key
- `SCREENSHOT_S3_ENDPOINT` — custom endpoint for S3-compatible services (optional)
- `SCREENSHOT_S3_PREFIX` — key prefix (default: screenshots/)
- `SCREENSHOT_S3_PUBLIC_URL` — base URL for public access (e.g. https://cdn.example.com)

```bash
# 1. Take screenshot
FILENAME="screenshot-$(date +%s).png"
shot-scraper https://example.com -o "/tmp/$FILENAME" --full-page

# 2. Upload to S3
S3_KEY="${SCREENSHOT_S3_PREFIX:-screenshots/}$FILENAME"

# For AWS S3:
aws s3 cp "/tmp/$FILENAME" "s3://${SCREENSHOT_S3_BUCKET}/${S3_KEY}" --acl public-read

# For S3-compatible (MinIO, R2, DO Spaces):
aws s3 cp "/tmp/$FILENAME" "s3://${SCREENSHOT_S3_BUCKET}/${S3_KEY}" \
  --endpoint-url "$SCREENSHOT_S3_ENDPOINT" --acl public-read

# 3. Build public URL
if [ -n "$SCREENSHOT_S3_PUBLIC_URL" ]; then
  PUBLIC_URL="${SCREENSHOT_S3_PUBLIC_URL}/${S3_KEY}"
else
  PUBLIC_URL="https://${SCREENSHOT_S3_BUCKET}.s3.${SCREENSHOT_S3_REGION:-us-east-1}.amazonaws.com/${S3_KEY}"
fi

echo "Download: $PUBLIC_URL"

# 4. Clean up local file
rm -f "/tmp/$FILENAME"
```

### Mode Selection Logic

Use this decision flow:
1. If `SCREENSHOT_S3_BUCKET` is set → use **S3 mode**
2. Otherwise → use **GoClaw signed URL mode** (default)

Always return the public URL to the user as a clickable link.

## Basic Usage

```bash
# Screenshot a URL (default: viewport-sized PNG)
shot-scraper https://example.com -o output.png

# Full-page screenshot
shot-scraper https://example.com -o full.png --full-page

# Custom viewport size
shot-scraper https://example.com -o mobile.png --width 375 --height 812

# JPEG with quality
shot-scraper https://example.com -o photo.jpg --quality 80
```

## Element Selection

```bash
# Capture specific element by CSS selector
shot-scraper https://example.com -s "h1" -o header.png
shot-scraper https://example.com -s ".main-content" -o content.png

# Add padding around element
shot-scraper https://example.com -s "h1" --padding 20 -o padded.png
```

## JavaScript Execution

```bash
# Execute JS before screenshot (dismiss modals, scroll, click)
shot-scraper https://example.com -o clean.png \
  --javascript "document.querySelector('.cookie-banner')?.remove()"

# Wait for content to load
shot-scraper https://example.com -o loaded.png \
  --javascript "await new Promise(r => setTimeout(r, 3000))"
```

## Wait Options

```bash
# Wait for specific element to appear
shot-scraper https://example.com -o ready.png --wait-for "article.loaded"

# Wait milliseconds before capture
shot-scraper https://example.com -o delayed.png --wait 3000
```

## PDF Export

```bash
shot-scraper pdf https://example.com -o page.pdf
shot-scraper pdf https://example.com -o page.pdf --landscape
```

## Multi-URL Batch

Create a YAML file `shots.yml`:

```yaml
- url: https://example.com
  output: example.png
  full_page: true
- url: https://github.com
  output: github.png
  width: 1200
  height: 800
```

Then run:

```bash
shot-scraper multi shots.yml
```

## HTML to Screenshot

```bash
# Screenshot from HTML string
echo "<h1>Hello</h1>" | shot-scraper html -o hello.png

# From HTML file
shot-scraper html -i page.html -o output.png
```

## Common Options

| Flag | Description |
|------|-------------|
| `-o FILE` | Output file path |
| `--full-page` | Capture entire scrollable page |
| `-s SELECTOR` | CSS selector for specific element |
| `--width N` | Viewport width (default: 1280) |
| `--height N` | Viewport height (default: 720) |
| `--quality N` | JPEG quality 0-100 |
| `--wait N` | Wait N ms before capture |
| `--wait-for SEL` | Wait for CSS selector to appear |
| `--javascript JS` | Run JS before screenshot |
| `--padding N` | Padding around selected element |
| `--retina` | High-DPI (2x) screenshot |

## Tips

- Default output is PNG; use `.jpg` extension for JPEG
- For SPAs, use `--wait` or `--javascript` to ensure content loads
- Use `--full-page` for long pages, otherwise only viewport is captured
- Always return a clickable public download link to the user
- S3 mode is best for permanent links; GoClaw signed URLs expire after a set TTL
