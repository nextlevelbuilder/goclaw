# Trace Recall Fixes (2026-04-05)

## Scope
This document summarizes the investigation and fixes for incorrect project overview answers in the `nta-telegram-ops` agent, especially when users ask “đang có dự án nào / hôm nay có gì mới”.

## Symptoms (Observed Traces)
- `019d59de-8c55-7b33-bdd4-e06cdf11c4cd`: model answered “chỉ có 2 dự án active” while recall had matching projects (reconciliation + data leak).
- `019d59eb-ff06-7572-957b-a4d3fa646bf0`: model used a generic query, retrieved daily logs, and returned incomplete project list.
- `019d59fa-e35b-7c5c-bfe0-71910b63c319`: **severe** mismatch. `auto_recall:knowledge_graph_projects` returned 38 projects, but answer claimed “chỉ có 1 dự án”.

## Root Cause Analysis
1. **Recall query was too generic**
   - Auto-recall used raw chat text (e.g., “alo em… check lại”) which diluted recall quality.
   - Memory search returned daily logs, not a canonical project inventory.

2. **Project inventory was not enforced**
   - Even when KG returned a full project list, the model could ignore it and anchor on prior answers or daily logs.
   - No hard gate existed to ensure a project overview response references multiple projects or counts.

3. **Tool output structure was inconsistent**
   - `knowledge_graph_search` previously returned prose; `memory_search` returned “No memory results found” strings.
   - The model had to infer structure; there was no machine-readable hint to enforce constraints.

## Fixes Implemented
### 1) Structured tool outputs (memory + KG)
Implemented JSON outputs for recall tools so the model and orchestration can reliably interpret results.

Files:
- `internal/tools/memory.go`
- `internal/tools/knowledge_graph.go`

Changes:
- `memory_search` now returns:
  - `query`, `count`, `has_results`, `matched_paths`, `results[]`
- `knowledge_graph_search` now returns:
  - `mode`, `count`, `has_results`, `matched_entities`, `matched_projects`, `project_names`, `has_project_match`, `summary`
- Added `entity_type` filter for list-all (`query="*"` + `entity_type="project"`)
- Dedup KG search results by `name+type`

### 2) Cleaner recall query + project inventory recall
Auto-recall now:
- Removes common fillers like “alo em”, “check lại”
- Detects “project overview” intent and triggers an explicit inventory query

Files:
- `internal/agent/loop_recall.go`

Changes:
- `cleanRecallQuery` strips filler/metadata
- New `isProjectOverviewIntent(...)`
- If intent matches, run `knowledge_graph_search` with `{query:"*", entity_type:"project"}`

### 3) Project overview enforcement (post-check + retry)
If the user asked for project overview and KG inventory has >1 project, but the response does **not** mention multiple projects or a total count, the loop forces **one corrective retry** with a strict instruction.

Files:
- `internal/agent/loop.go`
- `internal/agent/loop_recall.go`
- `internal/agent/loop_types.go`

Changes:
- Added `recallMeta` (captures project count + names).
- Added `responseMentionsProjectInventory(...)` guard.
- Added `buildProjectOverviewCorrection(...)` system hint.
- Added retry counter `projectOverviewRetries` to avoid loops.

### 4) Tests added/updated
- `internal/agent/loop_recall_test.go` (clean query + intent detection)
- `internal/tools/knowledge_graph_test.go` (project list-all filter)
- `internal/tools/memory_interceptor_test.go` updated for structured output

## Behavior Change Summary
When a user asks for project overview:
- The system *always* performs a dedicated project inventory recall.
- The response must mention multiple projects or explicitly mention the total count.
- If it fails, the loop will retry once with a corrective system prompt.

Expected outcome:
- No more answers like “chỉ có 1 dự án” when inventory has many.
- Responses include a minimum list of project names and a total count.

## Tests Run
From `/Users/nta7/nta-project-mac-mini/nta-goclaw`:
```bash
go test ./internal/agent
go build .
```

## Runtime Rollout
The running service was restarted via `launchctl submit` to ensure Telegram uses the new binary:
```bash
launchctl submit -l com.goclaw.runtime -o /tmp/goclaw-launchctl.out -e /tmp/goclaw-launchctl.err -- \
  /bin/zsh -lc 'cd /Users/nta7/nta-project-mac-mini/nta-goclaw && source .env.local && exec ./goclaw'
```

Health check:
```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18790/health
```

## Known Limitations / Next Steps
1. **Project inventory can be noisy/duplicated**
   - Many projects represent the same initiative under different names.
   - Suggest adding canonicalization/dedup by `external_id` or normalized name.

2. **Inventory size is large**
   - For very large lists, the retry prompt should sample top N canonical projects.
   - Consider returning a grouped summary (e.g., by domain or project category).

3. **Active vs archived status is unclear**
   - KG entities do not encode “active/inactive” reliably.
   - Consider adding a status attribute and filtering for “active” in overview mode.

4. **LLM compliance**
   - The guard is intentionally limited to one retry to avoid infinite loops.
   - If failures persist, consider a stricter post-processor or server-side templating for overview answers.

