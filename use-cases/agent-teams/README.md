# Agent Teams — Use Cases

Pre-built agent team configurations for common multi-agent workflows. Each team follows a **mini-company model**: one Lead agent (director) orchestrates specialist Members toward shared GOALs.

## Available Teams

| # | Team | Lead | Members | Pattern | GOAL |
|---|------|------|---------|---------|------|
| 01 | [Content Marketing](./01-content-marketing/) | Content Director | Researcher, Writer, SEO Editor | Sequential | Produce high-quality marketing content |
| 02 | [Software Development](./02-software-dev/) | Tech Lead | Backend Dev, Frontend Dev, QA Engineer | Mixed (parallel + review) | Develop software features end-to-end |
| 03 | [Customer Support](./03-customer-support/) | Support Manager | Tier-1 Agent, Knowledge Curator, Escalation Handler | Sequential + routing | Resolve support tickets efficiently |
| 04 | [Data Analytics](./04-data-analytics/) | Analytics Director | Data Collector, Analyst, Report Builder | Sequential | Turn questions into actionable insights |
| 05 | [Product Launch](./05-product-launch/) | Launch Manager | Market Researcher, Copywriter, Campaign Planner | Mixed | Plan & execute product launches |

## How Teams Work

```
User → Lead Agent → creates tasks on Task Board → delegates to Members
Members work independently → results auto-announced → Lead synthesizes → User
```

Key concepts:
- **TEAM.md** is auto-injected into all agents' system prompts with role-appropriate instructions
- **Task Board** tracks all work with status, dependencies, and atomic claiming
- **Mailbox** enables direct/broadcast communication between agents
- **Workspace** provides shared file storage for deliverables

## Structure

```
{team-name}/
├── README.md          # Overview, goals, roles, orchestration, example prompts
├── team.json          # Team creation config (name, description, settings)
└── agents/
    ├── {lead-role}/
    │   ├── agent.json          # Agent creation payload (POST /v1/agents)
    │   └── context-files/
    │       ├── SOUL.md         # Identity, expertise, workflow, boundaries
    │       ├── IDENTITY.md     # Name, emoji, role, traits
    │       └── AGENTS.md       # Operating rules, tool usage
    └── {member-role}/
        ├── agent.json
        └── context-files/
            ├── SOUL.md
            ├── IDENTITY.md
            └── AGENTS.md
```

## How to Deploy

### Step 1: Create Agents

```bash
# Create each agent in the team
for agent_dir in use-cases/agent-teams/{team}/agents/*/; do
  curl -X POST http://localhost:3000/v1/agents \
    -H "Authorization: Bearer $GOCLAW_GATEWAY_TOKEN" \
    -H "Content-Type: application/json" \
    -d @"${agent_dir}agent.json"
done
```

Then upload each agent's context files via the Web Dashboard (Agents > select agent > Context Files tab).

### Step 2: Create Team

1. Go to **Teams** page in Web Dashboard
2. Click **Create Team**
3. Set team name, description, and lead agent from `team.json`
4. Add member agents
5. The system auto-creates delegation links and injects TEAM.md

### Step 3: Test

Send a message to the **lead agent** via any channel (Web Chat, Telegram, etc.). The lead will orchestrate members automatically.

## Design Principles

1. **Lead-centric**: Only the lead receives full orchestration instructions. Members focus on execution.
2. **Goal-driven**: Each team's SOUL.md defines clear GOALs that guide all behavior.
3. **Mandatory task tracking**: Every delegation is linked to a task on the board.
4. **Parallel by default**: Where possible, members work simultaneously.
5. **Auto-completion**: Delegation results auto-complete linked tasks — no manual bookkeeping.
