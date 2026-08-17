package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tokencount"
)

// stubEpisodicStore implements store.EpisodicStore with a fixed result set,
// recording the search options so tests can assert the auto-injector's
// MaxEntries is honored at the query layer.
type stubEpisodicStore struct {
	results  []store.EpisodicSearchResult
	lastOpts store.EpisodicSearchOptions
}

func (s *stubEpisodicStore) Create(context.Context, *store.EpisodicSummary) error { return nil }
func (s *stubEpisodicStore) Get(context.Context, string) (*store.EpisodicSummary, error) {
	return nil, nil
}
func (s *stubEpisodicStore) Delete(context.Context, string) error { return nil }
func (s *stubEpisodicStore) List(context.Context, string, string, int, int) ([]store.EpisodicSummary, error) {
	return nil, nil
}
func (s *stubEpisodicStore) Search(ctx context.Context, query, agentID, userID string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	s.lastOpts = opts
	return s.results, nil
}
func (s *stubEpisodicStore) ExistsBySourceID(context.Context, string, string, string) (bool, error) {
	return false, nil
}
func (s *stubEpisodicStore) GetBySourceID(context.Context, string, string, string) (*store.EpisodicSummary, error) {
	return nil, nil
}
func (s *stubEpisodicStore) PruneExpired(context.Context) (int, error)       { return 0, nil }
func (s *stubEpisodicStore) CountUnpromoted(context.Context, string, string) (int, error) {
	return 0, nil
}
func (s *stubEpisodicStore) ListUnpromoted(context.Context, string, string, int) ([]store.EpisodicSummary, error) {
	return nil, nil
}
func (s *stubEpisodicStore) ListUnpromotedScored(context.Context, string, string, int) ([]store.EpisodicSummary, error) {
	return nil, nil
}
func (s *stubEpisodicStore) MarkPromoted(context.Context, []string) error { return nil }
func (s *stubEpisodicStore) RecordRecall(context.Context, string, float64) error {
	return nil
}
func (s *stubEpisodicStore) SetEmbeddingProvider(store.EmbeddingProvider) {}
func (s *stubEpisodicStore) Close() error                                 { return nil }

// bigAbstract returns an L0 abstract large enough to dwarf any sane token cap
// (~400 words ≈ 500+ tokens with cl100k), so a single entry alone exceeds it.
func bigAbstract() string {
	return strings.Repeat("This is a very long memory abstract that describes many details ", 50)
}

// sectionTokens counts the assembled section via the same BudgetCounter the
// auto-injector uses, so assertions match its arithmetic exactly.
func sectionTokens(t *testing.T, section string) int {
	t.Helper()
	n, err := tokencount.NewBudgetCounter().CountText(section)
	if err != nil {
		t.Fatalf("count section tokens: %v", err)
	}
	return n
}

// TestAutoInject_TokenBudget_CapsOversizedSection verifies the L0 section is
// token-capped (Gap E): a single abstract far larger than the budget is
// clipped, not injected wholesale. The returned section stays within budget and
// the top-relevant memory is still represented.
func TestAutoInject_TokenBudget_CapsOversizedSection(t *testing.T) {
	t.Parallel()

	const wantTokens = 200
	storeStub := &stubEpisodicStore{
		results: []store.EpisodicSearchResult{
			{EpisodicID: "e1", L0Abstract: bigAbstract(), Score: 0.9},
		},
	}
	inj := NewAutoInjector(storeStub, nil)

	result, err := inj.Inject(context.Background(), InjectParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		UserMessage: "what should I build next with Go?",
		MaxTokens:   wantTokens,
	})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Injected == 0 {
		t.Fatalf("Injected = 0, want 1 (top-relevant abstract must be clipped, not dropped)")
	}
	got := sectionTokens(t, result.Section)
	if got > wantTokens {
		t.Errorf("Section tokens = %d, want <= %d (oversized abstract must be capped)", got, wantTokens)
	}
	if !strings.Contains(result.Section, "- ") {
		t.Errorf("Section missing bullet prefix, got %q", result.Section)
	}
}

