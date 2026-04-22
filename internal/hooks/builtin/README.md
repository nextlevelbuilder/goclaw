# Builtin Hooks

Canonical, embed-shipped hook rows. Users may toggle `enabled` but cannot
edit content — the embedded `.js` is overwritten on every boot.

## Adding a new builtin

1. Pick a stable kebab identifier (`pii-redactor`, `sql-guard`). Never rename:
   the DB primary key is `UUIDv5(namespace, id + "/" + event)` and renaming
   would orphan any existing rows.
2. Drop `my-feature.js` alongside `builtins.yaml`. The script sees the
   [goja](https://github.com/dop251/goja) sandboxed globals `event` and
   `result`, plus the usual `JSON.*`, `Math.*`. No network, no I/O, no timers.
3. Add an entry to `builtins.yaml`:

   ```yaml
   - id: my-feature
     version: 1
     events: [pre_tool_use]
     scope: global
     timeout_ms: 200
     on_timeout: allow
     priority: 500
     mutable_fields: [toolInput.command]
     source_file: my-feature.js
     description: "One-line user-facing summary."
   ```

4. Bump `version` whenever the JS content changes. On next boot, matching
   rows get UPDATEd in-place (the user's `enabled` toggle is preserved).
5. Remove `_placeholder.js` once this is the first real `.js` in the package.

## Operator escape hatch

Operators can force-disable any builtin by id via `hooks.builtin_disable` in
`config.json` — useful when a builtin misbehaves before the next release:

```json
{
  "hooks": {
    "builtin_disable": ["pii-redactor"]
  }
}
```

The row stays in the DB (history preserved); only `enabled=false` is applied.

## Version reconciliation rules

| Embed version | DB version | Outcome               |
|---------------|------------|------------------------|
| missing row   | —          | INSERT, `enabled=true` |
| newer         | older      | UPDATE content; keep user's `enabled` toggle |
| equal         | equal      | no-op |
| older         | newer      | WARN log; keep DB (no rollback) |

A downgrade happens when someone rolls back the binary but the DB has already
been written by a newer version. We never destructively rewrite; operators
must resolve manually.
