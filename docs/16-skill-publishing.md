# 16 - Skill Publishing System

How agents create, register, and manage skills programmatically through the `publish_skill` builtin tool, working in tandem with the `skill-creator` core skill.

---

## 1. Overview

The skill publishing system bridges the gap between **skill creation** (filesystem) and **skill management** (database). It consists of two components:

| Component | Type | Purpose |
|-----------|------|---------|
| `skill-creator` | Core skill (bundled) | Guides agents through skill design, implementation, testing, and optimization |
| `publish_skill` | Builtin tool | Registers a skill directory in the database, copies files to managed store, auto-grants to creating agent |

Without `publish_skill`, skills created by agents exist only on the filesystem and are invisible to the database-backed skill management system (no search, no grants, no UI visibility).

---

## 2. End-to-End Flow

```
Agent receives request to create a skill
    │
    ▼
┌─────────────────────────────────────┐
│  1. skill-creator skill activated   │
│     Agent reads SKILL.md guidance   │
│     Creates files via write_file:   │
│       skills/my-skill/SKILL.md      │
│       skills/my-skill/scripts/      │
│       skills/my-skill/references/   │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│  2. publish_skill tool called       │
│     publish_skill(path: "skills/    │
│       my-skill")                    │
└──────────────┬──────────────────────┘
               │
               ▼
┌─────────────────────────────────────────────────────┐
│  3. Tool executes:                                  │
│     a. Validate SKILL.md + parse frontmatter        │
│     b. Derive slug, validate format                 │
│     c. Check system skill conflict                  │
│     d. Compute SHA-256 hash                         │
│     e. Copy dir → skills-store/{slug}/{version}/    │
│     f. INSERT/UPSERT into skills table              │
│     g. Auto-grant to calling agent                  │
│     h. Scan + report missing dependencies           │
│     i. Bump loader cache version                    │
│     j. Generate embedding (async)                   │
└──────────────┬──────────────────────────────────────┘
               │
               ▼
┌─────────────────────────────────────┐
│  4. Result returned to agent:       │
│     - Skill ID, slug, version       │
│     - Grant confirmation            │
│     - Dep warnings (if any)         │
└─────────────────────────────────────┘
```

---

## 3. publish_skill Tool

### Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `path` | string | yes | - | Path to skill directory containing SKILL.md (absolute or relative to workspace) |

### Activation Conditions

The tool is registered at gateway startup when:
1. `pgStores.Skills` is available (PostgreSQL skill store initialized)
2. `PGSkillStore` has at least one managed directory (`skills-store/`)
3. Skills loader is initialized

The tool appears in every agent's tool set — no per-agent configuration needed. Can be toggled via the builtin tools admin UI.

### Context Values Used

| Context Key | Source | Purpose |
|-------------|--------|---------|
| `store.UserIDFromContext(ctx)` | WS connect / HTTP header | Skill owner + grant source |
| `store.AgentIDFromContext(ctx)` | Agent loop | Agent to auto-grant access to |
| `ToolWorkspaceFromCtx(ctx)` | Tool registry | Resolve relative paths |

### SKILL.md Frontmatter Requirements

```yaml
---
name: my-skill-name          # REQUIRED — display name
description: What it does     # Recommended — used for search + auto-activation
slug: my-skill-name           # Optional — derived from name if absent
---
```

- `name` is mandatory; tool returns error if missing
- `slug` auto-derived via `Slugify(name)` if not specified
- Slug must match `^[a-z0-9][a-z0-9-]*[a-z0-9]$`

---

## 4. Core Logic Details

### 4.1 Slug Validation

```
name: "My Awesome Skill"
  → Slugify → "my-awesome-skill"
  → SlugRegexp check → ✓ valid
```

Rejects: leading/trailing hyphens, uppercase, special chars, spaces.

### 4.2 System Skill Conflict Check

Prevents overwriting bundled skills (pdf, xlsx, docx, pptx, skill-creator, etc.):

```go
if t.skills.IsSystemSkill(slug) {
    return ErrorResult("slug conflicts with a system skill")
}
```

### 4.3 Versioned Storage

Skills are stored in versioned directories. Re-publishing the same slug increments the version:

```
skills-store/
├── my-skill/
│   ├── 1/
│   │   ├── SKILL.md
│   │   └── scripts/
│   └── 2/          ← re-publish creates new version
│       ├── SKILL.md
│       └── scripts/
```

`GetNextVersion(slug)` queries `MAX(version)` from the skills table (includes archived skills).

### 4.4 Database Upsert

Uses `CreateSkillManaged()` with `ON CONFLICT(slug) DO UPDATE`:
- New slug → INSERT with `visibility = 'private'`
- Existing slug → UPDATE name, description, version, file_path, file_hash
- Archived skill re-published → status reset to `'active'`
- Embedding generated asynchronously after insert/update

### 4.5 Auto-Grant

When the calling agent has a valid `AgentID` in context:

```go
GrantToAgent(ctx, skillID, agentID, version, userID)
```

This also **auto-promotes** skill visibility from `private` → `internal`, making it accessible via `ListAccessible()` for the granted agent.

### 4.6 Dependency Scanning

After publishing, the tool runs static analysis on the skill's `scripts/` directory:

