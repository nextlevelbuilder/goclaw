# Skill Anatomy & Requirements

## Directory Structure

```
~/.goclaw/skills-store/
└── skill-name/           ← directory name IS the slug (unique DB identifier)
    ├── SKILL.md          (required, <300 lines)
    │   ├── YAML frontmatter (name, description required)
    │   └── Markdown instructions
    └── Bundled Resources (optional)
        ├── scripts/      Executable code (Python/Node.js)
        ├── references/   Docs loaded into context as needed
        ├── agents/       Eval agent templates (grader, comparator, analyzer)
        └── assets/       Files used in output (templates, etc.)
```

## Core Requirements

- **SKILL.md:** <300 lines. Concise quick-reference guide.
- **References:** <300 lines each. Split by logical boundaries.
- **Scripts:** No length limit. Must have tests. Must work cross-platform.
- **Description:** <200 chars. Specific triggers, not generic.
- **Consolidation:** Related topics combined (e.g., cloudflare+docker → devops)
- **No duplication:** Info lives in ONE place (SKILL.md OR references, not both)

## SKILL.md Frontmatter

Per [Agent Skills specification](https://agentskills.io/specification.md):

```yaml
---
name: skill-name          # required — must be kebab-case, match directory name
description: Under 1024 chars, specific triggers and use cases  # required
license: Optional         # optional
compatibility: Optional   # optional, environment requirements
metadata:                 # optional, arbitrary key-value pairs
  author: Author Name
  version: "1.0"
allowed-tools: "Bash(python3:*) Bash(node:*)"  # optional, experimental
---
```

**Key:** `name` must be lowercase, kebab-case, and **match the directory name exactly** — GoClaw uses the directory name as the slug (DB key). Changing directory name = new skill.

**Metadata quality** determines auto-activation. See `references/metadata-quality-criteria.md`.

## Scripts (`scripts/`)

- Deterministic code for repeated tasks
- **Prefer:** Python or Node.js (available in GoClaw runtime)
- **Avoid:** Bash scripts for complex logic
- Runtime packages: use `pip3 install <pkg>` or `npm install -g <pkg>` (no sudo needed, persists in `/app/data/.runtime/`)
- Token-efficient: executed without loading into context

See `references/script-quality-criteria.md` for full criteria.

## References (`references/`)

- Documentation loaded as-needed into context
- Use cases: schemas, APIs, workflows, cheatsheets, domain knowledge
- **Best practice:** Split >300 lines into multiple files
- Include grep patterns in SKILL.md for discoverability
- Practical instructions, not educational documentation

## Assets (`assets/`)

- Files used in output, NOT loaded into context
- Use cases: templates, images, icons, boilerplate, fonts
- Separates output resources from documentation

## Progressive Disclosure

Three-level loading for context efficiency:
1. **Metadata** (~200 chars) — always in context
2. **SKILL.md body** (<300 lines) — when skill triggers
3. **Bundled resources** — as needed (scripts: unlimited, execute without loading)

## Writing Style

- **Imperative form:** "To accomplish X, do Y"
- **Third-person metadata:** "This skill should be used when..."
- **Concise:** Sacrifice grammar for brevity in references
- **Practical:** Teach *how* to do tasks, not *what* tools are
