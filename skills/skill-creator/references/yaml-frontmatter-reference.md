# YAML Frontmatter Reference

Per [Agent Skills specification](https://agentskills.io/specification.md).

## Required Fields

```yaml
---
name: skill-name-in-kebab-case
description: What it does and when to use it. Include specific trigger phrases.
---
```

## All Fields

```yaml
---
name: skill-name                                     # required — kebab-case, matches dir name
description: [required — under 1024 chars]           # required
license: MIT                                         # optional — license name or file ref
compatibility: Requires Python 3.10+, network access # optional — 1-500 chars
metadata:                                            # optional — arbitrary key-value pairs
  author: Company Name
  version: "1.0"
  category: productivity
  tags: [project-management, automation]
allowed-tools: "Bash(python3:*) Bash(node:*) Read"  # optional — experimental
---
```

## Field Details

### name (required)
- Must be **1-64 characters**
- Lowercase letters, digits, hyphens only (`a-z`, `0-9`, `-`)
- Must not start or end with a hyphen
- Must not contain consecutive hyphens (`--`)
- **Must match the parent directory name** exactly
- GoClaw uses directory name as the DB slug — `name` is also used as display name fallback

### description (required)
- **Under 1024 characters** (keep under 200 for concision)
- Structure: `[What it does] + [When to use it] + [Key capabilities]`
- Include trigger phrases users/agents would actually say
- Mention relevant file types if applicable

### license (optional)
- Common: `MIT`, `Apache-2.0`, `Proprietary`
- Can reference bundled file: `"Proprietary. LICENSE.txt has complete terms"`

### compatibility (optional)
- 1-500 characters
- Describe environment requirements: intended product, system packages, network access
- Most skills don't need this field

### metadata (optional)
- Arbitrary key-value map for additional properties
- Suggested keys: `author`, `version`, `category`

### allowed-tools (optional, experimental)
- Space-delimited tool patterns pre-approved for this skill
- Support varies between agent implementations

## GoClaw Behavior

GoClaw reads only `name` and `description` from frontmatter (the `Metadata` struct).
All other fields are stored as raw `frontmatter JSONB` in the DB for display purposes.

The **slug** (DB key) is always derived from the **directory name**, not from frontmatter:
```
skills/read-pdf/SKILL.md  →  slug = "read-pdf"
```

Renaming the directory creates a new skill entry. Same directory = upsert existing.

## Description Examples

**Good — specific with triggers:**
```yaml
description: Extracts text and tables from PDF files, fills PDF forms, and merges
  multiple PDFs. Use when working with PDF documents or when user mentions PDFs,
  forms, or document extraction.
```

```yaml
description: Create, edit, and read Word .docx files. Triggers: 'Word doc',
  '.docx', reports, memos, letters, tracked changes, table of contents.
```

**Bad — vague or missing triggers:**
```yaml
description: Helps with projects.                              # Too vague
description: Creates sophisticated documentation systems.      # No triggers
description: Implements the Project entity model.              # Too technical
```
