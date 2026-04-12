# OpenHands Specialist Integration — Checkpoints

Date: 2026-04-12

This checkpoint plan assumes:

- GoClaw remains the user-facing orchestrator
- OpenHands runs on a dedicated Linux VM
- the public contract is a Blackbird adapter under
  `https://openhands.blackbirdzzzz.art`
- MVP uses `git_ref` workspace mode only

## Checkpoint Overview

```text
CP-OH-00  Architecture Lock + Threat Model
  |
  v
CP-OH-01  Linux VM Runtime + Domain + TLS
  |
  v
CP-OH-02  Blackbird Adapter API
  |
  v
CP-OH-03  OpenHands Worker Wiring
  |
  v
CP-OH-04  GoClaw Native Tool Integration
  |
  v
CP-OH-05  Specialist Agent + UX Flow
  |
  v
CP-OH-06  Safety, Quotas, and Observability
  |
  v
CP-OH-07  Production Pilot + Rollout Gate
```

## Current status

All checkpoints below are implemented for the MVP production slice and verified on
2026-04-12.

| Checkpoint | Status | Evidence |
| --- | --- | --- |
| `CP-OH-00` | Done | architecture and threat model locked in [`README.md`](/Users/nguyenquocthong/project/goclaw/docs/openhands-specialist/README.md) |
| `CP-OH-01` | Done | `https://openhands.blackbirdzzzz.art/health` healthy on production |
| `CP-OH-02` | Done | adapter endpoints `/v1/jobs`, `/events`, `/artifacts` implemented |
| `CP-OH-03` | Done | direct smoke job `d5006e8b-9be1-4c3b-9506-8ba2085ce22d` succeeded |
| `CP-OH-04` | Done | native tool `openhands_delegate` enabled in production `coder` |
| `CP-OH-05` | Done | predefined agent `openhands-coder` created on production |
| `CP-OH-06` | Done | auth, repo allowlist, upload caps, job persistence, metrics endpoint, structured events live |
| `CP-OH-07` | Done | GoClaw end-to-end smoke job `8f0da4d7-7c92-420c-978c-443f1a919f64` succeeded |

## CP-OH-00 — Architecture Lock + Threat Model

Objective:

- lock the integration shape before implementation
- decide public contract, auth model, workspace mode, and rollout scope

Deliverables:

- final architecture decision record
- threat model
- approved domain plan
- approved MVP scope

Must decide:

- adapter vs direct-call:
  choose adapter
- public endpoint:
  `openhands.blackbirdzzzz.art`
- raw OpenHands exposure:
  private only
- workspace mode:
  `git_ref`
- output mode:
  artifact-return, no direct deploy

Verification:

- architecture doc signed off
- threat list exists with mitigations
- no unresolved blocker on host trust boundary

Estimated effort:

- 0.5 day

## CP-OH-01 — Linux VM Runtime + Domain + TLS

Objective:

- prepare the dedicated Linux VM for OpenHands workloads
- expose the Blackbird adapter endpoint securely

Deliverables:

- Docker installed and hardened
- compose stack skeleton
- reverse proxy or tunnel routing
- TLS on `openhands.blackbirdzzzz.art`
- private Docker network between adapter and OpenHands

Implementation slices:

- provision VM user, disk layout, firewall
- attach persistent workspace volume
- configure reverse proxy / Cloudflare route
- expose only adapter publicly
- keep raw OpenHands bound to private network

Verification:

- `https://openhands.blackbirdzzzz.art/health` returns healthy
- raw OpenHands port is not reachable from public internet
- VM survives service restart and reboot

Estimated effort:

- 0.5 to 1 day

## CP-OH-02 — Blackbird Adapter API

Objective:

- create the stable Blackbird-owned API contract in front of OpenHands

Deliverables:

- adapter service
- `/v1/jobs` create endpoint
- `/v1/jobs/{id}` status endpoint
- `/v1/jobs/{id}/events` stream endpoint
- `/v1/jobs/{id}/cancel`
- artifact download endpoint
- auth middleware

Implementation slices:

- define request/response schema
- add idempotency with `request_id`
- persist job state
- normalize error categories
- implement bearer token validation

Verification:

- can create a dummy job and observe status transitions
- repeated `request_id` is idempotent
- unauthorized requests are rejected

Estimated effort:

- 1 to 1.5 days

## CP-OH-03 — OpenHands Worker Wiring

