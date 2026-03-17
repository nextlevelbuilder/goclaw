# 15 - Core Skills System

How bundled (system) skills are loaded, stored, injected into agents, and managed throughout their lifecycle — including dependency checking, toggle control, and hot-reload.

---

## 1. Overview

GoClaw ships with a set of **core skills** — SKILL.md-based modules bundled inside the binary's embedded filesystem. Unlike custom skills uploaded by users, core skills are:

- Seeded automatically on every gateway startup
- Tracked by content hash (no re-import if file unchanged)
- Tagged `is_system = true` in the database
- Always `visibility = 'public'` (accessible by all agents)
- Subject to dependency checking (archived if required deps are missing)

Current bundled core skills:

| Slug | Purpose |
|------|---------|
| `pdf` | Read, create, merge, split PDF files via pypdf |
| `docx` | Read, create, edit Word documents via python-docx |
| `pptx` | Read, create, edit PowerPoint presentations via python-pptx |
| `xlsx` | Read, create, edit Excel spreadsheets via openpyxl |
| `skill-creator` | Meta-skill for creating new skills |

Shared helper modules live in `skills/_shared/` and are copied alongside each skill but not registered as standalone skills.

---

## 2. Startup Flow

```
cmd/gateway.go  NewSkillLoader()
       │
       ▼
internal/skills/loader.go  NewLoader(baseDir, db)
       │  ── scans filesystem skill dirs
       │  ── wires managed DB directory
       │  ── calls BumpVersion() → invalidates list cache
       │
       ▼
internal/skills/seeder.go  Seed(ctx, db, embedFS, baseDir)
       │
       ├─ For each bundled skill in embed.FS (skills/*/SKILL.md):
       │     1. Read SKILL.md → parse YAML frontmatter (name, slug, description, author, ...)
       │     2. Compute SHA-256 of content → FileHash
       │     3. Call GetNextVersion(slug) → next DB version number
       │     4. UpsertSystemSkill(ctx, params) ──► see §4
       │     5. Copy skill files to baseDir/<slug>/<version>/
       │
       ├─ CheckDepsAsync(ctx, seededSlugs, baseDir, skillStore, broadcaster)
       │     └─ goroutine (non-blocking):
       │           for each slug:
       │             broadcast EventSkillDepsChecking {slug}
       │             ScanSkillDeps(skillDir) → manifest
       │             CheckSkillDeps(manifest) → (ok, missing[])
       │             StoreMissingDeps(id, missing) → UPDATE skills SET deps=...
       │             if !ok: UpdateSkill(id, {status: "archived"})
       │             else:   UpdateSkill(id, {status: "active"})
       │             broadcast EventSkillDepsChecked {slug, ok, missing}
       │
       └─ Register file watcher (500ms debounce) → on SKILL.md change: re-seed + BumpVersion
```

**Key invariant:** Startup is non-blocking. Dep checks run in a background goroutine and notify clients via WebSocket events. The agent loop is unaffected during the check window.

---

## 3. Skill Directory Layout

```
skills/
├── _shared/               # Shared Python helpers (not standalone skills)
│   ├── office_helpers.py
│   └── ...
├── pdf/
│   ├── SKILL.md           # Frontmatter + instructions
│   └── scripts/
│       └── read_pdf.py
├── docx/
│   ├── SKILL.md
│   └── scripts/
│       └── read_docx.py
├── pptx/
│   └── ...
├── xlsx/
│   └── ...
└── skill-creator/
    └── SKILL.md
```

Each version is copied to: `<baseDir>/<slug>/<version>/`
Example: `/app/data/skills/pdf/3/`

---

## 4. SKILL.md Frontmatter Format

```yaml
---
name: pdf
description: Use this skill whenever the user wants to do anything with PDF files...
author: GoClaw Team
tags: [pdf, document]
---

## Instructions

(Skill body used as system prompt injection)
```

Supported frontmatter fields:

| Field | Required | Notes |
|-------|----------|-------|
| `name` | Yes | Display name |
| `slug` | Yes | Unique identifier, kebab-case |
| `description` | Yes | Short summary for agent search |
| `author` | No | Shown in UI custom skills tab |
| `tags` | No | Array, used for filtering |

