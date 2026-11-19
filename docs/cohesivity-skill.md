# Cohesivity Skill

Bundled system skill that lets GoClaw agents set up a backend for the apps they build through [Cohesivity](https://cohesivity.ai?ref=gh-goclaw): hosting, Postgres, auth and social login, realtime, object and vector storage, Redis, cron, an email inbox, and AI model APIs.

Skill path: `skills/cohesivity/` (seeded as a system skill on gateway startup). Adapted from [cohesivity-org/cohesivity-skill](https://github.com/cohesivity-org/cohesivity-skill) under the MIT license; the pinned commit is in `skills/cohesivity/UPSTREAM.txt`.

The skill does nothing until an admin adds the Cohesivity MCP server. Before that, the agent uses it only when a user asks for Cohesivity by name, and then only to point them to the setup below.

## What you can do

| Ask the agent | What happens |
|---------------|--------------|
| "Build me a notes app with a database and deploy it" | Creates a tenant, provisions Postgres and hosting, deploys, returns the public URL |
| "Add login" / "send a confirmation email" | Provisions `social-login` or `inbox` on the existing tenant |
| "Keep this project" | Calls `claim_tenant` and hands you an approval link |

The first tenant is free and ephemeral: no signup or API key, and it expires after 72 hours unless you claim it. The agent creates a tenant or provisions a resource only when your request asks for it, and asks you first otherwise.

## Add the MCP server (once)

1. Dashboard → **MCP Servers** → **Add Server**.
2. Name `cohesivity`, transport **Streamable HTTP**, URL `https://cohesivity.ai/mcp`. No headers or credentials.
3. **Manage grants** → grant the agents that should use it.

The agent then sees `mcp_cohesivity__create_tenant`, `mcp_cohesivity__provision_resource`, `mcp_cohesivity__tenant_status`, `mcp_cohesivity__claim_tenant`, `mcp_cohesivity__give_feedback`, and `mcp_cohesivity__get_cohesivity_documentation`. The skill runs nothing from npm.

To turn the skill off, use **Disable skill** on `cohesivity` in the Skills page, or set a per-tenant override there.

## Credentials

Before it writes any keys, the agent adds `.cohesivity` to `.gitignore` and creates the file with mode 0600. The file sits in the agent workspace. GoClaw's backup and agent export both skip dot-files, so it is left out of both. The keys work only for that one tenant, and the skill tells the agent to reuse the file and never print or commit them.

## What Cohesivity records

When the agent creates a tenant it passes `attribution: "gh-goclaw"`, and Cohesivity stores that with the MCP client name as `goclaw.gh-goclaw`. Links to cohesivity.ai in the skill and this page carry `?ref=gh-goclaw`. Both only tell Cohesivity that a tenant or a visit came from GoClaw. Neither includes anything about the user, the agent, or the workspace.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Agent says no Cohesivity tools are available | Add the MCP server and grant the agent |
| HTTP 403 "error 1010" | A request used a default HTTP client User-Agent. The agent retries with a custom one and tells you if it still fails |
| Tenant paused | An ephemeral limit was reached. Ask the agent to check `tenant_status` |

## More

- Cohesivity docs: [cohesivity.ai/docs](https://cohesivity.ai/docs?ref=gh-goclaw)
- MCP server source: [cohesivity-org/cohesivity-mcp](https://github.com/cohesivity-org/cohesivity-mcp)
