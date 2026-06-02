---
name: workspace-organizing
description: Use whenever the agent creates, writes, moves, or renames a file in a team/delegate (shared) workspace, OR when the user asks to organize, clean up, restructure, audit, or find files in any workspace or the Vault, OR when starting a multi-file task or named project. Enforces a purpose-based folder convention (flat mode: notes/, data/, outputs/, scripts/, archive/; project mode: projects/<slug>/{docs,assets,source,reports,research}/), per-agent namespacing under shared/<agent_key>/, and pre-write discovery via memory_search, vault_search, knowledge_graph_search to surface related files and avoid duplicates. Trigger before any write_file or exec at workspace root, when starting a project, generating reports/assets/exports, delegating, or when the user says "messy", "where did I save", "tổ chức lại", "dọn workspace", "tạo report", "find related", "search vault". Do NOT trigger for read-only ops, edits inside an existing project tree (cloned repo), or short-lived files deleted in the same turn.
license: Proprietary. Part of GoClaw bundled skills.
metadata:
  author: GoClaw
  version: "1.2.0"
---

# Workspace Organizing

Keep agent workspaces tidy, predictable, and collision-free — and discoverable via memory, Vault, and the knowledge graph. This is a **discipline** skill: it runs *before* file writes to decide where a file belongs (and whether it already exists), and *after* the fact when asked to clean up or find something.

## Scope

This skill governs file/folder layout and discovery inside:

- `ActivePath` — primary read/write root for the current run
- `SharedPath` — delegate exchange area (when delegating)
- `TeamPath` — team workspace root (when in a team context)
- **Vault** — the cross-workspace knowledge index (personal / team / shared scopes); searched via `vault_search`, read via `vault_read`

Does NOT govern: code edits inside an existing project tree (e.g. a cloned repo), system paths, ephemeral `/tmp` files, or any path outside the resolved workspace.

## Two Modes

Pick **one** mode per workspace root. They do not nest.

### Mode A — Flat (default for ad-hoc work)

Use for scattered, short-lived, or one-off tasks: quick notes, a single report, a CSV cleanup, an isolated script.

```
workspace-root/
├── notes/        ← markdown thinking, drafts, summaries, decisions
├── data/         ← structured input data the agent will re-read
├── outputs/      ← final deliverables for the user
├── scripts/      ← code the agent wrote to execute
├── archive/      ← superseded files (move, don't delete)
└── (optional) tmp/  assets/  logs/
```

### Mode B — Project (for multi-file, named work)

Use when the user names a project, when the task produces ≥3 related files of mixed kinds (docs + assets + code + reports), or when work continues across sessions.

```
workspace-root/
└── projects/
    └── <project-slug>/
        ├── docs/        ← project documentation, specs, design notes, READMEs
        ├── assets/      ← images, media, generated artifacts (PNG, MP4, SVG)
        ├── source/      ← code the project produces (scripts, snippets, configs)
        ├── reports/     ← analyses, audits, status reports for this project
        └── research/    ← raw research material, references, scraped data
```

`<project-slug>` is kebab-case and descriptive: `customer-churn-q2`, `landing-page-redesign`.

### Choosing the mode

| Signal | Mode |
|--------|------|
| User mentions a project/campaign name | B |
| Task produces ≥3 files of mixed kinds | B |
| Work spans multiple sessions / will be revisited | B |
| Ad-hoc single deliverable | A |
| Existing workspace already uses one mode | Match it |

If two projects coexist, both live under `projects/`. Flat-mode folders never live inside a project — `projects/<slug>/notes/` is wrong; use `projects/<slug>/docs/` instead.

## Discovery First: Memory, Vault, Knowledge Graph

**Before writing or organizing, search.** Duplicating a file someone already wrote — or burying a new one where Vault cannot index it — is the failure mode this skill exists to prevent.

### The discovery tools

