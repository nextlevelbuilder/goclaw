package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/contract"
	orchestration "github.com/nextlevelbuilder/goclaw/internal/orchestration"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// testContractStore is an in-memory ContractStore fixture.
type testContractStore struct {
	mu      sync.Mutex
	records []store.ContractRecord
}

func (s *testContractStore) CreateContractRecord(_ context.Context, rec *store.ContractRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	s.records = append(s.records, *rec)
	return nil
}

func (s *testContractStore) GetContractRecord(context.Context, uuid.UUID) (*store.ContractRecord, error) {
	return nil, nil
}

func (s *testContractStore) ListContractRecords(context.Context, store.ContractRecordListOpts) ([]store.ContractRecord, error) {
	return nil, nil
}

func (s *testContractStore) UpdateContractRecordStatus(context.Context, uuid.UUID, string) error { return nil }

func (s *testContractStore) last() *store.ContractRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.records) == 0 {
		return nil
	}
	rec := s.records[len(s.records)-1]
	return &rec
}

// testArtifactStore is an in-memory ArtifactStore fixture.
type testArtifactStore struct {
	mu        sync.Mutex
	artifacts []store.Artifact
}

func (s *testArtifactStore) CreateArtifact(_ context.Context, art *store.Artifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if art.ID == uuid.Nil {
		art.ID = uuid.New()
	}
	if art.Version == 0 {
		art.Version = 1
	}
	if art.Checksum == "" {
		art.Checksum = store.ArtifactChecksum(art.Content)
	}
	s.artifacts = append(s.artifacts, *art)
	return nil
}

func (s *testArtifactStore) GetArtifact(context.Context, uuid.UUID) (*store.Artifact, error) { return nil, nil }
func (s *testArtifactStore) ListArtifacts(context.Context, store.ArtifactListOpts) ([]store.Artifact, error) {
	return nil, nil
}
func (s *testArtifactStore) GetVersionChain(context.Context, uuid.UUID, *uuid.UUID) ([]store.Artifact, error) {
	return nil, nil
}
func (s *testArtifactStore) MarkArtifactStatus(context.Context, uuid.UUID, string) error { return nil }

func (s *testArtifactStore) all() []store.Artifact {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]store.Artifact(nil), s.artifacts...)
}

// fixedDelegateRunner echoes the requested task into DelegateResult.Content so
// fan-out results are deterministic for verdict assertions.
func fixedDelegateRunner(content func(task string) string) DelegateRunFunc {
	return func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
		return DelegateResult{Content: content(req.Task)}, nil
	}
}

func juryTestCtx() context.Context {
	return store.WithTenantID(context.Background(), uuid.New())
}