---

## 5. Hash-Based Change Detection (UpsertSystemSkill)

`UpsertSystemSkill` (`internal/store/pg/skills.go:410`) prevents unnecessary DB version bumps:

```
SELECT id, file_hash, file_path FROM skills WHERE slug = $1

Case 1: No row found
  → INSERT new skill (version = GetNextVersion())
  → BumpVersion() (cache invalidation)

Case 2: Row found, existingHash == incomingHash
  → Return unchanged (no DB write)

Case 3: Row found, existingHash IS NULL (old record, no hash stored)
  → UPDATE skills SET file_hash = $1 WHERE id = $2  (backfill only)
  → Return unchanged (no version bump)

Case 4: Row found, hash changed
  → Full UPDATE (name, description, version, file_path, file_hash, status, ...)
  → BumpVersion()
```

**Why Case 3 matters:** Before hash tracking was added, existing rows had `file_hash = NULL`. Without this guard, every startup would fail the hash equality check and run a full UPDATE — incrementing the DB `version` column even though the skill content hadn't changed.

---

## 6. Database Schema

```sql
-- Core columns added for system skills (migration 017)
ALTER TABLE skills ADD COLUMN is_system BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE skills ADD COLUMN deps     JSONB    NOT NULL DEFAULT '{}';
ALTER TABLE skills ADD COLUMN enabled  BOOLEAN  NOT NULL DEFAULT true;

-- Indexes
CREATE INDEX idx_skills_system  ON skills(is_system) WHERE is_system = true;
CREATE INDEX idx_skills_enabled ON skills(enabled)   WHERE enabled = false;
```

`deps` JSONB structure: `{"missing": ["pip:openpyxl", "npm:marked"]}`

Full `skills` table columns relevant to core skills:

| Column | Type | Purpose |
|--------|------|---------|
| `id` | UUID | PK |
| `slug` | TEXT | Unique skill identifier |
| `name` | TEXT | Display name |
| `description` | TEXT | Agent-facing summary |
| `version` | INT | Increments on content change |
| `is_system` | BOOL | True for bundled skills |
| `status` | TEXT | `active` / `archived` |
| `enabled` | BOOL | User toggle (independent of status) |
| `file_path` | TEXT | Path to versioned copy on disk |
| `file_hash` | TEXT | SHA-256 of SKILL.md content |
| `frontmatter` | JSONB | Parsed YAML key-value pairs |
| `deps` | JSONB | `{"missing": [...]}` from dep scan |
| `embedding` | vector | pgvector embedding for semantic search |

---

## 7. Dependency System

### 7a. Scanner (`internal/skills/dep_scanner.go`)

Statically analyzes `scripts/` subdirectory for Python and Node.js imports:

**Python detection:**
- Regex matches: `import X`, `from X import ...`
- Sets `PYTHONPATH=scriptsDir` when running the subprocess check — this makes local helpers (e.g. `office_helpers`) resolve successfully without false positives

**Node.js detection:**
- Matches `require('X')` and `import ... from 'X'`
- Skips relative imports (`./`, `../`)
- Skips Node.js built-ins (`fs`, `path`, `os`, ...)

**Shebang detection:**
- `#!/usr/bin/env python3` or `#!/usr/bin/env node` sets runtime requirement

Result: `SkillManifest{RequiresPython [], RequiresNode [], ScriptsDir}`

### 7b. Checker (`internal/skills/dep_checker.go`)

Verifies each import actually resolves at runtime via subprocess:

**Python check:**
```python
# One-liner per import, run with PYTHONPATH=scriptsDir
python3 -c "import openpyxl"   # success = installed
python3 -c "import missing_pkg" # exit 1 = missing
```
- `importToPip` map translates import names to pip package names (e.g. `PIL` → `Pillow`)
- Missing → `"pip:openpyxl"`

**Node.js check:**
```js
// cmd.Dir = scriptsDir
node -e "require.resolve('marked')"  // success = installed
```
- Missing → `"npm:marked"`

