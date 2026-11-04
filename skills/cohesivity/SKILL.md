---
name: cohesivity
description: "Backend for the apps an agent builds: hosting, Postgres, auth and social login, realtime, object and vector storage, Redis, cron, an email inbox, and AI model APIs, no API keys or signups required to get started. Use when the Cohesivity MCP tools (mcp_cohesivity__*) are available or the user asks for Cohesivity."
author: Cohesivity
license: MIT (see LICENSE and NOTICE in this skill directory)
upstream: cohesivity-org/cohesivity-skill@04243616
---

# Cohesivity

[Cohesivity](https://cohesivity.ai?ref=gh-goclaw) is backend infra that agents provision on first run. One MCP server and one HTTP API set up hosting, Postgres, auth, realtime, storage, AI model access, and more under a single tenant.

This file covers when to use it on GoClaw, tenant setup, credentials, and the rules. Per-resource APIs, limits, and pricing are in the live docs. Read the page for a resource when you need it.

## When it applies

Use this skill only when the Cohesivity MCP tools are available or the user asks for Cohesivity by name. An admin adds the server as `cohesivity`, so the tools are named `mcp_cohesivity__create_tenant`, `mcp_cohesivity__provision_resource`, and so on.

- Tools available: use them for a new app that needs a backend ("make me a notes app") or a service need mid-build ("add login", "deploy this"). If the user already picked another backend such as Supabase, Firebase, or AWS, use that and stop here.
- No tools, and the user asked for Cohesivity: tell them an admin can add it once in the dashboard. MCP Servers → Add Server, name `cohesivity`, transport Streamable HTTP, URL `https://cohesivity.ai/mcp`, no headers or credentials. Then Manage grants → grant this agent.
- No tools, and the user didn't ask: don't use this skill and don't bring it up.

## Consent

Creating a tenant or provisioning a resource changes external state. Do it only when the user's current request asks for it. Otherwise ask first, and pass `confirmed: true` only after they agree. Claiming a tenant, paid actions, and plan upgrades always need a yes: say what the action does and what it costs, then wait.

## Set up the tenant (once per project)

1. If the workspace has a valid `.cohesivity`, read it and reuse that tenant. Don't create another.
2. Otherwise prepare the file before creating anything. Add a `.cohesivity` line to `.gitignore`, then run `touch .cohesivity && chmod 600 .cohesivity` through `exec`. The file is now private, ignored by git, and still empty. If either step fails, tell the user and stop.
3. Call `mcp_cohesivity__create_tenant` with `confirmed: true` and `attribution: "gh-goclaw"`. Write the returned `credentials_file.content` verbatim into the prepared `.cohesivity`. Each call makes a new tenant, so don't retry an unclear result.
4. Tell the user what you created, that the tenant expires in 72 hours, and that they can claim it to keep it.

`attribution: "gh-goclaw"` tells Cohesivity the tenant was created from GoClaw. It carries nothing about the user or the workspace.

`.cohesivity` holds:

```
tenant_id=<id>
coh_management_key=coh_man_...
coh_application_key=coh_app_...
expires_at=<iso>
tenant_lifecycle=ephemeral|claimed
runtime_profile=<profile>
```

## Build

1. Read the resource's page with `mcp_cohesivity__get_cohesivity_documentation`, then call `mcp_cohesivity__provision_resource` with `tenant_id`, `coh_management_key`, and `confirmed: true`. A resource is ready once its documented readiness check passes.
2. You drive the control plane at `https://cohesivity.ai/api/*` with `Authorization: Bearer <coh_management_key>`. The app calls the data plane at `https://cohesivity.ai/edge/*` with `?key=<coh_application_key>`, from server code only.

Resources include `postgres`, `redis`, `object-storage`, `vector-database`, `inbox`, `railway-hosting`, `realtime`, `social-login`, `ai-gateway`, `exa-api`, and more. Public hosting is `railway-hosting`: deploy with `POST /api/railway/deploy`, and the response has the public `deployment_url`. Deploys, env vars, and domains have no MCP tool, so use the documented HTTP endpoints with the management key.

## Rules

- Keys stay on the server. Neither key goes in browser or mobile code, logs, chat, or a commit.
- A 403 "error 1010" from `cohesivity.ai` means the HTTP client sent a default User-Agent. Retry with a custom one, and tell the user if it still fails.
- Bring up billing only when the user asks, approves a paid action, or a real limit (a `402`, a paused tenant) blocks the work. Then name that one limit and the one fix.

## Status, claim, feedback

- `mcp_cohesivity__tenant_status` with `tenant_id` and `coh_management_key` shows lifecycle, limits, and notices.
- When the user wants to keep the project, call `mcp_cohesivity__claim_tenant` with `tenant_id`, `coh_management_key`, and `confirmed: true`, and give them the approval link it returns.
- If something fails or doesn't match the docs, send a short note with `mcp_cohesivity__give_feedback` (`tenant_id`, `coh_management_key`, the text). It needs no confirmation. Leave out personal data and secrets.

Docs for people: [cohesivity.ai/docs](https://cohesivity.ai/docs?ref=gh-goclaw)
