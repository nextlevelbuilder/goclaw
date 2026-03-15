# Team 01: Content Marketing 📝

> A mini content agency that produces SEO-optimized marketing content through a structured research → write → optimize pipeline.

## GOAL

Produce high-quality, SEO-optimized marketing content (blog posts, articles, social media) by combining deep research, professional writing, and SEO optimization into a seamless automated workflow.

## Team Composition

| Role | Agent Key | Emoji | Responsibility |
|------|-----------|-------|---------------|
| **Lead** | `content-lead` | 🎯 | Content Director — receives briefs, creates tasks, reviews final output |
| Member | `researcher` | 🔍 | Research Analyst — topic research, competitor analysis, data gathering |
| Member | `writer` | ✍️ | Content Writer — drafts content based on research output |
| Member | `seo-editor` | 🔎 | SEO Editor — optimizes for search, formatting, meta tags |

## Orchestration Pattern

**Sequential Pipeline:**

```
User Brief → content-lead
  ├── 1. Create task "Research {topic}" → delegate to researcher
  │   └── researcher delivers: research brief with data, sources, angles
  ├── 2. Create task "Write article" (blocked_by: research) → delegate to writer
  │   └── writer produces: full draft article based on research
  └── 3. Create task "SEO optimize" (blocked_by: writing) → delegate to seo-editor
      └── seo-editor delivers: final optimized article + meta tags
content-lead → synthesizes and presents to user
```

For multi-piece requests (e.g., "Create 5 blog posts"), the lead parallelizes:
- All 5 research tasks run in parallel
- Each writing task starts when its research completes (via `blocked_by`)
- SEO tasks run as articles finish

## Example Prompts

```
Write a comprehensive blog post about "AI Agents in Enterprise" targeting CTOs.
Include data points and real-world examples. Optimize for SEO.
```

```
Create a content series (3 articles) about microservices architecture:
1. What are microservices
2. Migration strategies
3. Best practices and pitfalls
```

```
Research and write a LinkedIn article about the impact of LLMs on software development.
Focus on productivity gains with supporting data.
```

## Deployment

1. Create all 4 agents using `agents/*/agent.json`
2. Upload context files for each agent
3. Create team with `content-lead` as lead, others as members
4. Send content briefs to `content-lead` via any channel
