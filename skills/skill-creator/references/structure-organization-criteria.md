# Structure & Organization Criteria

Proper structure enables discovery and maintainability.

## Required Directory Layout

Skills are created directly in `~/.goclaw/skills-store/<skill-name>/`.
After writing SKILL.md, use `publish_skill` to register in the DB (version managed by GoClaw).

```
~/.goclaw/skills-store/
└── skill-name/          ← directory name = slug (DB key)
    ├── SKILL.md         # Required, exact uppercase filename
    ├── scripts/         # Optional: executable code
    ├── references/      # Optional: documentation
    └── assets/          # Optional: output resources
```

## SKILL.md Requirements

**File name:** Exactly `SKILL.md` (case-sensitive)

**YAML Frontmatter** per [Agent Skills spec](https://agentskills.io/specification.md):

```yaml
---
name: skill-name           # required, kebab-case, must match directory name
description: Under 1024 chars, specific triggers
license: Optional
metadata:
  author: GoClaw
  version: "1.0"
---
```

## Resource Directories

### scripts/
Executable code for deterministic tasks. Runs via GoClaw's `exec` tool (no sudo needed).
Packages installed at runtime persist to `/app/.goclaw/data/.runtime/`.

```
scripts/
├── main-operation.py
├── helper-utils.py
└── requirements.txt       # document dependencies
```

### references/
Documentation loaded into context as needed. Keep each file under 300 lines.

```
references/
├── api-documentation.md
├── schema-definitions.md
└── workflow-guides.md
```

### assets/
Files used in output, NOT loaded into context.

```
assets/
├── templates/
└── boilerplate/
```

## File Naming

**Format:** kebab-case, descriptive (self-documenting for LLM Grep/Glob)

**Good:**
- `api-endpoints-authentication.md`
- `database-schema-users.md`
- `rotate-pdf-script.py`

**Bad:**
- `docs.md` — not descriptive
- `apiEndpoints.md` — wrong case
- `1.md` — meaningless

## Cleanup

After initialization, delete unused example files:

```bash
rm -f scripts/example.py
rm -f references/api_reference.md
rm -f assets/example_asset.txt
```

## Scope Consolidation

Related topics should be combined into one skill:

**Consolidate:**
- `cloudflare` + `cloudflare-r2` + `cloudflare-workers` → `devops`
- `mongodb` + `postgresql` → `databases`

**Keep separate:**
- Unrelated domains
- Different tech stacks with no overlap

## Validation

Run packaging script to check structure:

```bash
python3 scripts/skill-creator/scripts/package_skill.py ~/.goclaw/skills-store/<name>
```

Checks: SKILL.md exists, valid frontmatter, proper directory structure.
