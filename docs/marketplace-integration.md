# GoClaw Hub Marketplace Integration

Discover, search, and install pre-built AI agent teams from GoClaw Hub.

## Configuration

```json
{
  "tools": {
    "marketplace": {
      "api_base": "https://your-hub-api.example.com/v1",
      "api_key": "your-hub-api-key",
      "frontend_base": "https://your-hub.example.com"
    }
  }
}
```

Both `api_base` and `api_key` are required. Without them, marketplace tools are not registered.

### Environment Variable

```bash
export GOCLAW_MARKETPLACE_API_KEY=your-hub-api-key
```

Config file `api_key` takes precedence over the environment variable.

## Tools

### marketplace_search

Search for agent teams. Parameters: `query` (required), `category`, `type`.

### marketplace_hire

Download and install a team. Parameter: `slug` (required). For paid listings, returns a purchase link instead of downloading.

### marketplace_check_updates

Check installed Hub teams for newer versions. No parameters.

## Web Install (Hub-initiated)

Hub generates a signed URL pointing to your GoClaw instance:

```
GET /marketplace/install?slug=...&title=...&agents=...&bundle_url=...&sig=...&ts=...
```

- HMAC-SHA256 signature prevents tampering
- Links expire after 15 minutes (`ts` parameter)
- Admin sees confirmation page before install
- Already-installed teams show "Update" instead of "Install"

## Security

- No Hub credentials stored in GoClaw — only the shared registry key
- Bundle downloads authenticated via `X-Registry-Key` header
- SSRF protection: validates download URLs, resolves DNS, blocks private IPs
- Bundle validation: checks gzip + tar structure, blocks path traversal and symlinks
- Install links signed with HMAC-SHA256, expire after 15 minutes
- Registry key never exposed in URLs or HTML

## Version Tracking

When a team is installed from Hub, `hub_slug` and `hub_version` are saved to the team's `settings` JSONB. The `marketplace_check_updates` tool queries these to detect available updates.

## Registry Client API

```go
client := registry.NewClient("https://hub-api.example.com/v1", "api-key")

listings, _ := client.Search(ctx, "web development", "", "team")
listing, _ := client.GetListing(ctx, "team-slug")
reader, _ := client.Download(ctx, "team-slug", "1.0.0")
info, _ := client.CheckUpdate(ctx, "team-slug", 1)
categories, _ := client.Categories(ctx)
```

## Troubleshooting

| Issue | Fix |
|-------|-----|
| "marketplace tools disabled" | Set both `api_base` and `api_key` in config |
| "install link expired" | Generate a new link from Hub (links valid 15 min) |
| "invalid signature" | Ensure GoClaw and Hub use the same registry key |
| Search returns nothing | Try broader terms, check Hub has published listings |