Returns: `(allOk bool, missing []string)`

### 7c. Installer (`internal/skills/dep_installer.go`)

Installs individual deps by prefix:

| Prefix | Command |
|--------|---------|
| `pip:name` | `pip3 install --target $PIP_TARGET name` |
| `npm:name` | `npm install -g name` |
| `apk:name` | `doas apk add --no-cache name` |
| (no prefix) | treated as `apk:` |

After install: re-runs rescan to update `deps` column and skill `status`.

### 7d. Runtime Checker (`internal/skills/runtime_check.go`)

Called before dep checking to detect available runtimes:

```go
type RuntimeInfo struct {
    PythonAvailable bool
    PipAvailable    bool
    NodeAvailable   bool
    NpmAvailable    bool
    DoasAvailable   bool
}
```

Probes: `python3 --version`, `pip3 --version`, `node --version`, `npm --version`, `doas --version`

Result is exposed via `GET /v1/skills/runtimes` and displayed in the UI `MissingDepsPanel` when core runtimes are absent.

---

## 8. Agent Injection

File: `internal/agent/loop_history.go` — `resolveSkillsSummary()`

### Thresholds

```go
const (
    skillInlineMaxCount  = 40   // max skills to inline
    skillInlineMaxTokens = 5000 // max estimated token budget
)
```

### Decision Logic

```
skillFilter = agent.AllowedSkills  (nil = all enabled skills)

FilterSkills(skillFilter)
  └── excludes disabled skills (enabled = false)
  └── if allowList != nil: also filters by slug

Count skills → if > 40 OR estimated tokens > 5000:
  → return "" (agent uses skill_search tool instead)

Count ≤ 40 AND tokens ≤ 5000:
  → build XML block injected into system prompt:

<available_skills>
  <skill name="pdf" slug="pdf">Read, create, merge, split PDF files</skill>
  <skill name="docx" slug="docx">Read, create, edit Word documents</skill>
  <skill name="pptx" slug="pptx">Read, create, edit PowerPoint presentations</skill>
  <skill name="xlsx" slug="xlsx">Read, create, edit Excel spreadsheets</skill>
  <skill name="skill-creator" slug="skill-creator">Create new skills</skill>
</available_skills>
```

**Token estimation:** `(len(Name) + len(Description) + 10) / 4` per skill ≈ 100–150 tokens each.

### Search Fallback (BM25)

When skills exceed thresholds, the `skill_search` tool is injected instead. The agent calls it with a query; results are ranked by BM25 score (`internal/skills/search.go`).

---

## 9. Toggle System (enabled column)

The `enabled` column decouples **user intent** from **dep availability** (`status`):

| enabled | status | Effect |
|---------|--------|--------|
| true | active | Fully functional, injected into prompts |
| true | archived | Has missing deps; injected but warns agent |
| false | active | Hidden — not injected, not searchable |
| false | archived | Hidden — not injected, dep check skipped |

**Toggle ON flow** (`POST /v1/skills/{id}/toggle` with `{enabled: true}`):
1. `ToggleSkill(id, true)` → `UPDATE skills SET enabled = true`
2. Re-run `ScanSkillDeps` + `CheckSkillDeps` for this skill
3. `StoreMissingDeps` + `UpdateSkill({status: "active"|"archived"})`
4. `BumpVersion()` → invalidates list cache
5. Returns `{ok, enabled, status}`

**Toggle OFF flow** (`{enabled: false}`):
1. `ToggleSkill(id, false)` → `UPDATE skills SET enabled = false`
2. `BumpVersion()` → list cache invalidated
3. Skill disappears from all agent prompts on next request

**Store-layer enforcement:**

| Method | Behavior with disabled skills |
|--------|-------------------------------|
| `ListSkills()` | Returns disabled skills (admin UI needs them) |
| `FilterSkills()` | **Excludes** disabled (agent injection gate) |
| `ListAllSkills()` | Excludes disabled (dep rescan skips them) |
| `ListSystemSkillDirs()` | Excludes disabled (startup dep scan skips them) |
| `SearchByEmbedding()` | Excludes disabled |
| `BackfillEmbeddings()` | Excludes disabled |

