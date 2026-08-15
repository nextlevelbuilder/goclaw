// Package testutil provides reusable helpers for Go tests across the codebase.
//
// The package is split into default-build files (context builders, mock hooks)
// and integration-tagged files (DB connection helper) so default `go test ./...`
// never requires Postgres. Import paths stay consistent regardless of build tag.
//
// Helpers:
//   - TestDB (integration tag): shared Postgres connection + migrations, once per binary.
//   - TenantCtx / UserCtx / AgentCtx / FullCtx: context builders mirroring store.With* setters.
//   - Mock stores (generated via go:generate, see generate.go): gomock doubles
//     for unit tests that need a store interface without hitting Postgres.
package testutil