// TestJuryTool_RunsFanOutAndApprove verifies that the tool runs each
// strategy through the delegate runner, judges an approve verdict, and
// persists a closed competition record.
func TestJuryTool_RunsFanOutAndApprove(t *testing.T) {
	contracts := &testContractStore{}
	artifacts := &testArtifactStore{}
	tool := NewJuryTool(
		fixedDelegateRunner(func(task string) string { return strings.Repeat("solution for "+task, 40) }),
		contracts,
		artifacts,
	)

	result := tool.Execute(juryTestCtx(), map[string]any{
		"task":       "build a parser",
		"agent":      "coder-a",
		"strategies": []any{"simplest", "performance"},
		"criteria":   []any{"correctness"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var payload struct {
		Decision      string  `json:"decision"`
		ContenderID   string  `json:"contender_id"`
		Score         float64 `json:"score"`
		WinningOutput string  `json:"winning_output"`
	}
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("result not JSON: %v", err)
	}
	if payload.Decision != "approve" {
		t.Errorf("decision = %q, want approve", payload.Decision)
	}
	if payload.WinningOutput == "" {
		t.Error("expected a winning output")
	}

	rec := contracts.last()
	if rec == nil {
		t.Fatal("expected a persisted contract record")
	}
	if rec.Kind != store.ContractRecordCompetition {
		t.Errorf("kind = %q, want %q", rec.Kind, store.ContractRecordCompetition)
	}
	if rec.Status != store.ContractRecordClosed {
		t.Errorf("status = %q, want closed", rec.Status)
	}
	if rec.Body == "" {
		t.Error("expected a non-empty body")
	}

	arts := artifacts.all()
	if len(arts) != 1 {
		t.Fatalf("expected 1 review artifact, got %d", len(arts))
	}
	if arts[0].Type != store.ArtifactTypeReview {
		t.Errorf("artifact type = %q, want review", arts[0].Type)
	}
	if arts[0].Status != store.ArtifactStatusFinal {
		t.Errorf("artifact status = %q, want final", arts[0].Status)
	}
}

// TestJuryTool_RejectsWhenNoContenderScores verifies that a round where every
// contender fails produces a reject verdict and a persisted record but no
// review artifact.
func TestJuryTool_RejectsWhenNoContenderScores(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewJuryTool(
		fixedDelegateRunner(func(task string) string { return "" }), // empty content scores 0
		contracts,
		nil,
	)

	result := tool.Execute(juryTestCtx(), map[string]any{
		"task":  "guess the answer",
		"agent": "coder-a",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, `"reject"`) {
		t.Errorf("expected reject verdict, got: %s", result.ForLLM)
	}
	rec := contracts.last()
	if rec == nil {
		t.Fatal("expected a persisted contract record")
	}
	if rec.Status != store.ContractRecordClosed {
		t.Errorf("status = %q, want closed", rec.Status)
	}
}

// TestJuryTool_ReturnsErrorWithoutTask verifies the required-task guard and
// the nil-runner fail-safe.
func TestJuryTool_ReturnsErrorWithoutTask(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewJuryTool(nil, contracts, nil)

	result := tool.Execute(juryTestCtx(), nil)
	if !result.IsError {
		t.Fatal("expected error for missing task")
	}

	// A task with an agent but no runner must fail closed on the runner.
	result = tool.Execute(juryTestCtx(), map[string]any{
		"task":  "work",
		"agent": "coder-a",
	})
	if !result.IsError {
		t.Fatal("expected error for nil delegate runner")
	}
	if contracts.last() != nil {
		t.Fatal("expected no persisted record for failed execution")
	}
}

// TestJuryTool_ScoringRespectsLabels verifies that the simplest strategy wins
// when the criteria prefer brevity over ponderous correctness.
func TestJuryTool_ScoringRespectsLabels(t *testing.T) {
	contracts := &testContractStore{}
	// The runner emits a verbose solution for every target except the first
	// contender, which stays concise.
	tool := NewJuryTool(
		func(_ context.Context, req DelegateRequest) (DelegateResult, error) {
			if req.ToAgentKey == "coder-a" {
				return DelegateResult{Content: "OK"}, nil
			}
			return DelegateResult{Content: strings.Repeat("very long and detailed explanation...", 50)}, nil
		},
		contracts,
		nil,
	)

	result := tool.Execute(juryTestCtx(), map[string]any{
		"task":       "short or long?",
		"agents":     []any{"coder-a", "coder-b"},
		"strategies": []any{"simplest", "performance"},
		"criteria":   []any{"simplest"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, `"contender-0"`) {
		t.Errorf("expected simplest contender-0 to win on brevity, got: %s", result.ForLLM)
	}
}

// TestJuryTool_CustomJudge verifies the pluggable judge path is honored: a
// judge that always approves contender-1 must override the default heuristic.
func TestJuryTool_CustomJudge(t *testing.T) {
	contracts := &testContractStore{}
	tool := NewJuryTool(
		fixedDelegateRunner(func(task string) string { return "same content" }),
		contracts,
		nil,
	)
	tool.SetJudge(func(_ context.Context, _ []orchestration.Contestant, _ []orchestration.ChildResult, _ orchestration.JudgeOpts) (contract.Verdict, error) {
		return contract.Verdict{ContenderID: "contender-1", Decision: "approve", Score: 0.9, Reason: "custom rubric"}, nil
	})

	result := tool.Execute(juryTestCtx(), map[string]any{
		"task":       "choose winner",
		"agents":     []any{"coder-a", "coder-b"},
		"strategies": []any{"a", "b"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, `"contender-1"`) {
		t.Errorf("expected custom judge to pick contender-1, got: %s", result.ForLLM)
	}
}