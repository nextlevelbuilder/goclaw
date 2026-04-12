package testutil

// gomock-based store mocks generation hooks.
//
// Setup: go install go.uber.org/mock/mockgen@latest
// Run:   go generate ./internal/testutil/...
//
// Generated files are checked into the repo so tests work without extra setup.
// Add new interfaces below when a new package needs a mock for unit tests.

//go:generate mockgen -destination=mock_session_store.go -package=testutil github.com/nextlevelbuilder/goclaw/internal/store SessionStore
//go:generate mockgen -destination=mock_agent_store.go -package=testutil github.com/nextlevelbuilder/goclaw/internal/store AgentStore
//go:generate mockgen -destination=mock_contact_store.go -package=testutil github.com/nextlevelbuilder/goclaw/internal/store ContactStore
