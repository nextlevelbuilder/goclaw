# Team 03: Customer Support 🎧

> An automated support team that triages tickets, handles common queries, escalates complex issues, and continuously improves the knowledge base.

## GOAL

Resolve customer support tickets efficiently through a tiered system: Tier-1 handles common questions, complex cases escalate to specialists, and all solutions feed back into a growing knowledge base.

## Team Composition

| Role | Agent Key | Emoji | Responsibility |
|------|-----------|-------|---------------|
| **Lead** | `support-lead` | 📋 | Support Manager — triages, routes, quality-checks responses |
| Member | `tier1-agent` | 💬 | Tier-1 Support — handles FAQ, common issues |
| Member | `knowledge-curator` | 📚 | Knowledge Curator — maintains and updates knowledge base articles |
| Member | `escalation-handler` | 🔥 | Escalation Handler — deep technical/complex issue resolution |

## Orchestration Pattern

**Sequential with routing:**
```
Ticket → support-lead (triage)
  ├── Common issue → tier1-agent → resolve
  ├── Complex/technical → escalation-handler → resolve
  └── New pattern found → knowledge-curator → update KB
```

## Example Prompts

```
A customer reports they can't log in after password reset.
Error message: "Invalid token". They've tried 3 times.
```

```
Customer asks: "How do I integrate your API with our CRM?"
They're using Salesforce and need webhook configuration help.
```
