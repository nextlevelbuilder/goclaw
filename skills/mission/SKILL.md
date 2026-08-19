---
name: mission
description: Plan, create, drive, pause, resume, and complete a durable mission through the GoClaw Mission Mode surface (mission.create/get/list/pause/resume/delete and /gc:mission scheduling)
license: Proprietary. Part of GoClaw bundled skills.
version: 1
inputs:
  - mission_brief
  - agent_id
  - session_key
outputs:
  - mission_record
  - progress_report
allowed-tools:
  - search
  - read_file
  - write_file
quality-gates:
  - mission_created_or_updated
  - progress_recorded
---

# Mission Mode

Drive a durable, named objective through GoClaw's Mission Mode surface. A
mission is a first-class record (`missions` table) with goals, milestones, and
acceptance criteria, tied to an owning agent and a session. Missions live
across runs: a paused mission is resumed from its latest durable checkpoint
instead of starting fresh.

## What a mission is

- `name` — a short, unambiguous objective name.
- `goals` — the outcome the agent is working toward (ordered).
- `milestones` — checkpoints along the way that can be verified.
- `acceptance` — concrete, testable criteria that mark the mission done.
- `agentId` — the agent that owns the mission's work (UUID).
- `sessionKey` — the working session the mission continues on each tick.
- `status` — `active` | `paused` | `completed` | `failed` | `cancelled`.

## RPC surface

All methods are WebSocket RPC under `mission.*`; param names are camelCase.

| Method | Params | Purpose |
|---|---|---|
| `mission.create` | `name`, `goals[]`, `milestones[]`, `acceptance[]`, `agentId`, `sessionKey`, `metadata` | Create a mission record (defaults status `active`). |
| `mission.get` | `missionId` | Read one mission. |
| `mission.list` | `status?`, `limit`, `offset` | List missions, newest first, optional status filter. |
| `mission.pause` | `missionId` | Transition the mission to `paused` — it can be resumed later. |
| `mission.resume` | `missionId` | Re-drive the owning agent from the mission's latest checkpoint and set status back to `active`. |
| `mission.delete` | `missionId` | Remove the record. |

A `missionId` is always a UUID. `mission.pause`/`mission.resume` are scoped to
the calling tenant — a mission owned by another tenant is invisible.

## Scheduling a mission

A mission can be advanced on a cadence by a cron job whose payload `kind` is
`mission` and whose `message` carries the mission UUID. Each tick resumes the
mission's session through the scheduler cron lane, so per-session concurrency
control and `/stop` integration apply exactly as they do for agent-turn cron
jobs.

## Operating Rules

1. **Define acceptance first.** A mission without acceptance criteria cannot be
   verified as complete. Write acceptance criteria that are concrete and
   testable before creating the record.
2. **Ground every claim in reality.** Read the current mission via
   `mission.get` before reporting progress. Never invent checkpoint sequences or
   status.
3. **Pause, don't abandon.** When work is interrupted, prefer `mission.pause`
   over leaving the mission running. A paused mission resumes from its
   checkpoint.
4. **Resume drives the loop.** `mission.resume` re-drives the owning agent from
   the latest durable checkpoint — it does not start a new run. Use it when the
   mission needs to continue where it left off.
5. **Terminal states are final.** `completed`, `failed`, and `cancelled`
   missions cannot be resumed. Create a new mission for a fresh objective.
6. **Scope the change.** Only touch the mission the caller asked about. Never
   list or mutate another tenant's missions.

## Workflow

1. **Clarify the brief** — capture the objective, the owning agent, and the
   session the work will happen in. If acceptance criteria are missing, draft
   them and get confirmation.
2. **Create the mission** — `mission.create` with `name`, `goals`,
   `milestones`, `acceptance`, `agentId`, `sessionKey`. Keep the mission
   focused: one objective per mission.
3. **Drive it** — either `mission.resume` on demand, or a `mission` cron job
   that ticks the mission's session on schedule. Record each advance against
   the mission's milestones.
4. **Verify completion** — run the mission's acceptance criteria against the
   actual output. Only mark the mission complete when every criterion passes.
5. **Report** — summarize the mission: current status, completed milestones,
   remaining work, and the checkpoint sequence if one was linked.

## Quality gates

Confirm both before finishing:

- **mission_created_or_updated** — a mission record was created, or an existing
  mission's status/progress was advanced, with real store round-trips.
- **progress_recorded** — the report reflects the live mission record
  (`mission.get`), including status and flags on the acceptance criteria.

Do not claim the mission is complete unless the acceptance criteria actually
pass.