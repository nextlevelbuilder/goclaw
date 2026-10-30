# Cohesivity Skill

Bundled system skill that lets GoClaw agents create a backend for the apps they build: hosting, Postgres, auth and social login, realtime, object and vector storage, Redis, cron, an email inbox, and AI model APIs through [Cohesivity](https://cohesivity.ai?ref=gh-goclaw).

Skill path: `skills/cohesivity/` (seeded as a system skill on gateway startup).

## What you can do

| Ask the agent | What happens |
|---------------|--------------|
| "Build me a notes app with a database and deploy it" | Creates a tenant, provisions Postgres and hosting, deploys, returns the public URL |
| "Add login" / "send a confirmation email" | Provisions `social-login` or `inbox` on the existing tenant |
| "Keep this project" | Calls `claim_tenant` and hands you an approval link |

The first tenant is free and ephemeral: no signup or API key, expires after 72 hours unless you claim it. The agent asks before creating anything.

## Prerequisites

The skill has no dependencies. It needs one of these to create a tenant:

| Path | Requirement |
|------|-------------|
| Hosted MCP (recommended) | An admin adds the Cohesivity MCP server once (below) |
| Installer | `npx` on the host. Docker `full` image or desktop / binary with Node.js |

## Add the MCP server (once)

1. Dashboard → **MCP Servers** → **Add Server**.
2. Name `cohesivity`, transport **Streamable HTTP**, URL `https://cohesivity.ai/mcp`. No headers or credentials.
3. **Manage grants** → grant the agents that should use it.

The agent then sees `mcp_cohesivity__create_tenant`, `mcp_cohesivity__provision_resource`, `mcp_cohesivity__tenant_status`, `mcp_cohesivity__claim_tenant`, `mcp_cohesivity__give_feedback`, and `mcp_cohesivity__get_cohesivity_documentation`.

## Enable the skill

Bundled system skills are public and seeded automatically. In an agent session:

- Start a prompt with `/cohesivity` or `/use cohesivity`, **or**
- Ask for an app that needs a database, login, storage, or a deploy.

## Credentials

Tenant credentials go in `.cohesivity` in the agent workspace (mode 0600, gitignored). The skill tells the agent to reuse that file instead of creating a second tenant, and never to print or commit the keys.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Agent says no Cohesivity tools are available | Add the MCP server and grant the agent, or use an image with Node.js |
| HTTP 403 "error 1010" | The request used a default HTTP client User-Agent. Set any custom one |
| Tenant paused | An ephemeral cap was reached. Ask the agent to check `tenant_status` |

## More

- Cohesivity docs: [cohesivity.ai/docs](https://cohesivity.ai/docs?ref=gh-goclaw)
- MCP server source: [cohesivity-org/cohesivity-mcp](https://github.com/cohesivity-org/cohesivity-mcp)
