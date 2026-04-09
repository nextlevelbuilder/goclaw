package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/cache"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ---- minimal AgentStore stub for interceptor tests ----

type stubAgentStore struct {
	agentFiles    []store.AgentContextFileData
	userFiles     []store.UserContextFileData
	agentCallsN   atomic.Int32 // counts GetAgentContextFiles calls
	setAgentCallN atomic.Int32
	setUserCallN  atomic.Int32
}

func (s *stubAgentStore) GetAgentContextFiles(_ context.Context, _ uuid.UUID) ([]store.AgentContextFileData, error) {
	s.agentCallsN.Add(1)
	return s.agentFiles, nil
}
func (s *stubAgentStore) SetAgentContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	s.setAgentCallN.Add(1)
	return nil
}
func (s *stubAgentStore) GetUserContextFiles(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return s.userFiles, nil
}
func (s *stubAgentStore) SetUserContextFile(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	s.setUserCallN.Add(1)
	return nil
}
func (s *stubAgentStore) DeleteUserContextFile(_ context.Context, _ uuid.UUID, _, _ string) error {
	return nil
}
func (s *stubAgentStore) ListUserContextFilesByName(_ context.Context, _ uuid.UUID, _ string) ([]store.UserContextFileData, error) {
	return nil, nil
}
func (s *stubAgentStore) MigrateUserDataOnMerge(_ context.Context, _ []string, _ string) error {
	return nil
}