| Tool | What it finds | When to use |
|------|---------------|-------------|
| `vault_search` | Cross-source discovery across **vault docs + memory + knowledge graph** | Primary entry point. Use first for "is there anything related to X?" |
| `memory_search` | Prior decisions, todos, dates, people, preferences from MEMORY.md + memory/*.md | When the question is "what did I do last time", "what did we decide" |
| `knowledge_graph_search` | People, projects, organizations, and their connections | Multi-hop relationships: "who worked on X with Y", project ↔ owner ↔ file |
| `memory_expand` | Full content of an episodic memory by `episodic_id` | After a search surfaces an episodic hit you need in full |
| `vault_read` | Full vault doc content by `doc_id` | After a search surfaces a vault hit |
| `memory_get` | Targeted line-range read from a memory file by path | Pull only the snippet, keep context small |

### Id routing (do not misroute)

`vault_search` returns three id types in one stream. Each id routes to one and only one follow-up tool:

- `doc_id` → `vault_read(doc_id)` *(vault document)*
- `entity_id` → `knowledge_graph_search(entity_id=...)` *(KG entity, NOT vault_read)*
- `episodic_id` → `memory_expand(id=episodic_id)` *(episodic memory, NOT vault_read)*

Sending an `entity_id` or `episodic_id` to `vault_read` will fail. Read the id name on each result before calling the follow-up tool.

### Language matching

`memory_search` requires the query language to match the stored content language. If memory was written in Vietnamese, search in Vietnamese. Mismatched language drops recall dramatically.

## Pre-Write Discovery Workflow

Before calling `write_file` for any new file in a shared workspace, or any file in project mode:

1. **Search Vault first.** Run `vault_search(query="<short topic>")` — Vault indexes filesystem + memory + KG.
2. **If results exist**, decide: update an existing file, link to it, or genuinely write a new one.
3. **If no results**, also run `memory_search` for personal context (prior decisions on this topic) and `knowledge_graph_search` if the work involves named people/projects.
4. **Only then write.** Pick the folder from the decision table below.
5. **Make the new file discoverable**: write a one-line top heading + descriptive intro paragraph so future `vault_search` recall is good. Reference related vault docs/KG entities by name in the body.

Skip discovery only for: pure `tmp/` files, archive moves, or when the user has already explicitly named the target path.

## Decision: Where Does This File Go?

Before calling `write_file`, ask in order:

1. **Is this an intermediate file I'll re-read and discard?** → `tmp/` (flat) — do not deliver
2. **Is the user going to download/read this as the result?** → `outputs/` (flat) or `projects/<slug>/reports/` (project)
3. **Is this code to be executed?** → `scripts/` (flat) or `projects/<slug>/source/` (project)
4. **Is this generated media (image/video/audio)?** → `assets/` (flat) or `projects/<slug>/assets/` (project)
5. **Is this raw research material or structured data?** → `data/` (flat) or `projects/<slug>/research/` (project)
6. **Is this me thinking/summarizing in prose?** → `notes/` (flat) or `projects/<slug>/docs/` (project)
7. **Am I replacing a previous version?** → move the old one to `archive/` first, then write the new one in its original folder

When unclear, default to `notes/` (flat) or `projects/<slug>/docs/` (project). **Never write to workspace root.**

## Naming Rules

- **kebab-case** with descriptive nouns: `customer-churn-analysis.md` (not `Analysis 1.md`)
- **Long names are fine** if they make the purpose obvious — file names are read by both LLMs and humans
- **ISO date prefix** (`YYYY-MM-DD-`) on time-sensitive files: meeting notes, status updates, dated reports
- **Task slug** when relevant: `pr-123-review.md`, `q2-forecast-chart.png`
- **No spaces, no special characters** other than `-` and `_`. Lowercase always
- **Extensions match content** (`.md` for markdown, `.csv` for CSV)
- **No `untitled`, `output`, `result`, `test`, `temp`, `final`** as standalone names — if you can't think of a better name, the file probably should not be written yet

## Vault Integration

The Vault is a content-addressed, scoped index of agent-readable documents. It synchronises with workspace files and exposes wikilinks + hybrid search. This skill assumes:

- **Files in `outputs/`, `reports/`, `docs/`, `notes/`, and `research/` are Vault-indexable.** Write them well — clear titles, intro paragraphs, named entities — so `vault_search` recall works.
- **Files in `tmp/`, `scripts/`, `data/`, `source/` are usually NOT meant for Vault.** Keep noise out of the index.
- **Vault scope mirrors workspace scope.** A file under personal workspace → personal vault scope. A file under `shared/_common/` → team vault scope. Where the file lives determines who can find it via `vault_search`.
- **Use wikilinks** (`[[other-file-title]]` or `[[doc_id]]`) inside markdown notes/docs/reports to create durable cross-references. Vault auto-resolves and traverses these.
- **Promote to Vault explicitly** when a draft from `notes/` becomes canonical: move to `outputs/` or `reports/`, write a clear title, and the Vault sync layer will index it.

When the user says "save this to my vault" / "lưu vào vault", interpret it as: write to the appropriate folder under the user's personal workspace (`notes/` or `outputs/`), with a strong title and a brief intro paragraph, so Vault indexing surfaces it later.

## Tool Behavior: `deliver` Flag

When the file-write tool exposes a `deliver` boolean (GoClaw filesystem tools):

- `deliver=true` → only for files the user should receive/see. Belongs in `outputs/`, `projects/<slug>/reports/`, or `projects/<slug>/assets/` (when the asset is the deliverable)
- `deliver=false` → everything in `notes/`, `data/`, `scripts/`, `tmp/`, `logs/`, `research/`, `source/`, `docs/`

Never set `deliver=true` on a `tmp/` file or unpromoted draft.

## Workspace-Specific Rules

### Personal workspace (`Scope = personal`)

- Apply the convention when **creating new files**
- Do **not** spontaneously reorganize existing files the user placed there manually
- Only restructure existing layout when the user explicitly asks ("clean up", "organize", "tidy", "tổ chức lại")
- The user is sole owner — preserve their conventions if they have one

### Team workspace (`Scope = team`, shared mode)

- **Every write goes under `shared/<agent_key>/`** unless writing to `shared/_common/`
- Inside `shared/<agent_key>/`, apply either flat or project mode (pick one per sandbox)
- `shared/_common/` is for artifacts the whole team needs. Write here only when the user/teammate puts it in scope; treat as append-mostly
- For team-wide projects, prefer `shared/_common/projects/<slug>/` and namespace files with `<agent_key>-` prefix or sub-folders
- **Never overwrite a file in another teammate's namespace** (`shared/<other-agent-key>/...`). Read is fine; write/move/delete is not
- Before writing in the team root, list the directory and run `vault_search` first

### Delegate workspace (`Scope = delegate`, with `SharedPath`)

- **Inputs from delegator** live in `SharedPath/inputs/` — read-only unless told otherwise
- **Outputs back to delegator** go in `SharedPath/outputs/`
- Working notes the delegatee needs only for itself stay in `ActivePath`
- End each delegated task with `SharedPath/outputs/SUMMARY.md` describing what was produced and where

## Workflow: Before Every File Write in a Shared Workspace

1. Decide flat vs project mode (or match existing mode)
2. **Run `vault_search` for the topic** — confirm no similar file already exists
3. Resolve the target folder using the decision table
4. If in team scope, prefix with `shared/<agent_key>/`
5. List the parent directory — confirm no name collision; if collision, pick a more specific name or archive the old file
6. Write the file with a kebab-case descriptive name and the right `deliver` flag
7. Inside the file, reference related Vault docs by `[[wikilink]]` or by title so traversal works later

## End-of-Task File Summary

After a complex task (≥3 files created, or any project-mode work), end the response with a manifest:

```
Created:
- projects/q2-forecast/reports/forecast-summary.md   ← deliverable
- projects/q2-forecast/assets/forecast-chart.png     ← deliverable
- projects/q2-forecast/source/build-chart.py         ← code used
- projects/q2-forecast/research/raw-sales-2026.csv   ← input data
Related (existing): [[2026-q1-forecast]], KG entity "Sales Team"
```

Mark deliverables vs intermediates. Mention any `tmp/` cleaned up. List related Vault docs / KG entities the new files connect to.

## When User Asks to "Organize" or "Clean Up"

1. **List the workspace root + one level deep.** Identify loose files (anything at root that isn't a canonical folder)
2. **Detect existing mode.** If `projects/` already exists, stay in project mode; otherwise flat
3. **For each loose file, run a quick `vault_search`** to find related Vault docs — group files that belong to the same project together
4. **Categorize each loose file** using the decision table — propose a destination
5. **Show the move plan to the user first**. Do not move silently
6. After approval, move files; do not delete. Discardables go to `archive/<YYYY-MM>/`
7. Clean out `tmp/` and `.tmp/` (disposable by definition, but confirm before deleting)
8. Update or generate a `notes/README.md` (flat) or `projects/<slug>/docs/README.md` (project) summarizing what lives where — include wikilinks to key files

## When User Asks "Where Did I Save X?"

Search Vault and memory **before** scanning the filesystem — they have semantic recall the filesystem does not.

1. **`vault_search(query="X")`** — covers vault docs + memory + KG in one call
2. **`memory_search(query="X")`** in the user's language if Vault returns nothing
3. **`knowledge_graph_search(query="X")`** if the answer hinges on a person/project relationship
4. **Filesystem grep/list** as last resort, scoped to the resolved workspace paths
5. Report matches with full path + last-modified time; route each id correctly (`doc_id` → `vault_read`, `entity_id` → `knowledge_graph_search`, `episodic_id` → `memory_expand`)
6. If a file is in the wrong folder per the convention, offer to move it (don't move without confirmation)

## Archive & Cleanup Policy

- Move to `archive/`, never delete, unless the user says "delete"
- Group long-lived archives by month: `archive/2026-05/old-draft.md`
- `tmp/` and `.tmp/` are exempt — disposable; clean at end of task
- If `archive/` exceeds ~50 items, propose collapsing older months into a zip — only on user request

## Anti-Patterns to Refuse

- Writing `output.txt`, `result.md`, `untitled.md`, `test.py` at the workspace root
- Creating an unrecognized top-level folder (`misc/`, `stuff/`, `wip/`)
- Mixing modes: `projects/<slug>/notes/` (wrong → use `docs/`) or `projects/<slug>/outputs/` (wrong → use `reports/`)
- Versioning by filename (`report-v2.md`, `report-final-FINAL-2.md`) — archive the old one, keep the canonical name
- Writing into another agent's `shared/<other-agent>/` namespace
- Reorganizing a user-curated personal workspace without being asked
- Setting `deliver=true` on `tmp/` files or unfinished drafts
- **Writing a new file without running `vault_search` first** in shared/project contexts — leads to silent duplication
- **Misrouting ids**: passing `entity_id` to `vault_read`, or `episodic_id` to `vault_read`, instead of their proper tools
- Searching memory in the wrong language (English query against Vietnamese memory)

## Quick Reference

```
# Flat mode (ad-hoc)
workspace-root/
├── notes/        ← Vault-indexed thinking
├── data/         ← inputs
├── outputs/      ← Vault-indexed deliverables (deliver=true)
├── scripts/      ← executable code
├── archive/      ← superseded
└── tmp/  assets/  logs/   ← optional

# Project mode (named, multi-file)
workspace-root/
└── projects/<project-slug>/
    ├── docs/      ← Vault-indexed project docs
    ├── assets/    ← images, media
    ├── source/    ← project code
    ├── reports/   ← Vault-indexed analyses (deliver=true)
    └── research/  ← raw research, references

# Shared (team) — wrap either mode under shared/<agent_key>/
team-root/
└── shared/
    ├── _common/                ← team-wide reads
    │   └── projects/<slug>/    ← team-wide project
    ├── <agent-key-1>/          ← agent 1's sandbox (flat or project)
    └── <agent-key-2>/          ← agent 2's sandbox

# Discovery before writing
vault_search → doc_id   → vault_read
             → entity_id → knowledge_graph_search
             → episodic_id → memory_expand
memory_search (match language)
knowledge_graph_search (entities + relationships)
```

Default to clarity over cleverness. A future agent (or human) opening this workspace — or searching the Vault for it — should find what they need within 10 seconds.