Objective:

- connect adapter to OpenHands agent server and make a remote coding run work

Deliverables:

- OpenHands agent server container
- adapter client for create/send/stream/cancel
- workspace creation and cleanup flow
- artifact harvesting flow

Implementation slices:

- create or resolve workspace for job
- create conversation
- send normalized task prompt
- stream events into adapter event bus
- collect diff, summary, test report, final result

Verification:

- test repo can be cloned and modified remotely
- sample task produces `summary.md` and `patch.diff`
- cancelled jobs terminate cleanly

Estimated effort:

- 1 to 1.5 days

## CP-OH-04 — GoClaw Native Tool Integration

Objective:

- let GoClaw invoke the remote coding specialist through a first-class native tool

Deliverables:

- Go tool `openhands_delegate`
- config for external worker endpoint and token
- tool parameters and response schema
- tool result rendering in sessions/traces

Likely GoClaw file surfaces:

- `internal/tools/openhands_delegate.go`
- `cmd/gateway_setup.go`
- `internal/config/...`
- `docs/03-tools-system.md`

Implementation slices:

- POST job from GoClaw tool
- optional stream/poll until completion
- attach artifact URLs and summary into tool result
- add clear error handling for timeout/auth/upstream failure

Verification:

- GoClaw can submit one remote job from a chat/tool flow
- job status and final artifacts appear back in GoClaw
- failures are human-readable

Estimated effort:

- 1 day

## CP-OH-05 — Specialist Agent + UX Flow

Objective:

- make the remote worker usable as a proper GoClaw specialist, not just a raw tool

Deliverables:

- predefined GoClaw agent `openhands-coder`
- tool policy for that agent
- delegation pattern from existing `coder`
- user-facing result formatting contract

Implementation slices:

- create specialist agent definition
- add agent link from `coder` to `openhands-coder`
- define prompt contract:
  summary, files changed, tests, risks, next actions
- decide sync vs async UX for long jobs

Verification:

- `coder` can delegate a coding task to `openhands-coder`
- user sees progress and final result without raw low-level noise
- final response references artifacts and changed files cleanly

Estimated effort:

- 0.5 to 1 day

## CP-OH-06 — Safety, Quotas, and Observability

Objective:

- harden the integration for production safety and operability

Deliverables:

- profile-based quotas
- approval flow for risky actions
- deterministic deny rails
- Prometheus metrics
- structured logs with correlation IDs
- stuck-job detection and timeout cleanup

Implementation slices:

- map caller to quota bucket
- add job timeout and orphan cleanup
- enforce repo allowlist
- enforce no-protected-branch-push in MVP
- expose metrics and dashboards

Verification:

- risky action enters approval state instead of executing silently
- over-quota requests are rejected
- stuck run is auto-failed and cleaned up
- metrics show active jobs and failure classes

Estimated effort:

- 1 day

## CP-OH-07 — Production Pilot + Rollout Gate

Objective:

- run a controlled production pilot and decide go/no-go

Deliverables:

- pilot report on real repos/tasks
- success/failure metrics
- list of bugs and sharp edges
- go/no-go checklist for wider rollout

Pilot scope:

- 1 to 2 repos only
- 1 profile only:
  `implement_deep`
- no auto-push
- no auto-deploy

Success criteria:

- remote coding runs complete reliably
- artifact contract is stable
- no cross-repo or auth leak
- operator can debug failures quickly

Verification:

- run at least 5 real jobs end to end
- classify failures by source
- decide next step:
  widen rollout, add bundle mode, or pause and fix

Estimated effort:

- 0.5 to 1 day for pilot, then iterative hardening

## Recommended Implementation Order

Phase A:

- CP-OH-00
- CP-OH-01
- CP-OH-02
- CP-OH-03

Phase B:

- CP-OH-04
- CP-OH-05

Phase C:

- CP-OH-06
- CP-OH-07

## Non-Goals for MVP

Do not include these in the first production cut:

- bundle upload for dirty local worktrees
- direct push to protected branches
- direct production deploy from OpenHands
- exposing raw OpenHands agent server publicly
- multi-engine abstraction beyond the adapter contract
- broad multi-tenant rollout before pilot proof

## One-Line Recommendation

If you want this project to stay operable, build it as:

`GoClaw -> Blackbird adapter -> OpenHands agent server -> isolated Docker workspace`

and ship only the `git_ref + artifact-return` slice first.
