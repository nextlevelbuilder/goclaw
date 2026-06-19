# Validation Report: Agent-scoped Git Credentials Plan

Date: 2026-05-31

Scope: validate plan completeness and consistency against current code.

## Checks

### V1 - Current failure mode is represented

Evidence: current resolver can return channel-specific user IDs, and `LookupByBinary` only joins user credentials when `userID` is non-empty.

Plan coverage: Phase 1 and Phase 5 include cross-channel/no-user tests.

Status: pass.

### V2 - API endpoints are included

Evidence: current routes include user credential endpoints only.

Plan coverage: Phase 3 defines list/get/put/delete agent credential endpoints and docs updates.

Status: pass.

### V3 - Store model separates policy from secret identity

Evidence: `SecureCLIAgentGrant` has policy fields plus encrypted env override but no typed credential metadata.

Plan coverage: Phase 2 adds dedicated `secure_cli_agent_credentials`.

Status: pass.

### V4 - Dual database migration is covered

Evidence: repo has separate PostgreSQL migrations and SQLite schema/version migrations.

Plan coverage: Phase 2 explicitly updates both systems.

Status: pass.

### V5 - UI default path matches product decision

Evidence: current table opens User Credentials, and current git guide documents User Credentials.

Plan coverage: Phase 4 and Phase 6 make Agent Credentials the default and keep User Credentials as advanced override.

Status: pass.

### V6 - TDD gates are explicit

Evidence: implementation phases depend on failing tests from Phase 1.

Plan coverage: every phase lists tests or validation commands.

Status: pass.

### V7 - Security boundary is explicit

Evidence: agent access is the proposed operational permission boundary.

Plan coverage: Phase 3, Phase 4, and Phase 6 all require warnings/docs/tests that credential does not grant binary access but agent users can trigger credential use.

Status: pass.

## Fixes Applied During Validation

- Added explicit non-global grant boundary tests.
- Added exact HTTP endpoint list and response shapes.
- Added SQLite migration requirement.
- Added runtime audit source requirement.
- Added PAT transport characterization requirement.

## Unresolved Questions

None.