1. **ScanSkillDeps** — detects required binaries, Python imports, Node packages
2. **CheckSkillDeps** — verifies each dependency is available on the system
3. If missing deps found:
   - Stored in `deps` JSONB column via `StoreMissingDeps()`
   - Warning returned to agent with specific missing packages
   - Agent is guided to install via `exec` (pip/npm) or inform the user

Unlike the HTTP upload handler, the tool does **not** archive the skill on missing deps — it warns and lets the agent decide.

### 4.7 Directory Copy Security

| Check | Action |
|-------|--------|
| `..` in relative path | Skip (prevent traversal) |
| Symlinks | Skip (prevent escape) |
| System artifacts | Skip (`.DS_Store`, `__MACOSX`, `Thumbs.db`, etc.) |
| Total dir size > 20 MB | Reject with error |

---

## 5. skill-creator Skill

### Activation Triggers

The skill-creator is a bundled system skill with a "pushy" description that triggers on:
- Creating new skills or extending agent capabilities
- Skill scripts, references, benchmark optimization
- Description optimization and eval testing

### Creation Workflow

1. **Capture Intent** — what, when, output
2. **Research** — best practices via docs-seeker
3. **Plan** — identify scripts, references, assets
4. **Initialize** — `scripts/init_skill.py <name> --path <dir>`
5. **Write** — implement SKILL.md + resources
6. **Test & Evaluate** — eval suite with parallel runs
7. **Optimize Description** — AI-powered trigger optimization
8. **Publish** — `publish_skill(path: "skills/<name>")`
9. **Package** (optional) — ZIP for external distribution
10. **Iterate** — refine from feedback

### Skill File Structure

```
skills/<skill-name>/
├── SKILL.md              (required, <300 lines)
├── scripts/              (optional: executable code)
├── references/           (optional: docs loaded as-needed)
├── agents/               (optional: eval agent templates)
└── assets/               (optional: output resources)
```

### Key Constraints

| Resource | Limit |
|----------|-------|
| Description | ≤1024 chars |
| SKILL.md | <300 lines |
| Each reference | <300 lines |
| Scripts | No limit (executed, not loaded into context) |

---

## 6. Database Schema

### skills table (relevant columns)

| Column | Type | Purpose |
|--------|------|---------|
| `id` | UUID | Primary key |
| `slug` | VARCHAR(255) UNIQUE | Canonical identifier |
| `name` | VARCHAR(255) | Display name |
| `description` | TEXT | Auto-activation trigger text |
| `owner_id` | VARCHAR(255) | User who created (or "system") |
| `visibility` | VARCHAR(10) | `private` → `internal` (on grant) → `public` |
| `version` | INT | Increments on re-publish |
| `status` | VARCHAR(20) | `active` or `archived` |
| `is_system` | BOOLEAN | True for bundled skills |
| `enabled` | BOOLEAN | Admin toggle |
| `file_path` | TEXT | Filesystem path to versioned dir |
| `file_hash` | VARCHAR(64) | SHA-256 of SKILL.md |
| `deps` | JSONB | `{"missing": ["pip:opencv", "python3"]}` |
| `frontmatter` | JSONB | Parsed YAML metadata |
| `embedding` | vector(1536) | pgvector for similarity search |

### skill_agent_grants table

| Column | Type | Purpose |
|--------|------|---------|
| `skill_id` | UUID FK | References skills |
| `agent_id` | UUID FK | References agents |
| `pinned_version` | INT | Stored but not used — agent always uses latest |
| `granted_by` | VARCHAR | User who granted |

---

## 7. Visibility & Access Model

```
publish_skill creates with visibility = "private"
        │
        ▼
GrantToAgent auto-promotes → "internal"
        │
        ▼
ListAccessible query includes:
  - is_system = true          (all system skills)
  - visibility = 'public'     (anyone)
  - visibility = 'private'    (owner only)
  - visibility = 'internal'   (agents/users with grants)
```

Revoking the last grant auto-demotes `internal` → `private` (atomic SQL).

---

## 8. Cache Invalidation

After publishing, two caches are bumped:

1. **PGSkillStore cache** — `BumpVersion()` sets `version = time.Now().UnixMilli()`, invalidating the `ListSkills()` cache (TTL 5min + version check)
2. **Skills Loader cache** — `loader.BumpVersion()` invalidates the filesystem-based skill index used for system prompt injection

Next agent turn picks up the new skill in its tool set.

---

## 9. Related Files

| File | Purpose |
|------|---------|
| `internal/tools/publish_skill.go` | Tool implementation |
| `internal/skills/helpers.go` | Shared helpers: ParseSkillFrontmatter, Slugify, IsSystemArtifact, SlugRegexp |
| `internal/store/pg/skills.go` | DB operations: CreateSkillManaged, GetNextVersion, IsSystemSkill, StoreMissingDeps |
| `internal/store/pg/skills_grants.go` | GrantToAgent, RevokeFromAgent, ListAccessible |
| `internal/skills/loader.go` | Filesystem skill loader with priority hierarchy |
| `internal/skills/seeder.go` | System skill seeder (bundled → DB) |
| `internal/skills/dep_scanner.go` | Static analysis for skill dependencies |
| `internal/skills/dep_checker.go` | Runtime dependency verification |
| `internal/http/skills_upload.go` | HTTP ZIP upload handler (alternative to publish_skill) |
| `cmd/gateway.go` | Tool registration |
| `cmd/gateway_builtin_tools.go` | Builtin tool seed data |
| `skills/skill-creator/SKILL.md` | Core skill instructions |