---

## 10. Cache Invalidation (BumpVersion)

`BumpVersion()` updates an atomic `int64` (Unix nanosecond timestamp) in memory. It does **not** touch the DB `version` column.

`ListSkills()` caches results using this version + a TTL safety net. On BumpVersion, next call to `ListSkills()` re-queries the DB.

Triggers:
- New skill inserted
- Skill content hash changed → full UPDATE
- Skill enabled/disabled toggle
- Missing deps stored

---

## 11. WebSocket Events

Broadcast to all connected clients during dep operations:

| Event | Payload | Trigger |
|-------|---------|---------|
| `skill.deps.checking` | `{slug}` | About to check deps for a skill |
| `skill.deps.checked` | `{slug, ok, missing[]}` | Dep check complete |
| `skill.deps.installing` | `{deps[]}` | Bulk install started |
| `skill.deps.installed` | `{system[], pip[], npm[], errors[]}` | Bulk install complete |
| `skill.dep.item.installing` | `{dep}` | Single dep install started |
| `skill.dep.item.installed` | `{dep, ok, error?}` | Single dep install complete |

The frontend listens to these events via `use-query-invalidation.ts` to automatically refresh the skills list.

---

## 12. HTTP API Endpoints

All endpoints under `/v1/skills/` require authentication (`authMiddleware`).

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/skills` | List all skills (admin) |
| `POST` | `/v1/skills/upload` | Upload custom skill ZIP |
| `POST` | `/v1/skills/rescan-deps` | Re-scan all enabled skills for missing deps |
| `POST` | `/v1/skills/install-deps` | Install all missing deps (bulk) |
| `POST` | `/v1/skills/install-dep` | Install one dep, broadcast events |
| `GET` | `/v1/skills/runtimes` | Check python3/node/pip/npm availability |
| `GET` | `/v1/skills/{id}` | Get single skill |
| `PUT` | `/v1/skills/{id}` | Update skill metadata (name, description, visibility, tags) |
| `DELETE` | `/v1/skills/{id}` | Delete custom skill |
| `POST` | `/v1/skills/{id}/toggle` | Enable/disable skill |
| `GET` | `/v1/skills/{id}/versions` | List available versions |
| `GET` | `/v1/skills/{id}/files` | List files in a version |
| `GET` | `/v1/skills/{id}/files/{path}` | Get file content |

**Note:** `PUT /v1/skills/{id}` explicitly ignores the `enabled` field — toggle must go through the dedicated endpoint to trigger dep re-check.

---

## 13. WebSocket RPC Methods

| Method | Description |
|--------|-------------|
| `skills.list` | Returns all skills with enabled/status/missing_deps |
| `skills.get` | Returns full skill detail including SKILL.md content |
| `skills.update` | Update skill metadata (visibility, tags, description) |

---

## 14. File Watcher (Hot Reload)

`internal/skills/watcher.go` uses `fsnotify` to watch the managed skills directory:

- **Debounce:** 500ms — rapid saves don't trigger multiple re-seeds
- **On change:** calls `Seed()` → `CheckDepsAsync()` → `BumpVersion()`
- **Scope:** watches `<baseDir>/` recursively for `SKILL.md` modifications

This allows editing core skill instructions in production without restarting the gateway.

---

## 15. Data Flow Summary

```
Embed FS (skills/)
      │
      ▼  startup
  Seeder.Seed()
      │  UpsertSystemSkill (hash check)
      │  Copy files to baseDir/<slug>/<version>/
      ▼
PostgreSQL skills table
  is_system=true, status=active|archived, enabled=true|false
      │
      ├──► ListSkills() [cached, version-gated]
      │         │
      │         └──► FilterSkills(allowList) ──► agent system prompt
      │                  (excludes disabled)       (inline XML or search)
      │
      ├──► SearchByEmbedding() ──► skill_search tool results
      │
      └──► HTTP/WS API ──► UI (skills-page.tsx)
                               toggle, rescan, install deps
```