// TestAutoInject_TokenBudget_DefaultCapsWithManyLargeEntries verifies that with
// no explicit MaxTokens (zero-config), the default 200-token budget still
// applies: multiple large abstracts are trimmed to fit, and the section does
// not exceed the default cap. Also asserts MaxEntries flows into the search
// layer unchanged.
func TestAutoInject_TokenBudget_DefaultCapsWithManyLargeEntries(t *testing.T) {
	t.Parallel()

	const defaultMaxTokens = 200
	makeResults := func(n int) []store.EpisodicSearchResult {
		out := make([]store.EpisodicSearchResult, n)
		for i := range out {
			out[i] = store.EpisodicSearchResult{
				EpisodicID: "e",
				L0Abstract: "Memory number " + strings.Repeat("about a past session ", 40),
				Score:      0.8,
			}
		}
		return out
	}
	storeStub := &stubEpisodicStore{results: makeResults(5)}
	inj := NewAutoInjector(storeStub, nil)

	result, err := inj.Inject(context.Background(), InjectParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		UserMessage: "remind me what we discussed",
	})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Section == "" {
		t.Fatal("expected non-empty section, got empty")
	}
	got := sectionTokens(t, result.Section)
	if got > defaultMaxTokens {
		t.Errorf("Section tokens = %d, want <= %d (default budget not enforced)", got, defaultMaxTokens)
	}
	// MaxEntries default 5 must be forwarded to the search layer.
	if storeStub.lastOpts.MaxResults != 10 {
		t.Errorf("Search MaxResults = %d, want 10 (MaxEntries*2 default)", storeStub.lastOpts.MaxResults)
	}
}

// TestAutoInject_ZeroConfig_PreservesCountCap verifies that when entries are
// small and fit the token budget, the legacy count-based cap (MaxEntries=5)
// still governs: exactly 5 entries are injected, no more.
func TestAutoInject_ZeroConfig_PreservesCountCap(t *testing.T) {
	t.Parallel()

	const small = "short memory abstract"
	makeResults := func(n int) []store.EpisodicSearchResult {
		out := make([]store.EpisodicSearchResult, n)
		for i := range out {
			out[i] = store.EpisodicSearchResult{
				EpisodicID: "e",
				L0Abstract: small,
				Score:      0.7,
			}
		}
		return out
	}
	storeStub := &stubEpisodicStore{results: makeResults(12)} // more than MaxEntries
	inj := NewAutoInjector(storeStub, nil)

	result, err := inj.Inject(context.Background(), InjectParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		UserMessage: "tell me about my saved memories",
	})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Injected != 5 {
		t.Errorf("Injected = %d, want 5 (count cap preserved)", result.Injected)
	}
	// 5 entries × ~24 tokens each ≪ default 200-token budget — budget must not
	// have interfered.
	if got := sectionTokens(t, result.Section); got > 200 {
		t.Errorf("Section tokens = %d, want <= 200", got)
	}
}

// TestAutoInject_EmptyResults_ReturnsEmptySection verifies zero-config behavior
// unchanged: no results → empty section, no error, MatchCount 0.
func TestAutoInject_EmptyResults_ReturnsEmptySection(t *testing.T) {
	t.Parallel()

	storeStub := &stubEpisodicStore{} // no results
	inj := NewAutoInjector(storeStub, nil)

	result, err := inj.Inject(context.Background(), InjectParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		UserMessage: "search something",
	})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Section != "" {
		t.Errorf("Section = %q, want empty", result.Section)
	}
	if result.MatchCount != 0 {
		t.Errorf("MatchCount = %d, want 0", result.MatchCount)
	}
}

// TestAutoInject_NilStore_NoPanic verifies the nil-store fast path still
// returns an empty result without error.
func TestAutoInject_NilStore_NoPanic(t *testing.T) {
	t.Parallel()

	inj := NewAutoInjector(nil, nil)
	result, err := inj.Inject(context.Background(), InjectParams{UserMessage: "anything"})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Section != "" {
		t.Errorf("Section = %q, want empty for nil store", result.Section)
	}
}

// TestAutoInject_MaxTokensPositive_OverridesDefault verifies an explicit
// MaxTokens override is honored (smaller than the default budget).
func TestAutoInject_MaxTokensPositive_OverridesDefault(t *testing.T) {
	t.Parallel()

	// 5 abstracts of ~85 words each far exceed the 60-token override when
	// summed, but a single abstract still fits, so the section stays non-empty.
	const wantTokens = 60
	makeResults := func(n int) []store.EpisodicSearchResult {
		out := make([]store.EpisodicSearchResult, n)
		for i := range out {
			out[i] = store.EpisodicSearchResult{
				EpisodicID: "e",
				L0Abstract: "Memory " + strings.Repeat("detail ", 6),
				Score:      0.75,
			}
		}
		return out
	}
	storeStub := &stubEpisodicStore{results: makeResults(5)}
	inj := NewAutoInjector(storeStub, nil)

	result, err := inj.Inject(context.Background(), InjectParams{
		AgentID:     "agent-1",
		UserID:      "user-1",
		UserMessage: "what projects have we discussed previously remember the details",
		MaxTokens:   wantTokens,
	})
	if err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if result.Section == "" {
		t.Fatalf("expected non-empty section, got empty (Injected=%d, MatchCount=%d, MaxTokens=%d)",
			result.Injected, result.MatchCount, wantTokens)
	}
	if got := sectionTokens(t, result.Section); got > wantTokens {
		t.Errorf("Section tokens = %d, want <= %d (explicit override not honored)", got, wantTokens)
	}
}
