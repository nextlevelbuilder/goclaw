## Mô tả

Implement P0 Durable Agent Run (§7 của `GoClaw_Upgrade_Improvement_Plan.md`) — biến mỗi agent run thành một **durable state machine** (1 dòng/run) thay vì phụ thuộc vào một request/stream duy nhất. Giải quyết lỗi "Something went wrong" giả khi backend vẫn đang chạy.

## Thay đổi chính

**Store + Migrations (dual-DB)**
- `agent_runs` table: migration `000097_agent_runs` (PostgreSQL) + schema/patch SQLite
- `RunsStore` interface: CreateRun / UpdateRunStatus / UpdateRunTerminal / TouchHeartbeat / GetRun / ListRuns / RecoverStaleRuns
- Config `reliability.runs.*`: heartbeat / stale-after / sweep interval / extension budget

**Agent loop** — single choke point tại `Loop.Run`, bao phủ mọi nguồn run (chat WS, cron, channels, heartbeat ticker, delegation, HTTP):
- RunStarted / RunCancelled / RunFailed / RunCompleted
- Non-blocking heartbeat để run đang sống không bị stale sweep
- Panic safety-net chốt record; ghi lỗi log-only (không chặn agent execution)

**Surface** — parity đầy đủ:
- WS RPC: `runs.get` / `runs.list` / `runs.events` + viewer-scoped RBAC + status-enum validation
- HTTP: `/v1/runs` endpoints + resync cursor
- CLI: `goclaw run list|get|events`
- i18n keys ở cả 5 catalog (en/vi/zh/ko/ru)
- Docs: `04-gateway-protocol.md`, `project-changelog.md`, journal

## Scope (theo quyết định user)
P0 chỉ **record attempt** trên run record — **không** auto-retry / requeue / resume trong phase này. `checkpoint` column đã reserve cho phase sau.

## Verify
- `go build ./...` + `go build -tags sqliteonly ./...` + `go vet ./...` — pass (Docker golang:1.26.0-alpine)
- Tests affected (agent, config, store, store/pg, store/base, gateway/methods) — pass
- Code-review fix: thêm `store.ValidAgentRunStatus` cho status-enum validation, rebuild xanh
- Không có `.env*`/secret trong commit

## Surface parity
- Gateway server: ✅ (loops, stores, RPC)
- API contract: ✅ (`pkg/protocol`, HTTP, docs)
- Web UI: N/A — read-only inspection, chưa có UI screen dùng runs.* (future)
- CLI/runtime: ✅ (`goclaw run` commands)