// Remaining interface methods — not exercised in these tests.
func (s *stubAgentStore) Create(_ context.Context, _ *store.AgentData) error { return nil }
func (s *stubAgentStore) GetByKey(_ context.Context, _ string) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetByID(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetByIDUnscoped(_ context.Context, _ uuid.UUID) (*store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetByKeys(_ context.Context, _ []string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetByIDs(_ context.Context, _ []uuid.UUID) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetDefault(_ context.Context) (*store.AgentData, error)        { return nil, nil }
func (s *stubAgentStore) Update(_ context.Context, _ uuid.UUID, _ map[string]any) error { return nil }
func (s *stubAgentStore) Delete(_ context.Context, _ uuid.UUID) error                   { return nil }
func (s *stubAgentStore) List(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) ShareAgent(_ context.Context, _ uuid.UUID, _, _, _ string) error { return nil }
func (s *stubAgentStore) RevokeShare(_ context.Context, _ uuid.UUID, _ string) error      { return nil }
func (s *stubAgentStore) ListShares(_ context.Context, _ uuid.UUID) ([]store.AgentShareData, error) {
	return nil, nil
}
func (s *stubAgentStore) CanAccess(_ context.Context, _ uuid.UUID, _ string) (bool, string, error) {
	return true, "admin", nil
}
func (s *stubAgentStore) ListAccessible(_ context.Context, _ string) ([]store.AgentData, error) {
	return nil, nil
}
func (s *stubAgentStore) GetUserOverride(_ context.Context, _ uuid.UUID, _ string) (*store.UserAgentOverrideData, error) {
	return nil, nil
}
func (s *stubAgentStore) SetUserOverride(_ context.Context, _ *store.UserAgentOverrideData) error {
	return nil
}
func (s *stubAgentStore) GetOrCreateUserProfile(_ context.Context, _ uuid.UUID, _, _, _ string) (bool, string, error) {
	return false, "", nil
}
func (s *stubAgentStore) ListUserInstances(_ context.Context, _ uuid.UUID) ([]store.UserInstanceData, error) {
	return nil, nil
}
func (s *stubAgentStore) UpdateUserProfileMetadata(_ context.Context, _ uuid.UUID, _ string, _ map[string]string) error {
	return nil
}
func (s *stubAgentStore) EnsureUserProfile(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *stubAgentStore) PropagateContextFile(_ context.Context, _ uuid.UUID, _ string) (int, error) {
	return 0, nil
}
// ---- Tests ----

// TestInterceptor_CacheHit verifies that a second read does NOT call GetAgentContextFiles again.
func TestInterceptor_CacheHit(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{
		agentFiles: []store.AgentContextFileData{
			{AgentID: agentID, FileName: "SOUL.md", Content: "you are helpful"},
		},
	}
	intc := NewContextFileInterceptor(as, "",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	ctx := store.WithAgentID(context.Background(), agentID)

	// First read — cache miss → goes to store
	content1, handled1, err := intc.readAgentFile(ctx, agentID, "SOUL.md")
	if err != nil || !handled1 || content1 != "you are helpful" {
		t.Fatalf("first read: want ('you are helpful', true, nil), got (%q, %v, %v)", content1, handled1, err)
	}
	if n := as.agentCallsN.Load(); n != 1 {
		t.Fatalf("expected 1 store call, got %d", n)
	}

	// Second read — cache hit → should NOT call store again
	content2, _, _ := intc.readAgentFile(ctx, agentID, "SOUL.md")
	if content2 != "you are helpful" {
		t.Errorf("second read: expected cached content, got %q", content2)
	}
	if n := as.agentCallsN.Load(); n != 1 {
		t.Errorf("cache hit should not call store again, got %d calls", n)
	}
}

// TestInterceptor_InvalidateAgent_ClearsCache verifies that after InvalidateAgent,
// the next read fetches fresh content from the store (not cached stale content).
func TestInterceptor_InvalidateAgent_ClearsCache(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{
		agentFiles: []store.AgentContextFileData{
			{AgentID: agentID, FileName: "SOUL.md", Content: "old content"},
		},
	}
	intc := NewContextFileInterceptor(as, "",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)
	ctx := store.WithAgentID(context.Background(), agentID)

	// Warm up cache with old content
	intc.readAgentFile(ctx, agentID, "SOUL.md")
	if n := as.agentCallsN.Load(); n != 1 {
		t.Fatalf("expected 1 store call after warm-up, got %d", n)
	}

	// Simulate wizard writing new content to the store
	as.agentFiles = []store.AgentContextFileData{
		{AgentID: agentID, FileName: "SOUL.md", Content: "new wizard content"},
	}

	// WITHOUT invalidation: stale cache is still served
	content, _, _ := intc.readAgentFile(ctx, agentID, "SOUL.md")
	if content != "old content" {
		t.Errorf("expected stale cached content before invalidation, got %q", content)
	}
	if n := as.agentCallsN.Load(); n != 1 {
		t.Errorf("expected no extra store call (cache hit), got %d", n)
	}

	// Invalidate — this is what agents.files.set must call
	intc.InvalidateAgent(agentID)

	// AFTER invalidation: must fetch from store and return fresh content
	content, _, err := intc.readAgentFile(ctx, agentID, "SOUL.md")
	if err != nil {
		t.Fatalf("read after invalidation: unexpected error: %v", err)
	}
	if content != "new wizard content" {
		t.Errorf("after invalidation expected fresh content, got %q", content)
	}
	if n := as.agentCallsN.Load(); n != 2 {
		t.Errorf("expected 2 store calls total (1 warm-up + 1 post-invalidate), got %d", n)
	}
}

// TestInterceptor_InvalidateAgent_ClearsUserCache verifies that InvalidateAgent
// also evicts per-user cache entries for that agent.
func TestInterceptor_InvalidateAgent_ClearsUserCache(t *testing.T) {
	agentID := uuid.New()
	userID := "user-42"
	as := &stubAgentStore{
		userFiles: []store.UserContextFileData{
			{AgentID: agentID, UserID: userID, FileName: "USER.md", Content: "old user content"},
		},
	}
	intc := NewContextFileInterceptor(as, "",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	// Warm user cache
	intc.readUserFile(context.Background(), agentID, userID, "USER.md")

	// Update store content
	as.userFiles = []store.UserContextFileData{
		{AgentID: agentID, UserID: userID, FileName: "USER.md", Content: "new user content"},
	}

	// Verify cache is served before invalidation
	content, _, _ := intc.readUserFile(context.Background(), agentID, userID, "USER.md")
	if content != "old user content" {
		t.Errorf("expected stale user cache before invalidation, got %q", content)
	}

	// Invalidate the agent — must also clear user cache
	intc.InvalidateAgent(agentID)

	// Now fresh content should come through
	content, _, err := intc.readUserFile(context.Background(), agentID, userID, "USER.md")
	if err != nil {
		t.Fatalf("unexpected error after user cache invalidation: %v", err)
	}
	if content != "new user content" {
		t.Errorf("after InvalidateAgent expected fresh user content, got %q", content)
	}
}

// TestInterceptor_InvalidateAgent_DoesNotAffectOtherAgents verifies that
// invalidating one agent's cache does not evict another agent's entries.
func TestInterceptor_InvalidateAgent_DoesNotAffectOtherAgents(t *testing.T) {
	agentA := uuid.New()
	agentB := uuid.New()

	as := &stubAgentStore{}
	as.agentFiles = []store.AgentContextFileData{
		{AgentID: agentA, FileName: "SOUL.md", Content: "agent A soul"},
	}

	intc := NewContextFileInterceptor(as, "",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)
	ctx := context.Background()

	// Warm agent A cache
	intc.readAgentFile(ctx, agentA, "SOUL.md")
	callsAfterWarmup := as.agentCallsN.Load()

	// Invalidate agent B — must not touch agent A's cache
	intc.InvalidateAgent(agentB)

	// Agent A should still be served from cache (no extra store call)
	intc.readAgentFile(ctx, agentA, "SOUL.md")
	if n := as.agentCallsN.Load(); n != callsAfterWarmup {
		t.Errorf("invalidating agent B should not evict agent A's cache: got %d store calls", n)
	}
}

// TestInterceptor_TTLExpiry verifies that entries older than TTL are re-fetched.
func TestInterceptor_TTLExpiry(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{
		agentFiles: []store.AgentContextFileData{
			{AgentID: agentID, FileName: "SOUL.md", Content: "soul content"},
		},
	}
	intc := NewContextFileInterceptor(as, "",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)
	intc.ttl = 10 * time.Millisecond // very short TTL for testing
	ctx := context.Background()

	intc.readAgentFile(ctx, agentID, "SOUL.md")
	if n := as.agentCallsN.Load(); n != 1 {
		t.Fatalf("expected 1 store call, got %d", n)
	}

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	// Should fetch from store again after TTL
	intc.readAgentFile(ctx, agentID, "SOUL.md")
	if n := as.agentCallsN.Load(); n != 2 {
		t.Errorf("after TTL expiry expected 2 store calls, got %d", n)
	}
}

// TestInterceptor_AllowsCapabilitiesWrite verifies that a predefined agent
// with self_evolve=true can write to CAPABILITIES.md (routed to agent-level store).
func TestInterceptor_AllowsCapabilitiesWrite(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{}
	intc := NewContextFileInterceptor(as, "/workspace",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	ctx := store.WithAgentID(context.Background(), agentID)
	ctx = store.WithAgentType(ctx, store.AgentTypePredefined)
	ctx = store.WithUserID(ctx, "user-1")
	ctx = store.WithSelfEvolve(ctx, true)

	handled, err := intc.WriteFile(ctx, "CAPABILITIES.md", "domain: finance")
	if err != nil {
		t.Fatalf("expected CAPABILITIES.md write to succeed with self_evolve=true, got error: %v", err)
	}
	if !handled {
		t.Fatal("expected CAPABILITIES.md to be handled as context file")
	}
	if n := as.setAgentCallN.Load(); n != 1 {
		t.Errorf("expected 1 SetAgentContextFile call, got %d", n)
	}
}

// TestInterceptor_BlocksCapabilitiesWithoutSelfEvolve verifies that a predefined agent
// without self_evolve cannot write to CAPABILITIES.md.
func TestInterceptor_BlocksCapabilitiesWithoutSelfEvolve(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{}
	intc := NewContextFileInterceptor(as, "/workspace",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	ctx := store.WithAgentID(context.Background(), agentID)
	ctx = store.WithAgentType(ctx, store.AgentTypePredefined)
	ctx = store.WithUserID(ctx, "user-1")
	// self_evolve NOT set (defaults to false)

	handled, err := intc.WriteFile(ctx, "CAPABILITIES.md", "domain: finance")
	if !handled {
		t.Fatal("expected CAPABILITIES.md to be handled as context file")
	}
	if err == nil {
		t.Fatal("expected error when writing CAPABILITIES.md without self_evolve")
	}
	if !strings.Contains(err.Error(), "predefined configuration") {
		t.Errorf("expected predefined-config error, got: %v", err)
	}
}

// TestInterceptor_AllowsCapabilitiesRead verifies that a predefined agent
// with self_evolve=true can read CAPABILITIES.md (needed before updating).
func TestInterceptor_AllowsCapabilitiesRead(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{
		agentFiles: []store.AgentContextFileData{
			{AgentID: agentID, FileName: "CAPABILITIES.md", Content: "domain: finance"},
		},
	}
	intc := NewContextFileInterceptor(as, "/workspace",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	ctx := store.WithAgentID(context.Background(), agentID)
	ctx = store.WithAgentType(ctx, store.AgentTypePredefined)
	ctx = store.WithUserID(ctx, "user-1")
	ctx = store.WithSelfEvolve(ctx, true)

	content, handled, err := intc.ReadFile(ctx, "CAPABILITIES.md")
	if err != nil {
		t.Fatalf("expected read to succeed with self_evolve=true, got error: %v", err)
	}
	if !handled {
		t.Fatal("expected CAPABILITIES.md to be handled")
	}
	if content != "domain: finance" {
		t.Errorf("expected content 'domain: finance', got %q", content)
	}
}

// TestInterceptor_BlocksCapabilitiesReadWithoutSelfEvolve verifies that a predefined agent
// without self_evolve cannot read CAPABILITIES.md via read_file.
func TestInterceptor_BlocksCapabilitiesReadWithoutSelfEvolve(t *testing.T) {
	agentID := uuid.New()
	as := &stubAgentStore{}
	intc := NewContextFileInterceptor(as, "/workspace",
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
		cache.NewInMemoryCache[[]store.AgentContextFileData](),
	)

	ctx := store.WithAgentID(context.Background(), agentID)
	ctx = store.WithAgentType(ctx, store.AgentTypePredefined)
	ctx = store.WithUserID(ctx, "user-1")

	_, _, err := intc.ReadFile(ctx, "CAPABILITIES.md")
	if err == nil {
		t.Fatal("expected error when reading CAPABILITIES.md without self_evolve")
	}
	if !strings.Contains(err.Error(), "already loaded into your context") {
		t.Errorf("expected context-loaded error, got: %v", err)
	}
}
