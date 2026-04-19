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

## Workflow: Screenshot → Public Download Link

After taking a screenshot, always create a shareable download link:

```bash
# 1. Take screenshot — save to workspace directory
shot-scraper https://example.com -o /app/workspace/screenshots/example-$(date +%s).png --full-page

# 2. Create signed public URL via GoClaw API
curl -s -X POST http://localhost:18790/v1/files/sign \
  -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"path": "/app/workspace/screenshots/example-1234567890.png"}'
# Returns: {"url": "/v1/files/app/workspace/screenshots/example-1234567890.png?ft=signed_token"}

# 3. Build full public URL
# https://<GOCLAW_SITE_URL>/v1/files/app/workspace/screenshots/example-1234567890.png?ft=signed_token
```

**IMPORTANT:** Always follow this 3-step flow:
1. `shot-scraper` → save file to workspace
2. Sign the file path via `/v1/files/sign` API
3. Return the full public URL to the user so they can click to download

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
- Always save to workspace dir so the file is accessible via GoClaw file API
- Always create a signed URL and return a clickable public link to the user
