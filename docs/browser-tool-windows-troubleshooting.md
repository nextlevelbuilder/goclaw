# Browser Tool - Windows Troubleshooting Guide

**Date:** 2026-05-22  
**Status:** RESOLVED  
**Platform:** Windows 10 Pro, GoClaw v3.12.0

## Executive Summary

Browser tool (go-rod/rod) failed on Windows with `context canceled` errors. Root cause: browser object was created with a short-lived context that expired after `Start()` returned. Fixed by using goroutine + channel for connect timeout instead of binding context to browser.

## Problem Symptoms

1. Browser starts successfully (`Browser started successfully`)
2. Status check works (`running: true, tabs: 0`)
3. **All page operations fail** with `context canceled`:
   - `open tab: context canceled`
   - `list pages: context canceled`
   - `navigate: context canceled`
4. Error occurs instantly (~83 microseconds), not after timeout

## Root Cause Analysis

### The Bug

In `pkg/browser/browser.go`, the browser was created with a connect context:

```go
// BUGGY CODE
connectCtx, connectCancel := context.WithTimeout(ctx, 15*time.Second)
defer connectCancel()

b := rod.New().Context(connectCtx).ControlURL(controlURL)
if err := b.Connect(); err != nil { ... }

m.browser = b  // Browser still bound to connectCtx!
```

**Problem:** When `Start()` returns, `defer connectCancel()` cancels the context. Since the browser object is bound to this context, all subsequent operations fail immediately with `context canceled`.

### Why It Worked in Standalone Test

The standalone test (`test_browser.go`) didn't use any context, so the browser defaulted to `context.Background()` which never expires.

## Solution

Replace context-based timeout with goroutine + channel pattern:

```go
// FIXED CODE
b := rod.New().ControlURL(controlURL)

connectDone := make(chan error, 1)
go func() {
    connectDone <- b.Connect()
}()

select {
case err := <-connectDone:
    if err != nil {
        // cleanup and return error
    }
case <-time.After(15 * time.Second):
    // cleanup and return timeout error
case <-ctx.Done():
    // cleanup and return context error
}

m.browser = b  // Browser has no context bound - uses Background
```

## Additional Fixes Applied

### 1. Windows-specific Chrome Flags

```go
l := launcher.New().
    Set("no-sandbox").           // Required on Windows
    Set("disable-gpu").
    Set("no-first-run").
    Set("disable-extensions").
    Set("disable-dev-shm-usage").
    // ... other stability flags
```

### 2. Leakless Disabled

Windows Defender blocks `leakless.exe` (go-rod's zombie process killer).

**Config (`config.json`):**
```json
{
  "tools": {
    "browser": {
      "enabled": true,
      "headless": false,
      "leakless": false,
      "action_timeout_ms": 60000
    }
  }
}
```

### 3. Microsoft Edge Support

Added automatic Edge detection as fallback when Chrome has issues:

```go
func findEdgePath() string {
    paths := []string{
        `C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
        `C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
    }
    for _, p := range paths {
        if _, err := os.Stat(p); err == nil {
            return p
        }
    }
    return ""
}
```

### 4. Isolated User Data Directory

Prevents conflicts with user's personal browser profile:

```go
userDataDir := filepath.Join(os.TempDir(), "goclaw-browser-profile")
l := launcher.New().UserDataDir(userDataDir)
```

### 5. Improved Page Loading

Changed from strict `WaitStable()` to more forgiving `WaitLoad()`:

```go
// Old: WaitStable(300ms) - fails on heavy sites with ads
// New: WaitLoad() + optional WaitStable(500ms)

if err := page.WaitLoad(); err != nil {
    m.logger.Warn("WaitLoad incomplete", "url", url, "error", err)
}
_ = page.WaitStable(500 * time.Millisecond)  // non-fatal
```

## Configuration Checklist for Windows

### config.json
```json
{
  "tools": {
    "browser": {
      "enabled": true,
      "headless": false,
      "leakless": false,
      "action_timeout_ms": 60000
    }
  }
}
```

### Environment Variables
```powershell
$env:GOCLAW_POSTGRES_DSN = "postgres://postgres:postgres@localhost:5433/goclaw?sslmode=disable"
$env:GOCLAW_GATEWAY_TOKEN = "your-token"
$env:GOCLAW_ENCRYPTION_KEY = "your-key"
```

### Prerequisites
- PostgreSQL 18 running on port 5433
- Microsoft Edge or Chrome installed
- Windows Defender exception for goclaw directory (optional, for leakless)

## Debugging Tools

### Trace Query Skill

Created `.claude/skills/trace-query/` for debugging:

```powershell
# List latest traces
.\query-trace.ps1 latest

# Check browser spans
.\query-trace.ps1 browser

# View errors
.\query-trace.ps1 errors
```

### Direct Browser Test

Test go-rod independently of GoClaw:

```powershell
go run test_browser.go
```

## Files Modified

| File | Changes |
|------|---------|
| `pkg/browser/browser.go` | Connect timeout via goroutine, Edge support, isolated profile |
| `pkg/browser/browser_tabs.go` | WaitLoad instead of WaitStable, blank page creation |
| `pkg/browser/browser_page.go` | WaitLoad for Navigate |
| `cmd/gateway_setup.go` | Read leakless config |
| `internal/config/config_channels.go` | Added Leakless field |
| `config.json` | leakless: false, headless: false |

## Troubleshooting Flowchart

```
Browser fails?
    │
    ├─ "Access Denied" on leakless.exe
    │   └─ Set leakless: false in config.json
    │
    ├─ "context canceled" instantly
    │   └─ Check browser.go uses goroutine connect (not Context())
    │
    ├─ Chrome crashes on launch
    │   └─ Try headless: false, or use Edge (auto-detected)
    │
    ├─ Page never loads (timeout)
    │   └─ Increase action_timeout_ms, check network/firewall
    │
    └─ Works in test_browser.go but not GoClaw
        └─ Check context handling in browser.go Start()
```

## Conclusion

The browser tool now works reliably on Windows with:
- Proper context management (no binding to short-lived contexts)
- Edge as fallback browser
- Disabled leakless (Windows Defender compatibility)
- Non-headless mode for stability
- Isolated user profile to avoid conflicts

**Test Result:** Successfully loaded vnexpress.net with full page content and snapshot.
