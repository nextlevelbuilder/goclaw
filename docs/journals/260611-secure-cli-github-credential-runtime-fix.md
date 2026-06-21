# Secure CLI GitHub Credential Runtime Fix

## Summary

Fixed GitHub-related SecureCLI auth diagnostics for `git` and `gh` runtime
commands linked to digitopvn/goclaw#138 and digitopvn/goclaw#151.

## Root Cause

- Adapter-managed `git` remote commands could reach raw Git when no typed PAT
  or SSH credential was selected.
- Required env validation only covered `rapidapi`, so credentialed `gh`
  commands without `GH_TOKEN` could reach raw GitHub CLI auth guidance.

## Changes

- Added fail-closed git credential readiness validation before adapter prepare.
- Generalized required env checks to preset non-optional env vars.
- Added regression tests for missing git typed credentials and missing
  `GH_TOKEN`.
- Updated git adapter docs and project changelog.

## Validation

- `/usr/local/go/bin/go test ./internal/tools`
- `/usr/local/go/bin/go build ./...`
- `/usr/local/go/bin/go build -tags sqliteonly ./...`

## Unresolved Questions

None.
