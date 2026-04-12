package pg

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- agents_scan_rows.go ---

func TestAgentShareRow_ToAgentShareData(t *testing.T) {
	id := uuid.New()
	aid := uuid.New()
	created := time.Now()
	r := agentShareRow{
		ID:        id,
		AgentID:   aid,
		UserID:    "user-1",
		Role:      "editor",
		GrantedBy: "admin",
		CreatedAt: created,
	}
	got := r.toAgentShareData()
	if got.ID != id || got.AgentID != aid {
		t.Errorf("IDs mismatch: %+v", got)
	}
	if got.UserID != "user-1" || got.Role != "editor" || got.GrantedBy != "admin" {
		t.Errorf("fields: %+v", got)
	}
	if !got.CreatedAt.Equal(created) {
		t.Errorf("CreatedAt mismatch: got %v want %v", got.CreatedAt, created)
	}
}

func TestUserInstanceRow_ToUserInstanceData(t *testing.T) {
	first := "2026-04-11T00:00:00Z"
	last := "2026-04-11T12:00:00Z"
	r := userInstanceRow{
		UserID:      "u1",
		FirstSeenAt: &first,
		LastSeenAt:  &last,
		FileCount:   3,
		MetadataRaw: []byte(`{"locale":"vi","tier":"pro"}`),
	}
	got := r.toUserInstanceData()
	if got.UserID != "u1" || got.FileCount != 3 {
		t.Errorf("basic fields: %+v", got)
	}
	if got.Metadata["locale"] != "vi" || got.Metadata["tier"] != "pro" {
		t.Errorf("metadata: %+v", got.Metadata)
	}
}

func TestUserInstanceRow_EmptyMetadataRaw(t *testing.T) {
	r := userInstanceRow{UserID: "u1", FileCount: 0}
	got := r.toUserInstanceData()
	if got.Metadata != nil {
		t.Errorf("expected nil metadata, got %v", got.Metadata)
	}
}

func TestUserInstanceRow_MalformedMetadata(t *testing.T) {
	// Malformed JSON is swallowed (errcheck disabled), should not panic.
	r := userInstanceRow{MetadataRaw: []byte(`{broken`)}
	got := r.toUserInstanceData()
	_ = got // zero-valued metadata is fine
}

// --- memory_scan_rows.go ---

func TestDocumentInfoRow_ToDocumentInfo(t *testing.T) {
	updated := time.Unix(1700000000, 0)
	uid := "user-42"
	r := documentInfoRow{
		AgentID:   "agent-1",
		Path:      "/mem/doc.md",
		Hash:      "abc",
		UserID:    &uid,
		UpdatedAt: updated,
	}
	got := r.toDocumentInfo()
	if got.AgentID != "agent-1" || got.Path != "/mem/doc.md" || got.Hash != "abc" {
		t.Errorf("%+v", got)
	}
	if got.UserID != "user-42" {
		t.Errorf("UserID = %q, want user-42", got.UserID)
	}
	if got.UpdatedAt != updated.UnixMilli() {
		t.Errorf("UpdatedAt = %d, want %d", got.UpdatedAt, updated.UnixMilli())
	}
}

func TestDocumentInfoRow_NilUserID(t *testing.T) {
	r := documentInfoRow{AgentID: "a", Path: "p", Hash: "h", UpdatedAt: time.Now()}
	got := r.toDocumentInfo()
	if got.UserID != "" {
		t.Errorf("nil userID should produce empty string, got %q", got.UserID)
	}
}

func TestDocumentDetailRow_ToDocumentDetail(t *testing.T) {
	created := time.Unix(1700000000, 0)
	updated := time.Unix(1700000100, 0)
	uid := "user-7"
	r := documentDetailRow{
		Path: "/p.md", Content: "body", Hash: "h",
		UserID: &uid, CreatedAt: created, UpdatedAt: updated,
		ChunkCount: 5, EmbeddedCount: 3,
	}
	got := r.toDocumentDetail()
	if got.Path != "/p.md" || got.Content != "body" {
		t.Errorf("%+v", got)
	}
	if got.UserID != "user-7" {
		t.Errorf("UserID = %q", got.UserID)
	}
	if got.ChunkCount != 5 || got.EmbeddedCount != 3 {
		t.Errorf("counts: %+v", got)
	}
	if got.CreatedAt != created.UnixMilli() || got.UpdatedAt != updated.UnixMilli() {
		t.Errorf("timestamps: %+v", got)
	}
}

func TestDocumentDetailRow_NilUserID(t *testing.T) {
	r := documentDetailRow{Path: "p"}
	got := r.toDocumentDetail()
	if got.UserID != "" {
		t.Errorf("UserID = %q, want empty", got.UserID)
	}
}

func TestChunkInfoRow_ToChunkInfo(t *testing.T) {
	r := chunkInfoRow{ID: "chunk-1", StartLine: 10, EndLine: 20, TextPreview: "preview", HasEmbedding: true}
	got := r.toChunkInfo()
	if got != (store.ChunkInfo{
		ID: "chunk-1", StartLine: 10, EndLine: 20, TextPreview: "preview", HasEmbedding: true,
	}) {
		t.Errorf("%+v", got)
	}
}

func TestScoredChunkRow_ToScoredChunk(t *testing.T) {
	uid := "u1"
	r := scoredChunkRow{
		Path: "p", StartLine: 1, EndLine: 5, Text: "text", UserID: &uid, Score: 0.7,
	}
	got := r.toScoredChunk()
	if got.Path != "p" || got.Score != 0.7 {
		t.Errorf("%+v", got)
	}
	if got.UserID == nil || *got.UserID != "u1" {
		t.Errorf("UserID pointer lost: %v", got.UserID)
	}
}

func TestEpisodicSummaryRow_ToEpisodicSummary(t *testing.T) {
	created := time.Now()
	tid := uuid.New()
	agentID := uuid.New()
	r := episodicSummaryRow{
		ID:         uuid.New().String(),
		TenantID:   tid.String(),
		AgentID:    agentID.String(),
		UserID:     "u1",
		SessionKey: "sess-1",
		Summary:    "did X",
		KeyTopics:  pq.StringArray{"topic-a", "topic-b"},
		TurnCount:  5, TokenCount: 100,
		L0Abstract: "abs",
		SourceID:   "src-1", SourceType: "session",
		CreatedAt:   created,
		RecallCount: 2, RecallScore: 0.8,
	}
	got := r.toEpisodicSummary()
	if got.UserID != "u1" || got.SessionKey != "sess-1" || got.Summary != "did X" {
		t.Errorf("%+v", got)
	}
	if len(got.KeyTopics) != 2 || got.KeyTopics[0] != "topic-a" {
		t.Errorf("topics = %v", got.KeyTopics)
	}
	if got.TurnCount != 5 || got.TokenCount != 100 {
		t.Errorf("counts: %+v", got)
	}
	if got.RecallCount != 2 || got.RecallScore != 0.8 {
		t.Errorf("recall: %+v", got)
	}
}

func TestEpisodicScoredRow_ToEpisodicScored(t *testing.T) {
	created := time.Now()
	r := episodicScoredRow{
		ID: "ep-1", SessionKey: "s-1", L0Abstract: "abs", Score: 0.5, CreatedAt: created,
	}
	got := r.toEpisodicScored()
	if got.id != "ep-1" || got.sessionKey != "s-1" || got.l0 != "abs" || got.score != 0.5 {
		t.Errorf("%+v", got)
	}
	if !got.createdAt.Equal(created) {
		t.Errorf("createdAt mismatch")
	}
}

// --- sessions_scan_rows.go ---

func TestSessionListRow_ToSessionInfo(t *testing.T) {
	label := "my-session"
	ch := "telegram"
	uid := "u1"
	meta := `{"topic":"support","locale":"vi"}`
	created := time.Unix(1700000000, 0)
	updated := time.Unix(1700000100, 0)
	r := sessionListRow{
		Key:      "session-key",
		MsgsJSON: []byte(`[]`),
		Created:  created, Updated: updated,
		Label: &label, Channel: &ch, UserID: &uid,
		MetaJSON: []byte(meta),
	}
	got := r.toSessionInfo(7)
	if got.Key != "session-key" || got.MessageCount != 7 {
		t.Errorf("%+v", got)
	}
	if got.Label != "my-session" || got.Channel != "telegram" || got.UserID != "u1" {
		t.Errorf("deref fields: %+v", got)
	}
	if got.Metadata["topic"] != "support" || got.Metadata["locale"] != "vi" {
		t.Errorf("metadata: %v", got.Metadata)
	}
}

func TestSessionListRow_NilPointers(t *testing.T) {
	r := sessionListRow{Key: "k"}
	got := r.toSessionInfo(0)
	if got.Label != "" || got.Channel != "" || got.UserID != "" {
		t.Errorf("nil deref should empty: %+v", got)
	}
	if got.Metadata != nil {
		t.Errorf("empty metadata should be nil, got %v", got.Metadata)
	}
}

func TestSessionPagedRow_ToSessionInfo(t *testing.T) {
	label := "L"
	r := sessionPagedRow{
		Key: "k", MsgCount: 10,
		Created: time.Now(), Updated: time.Now(),
		Label: &label,
	}
	got := r.toSessionInfo()
	if got.Key != "k" || got.MessageCount != 10 || got.Label != "L" {
		t.Errorf("%+v", got)
	}
}

func TestSessionRichRow_ToSessionInfoRich(t *testing.T) {
	model := "gpt-4o"
	provider := "openai"
	r := sessionRichRow{
		Key:             "k",
		MsgCount:        3,
		Created:         time.Now(),
		Updated:         time.Now(),
		Model:           &model,
		Provider:        &provider,
		InputTokens:     1000,
		OutputTokens:    500,
		AgentName:       "assistant",
		EstimatedTokens: 300,
		ContextWindow:   200000,
		CompactionCount: 1,
	}
	got := r.toSessionInfoRich()
	if got.Model != "gpt-4o" || got.Provider != "openai" {
		t.Errorf("deref: %+v", got)
	}
	if got.InputTokens != 1000 || got.OutputTokens != 500 {
		t.Errorf("tokens: %+v", got)
	}
	if got.AgentName != "assistant" || got.EstimatedTokens != 300 {
		t.Errorf("agent/est: %+v", got)
	}
	if got.ContextWindow != 200000 || got.CompactionCount != 1 {
		t.Errorf("window: %+v", got)
	}
}

// --- knowledge_graph_scan_rows.go ---

func TestEntityRow_ToEntity(t *testing.T) {
	created := time.Unix(1700000000, 0)
	r := entityRow{
		ID:          "e1",
		AgentID:     "a1",
		UserID:      "u1",
		ExternalID:  "ext-1",
		Name:        "Alice",
		EntityType:  "Person",
		Description: "friend",
		Properties:  json.RawMessage(`{"age":"30","role":"dev"}`),
		SourceID:    "src-1",
		Confidence:  0.9,
		CreatedAt:   created,
		UpdatedAt:   created,
	}
	got := r.toEntity()
	if got.ID != "e1" || got.Name != "Alice" || got.EntityType != "Person" {
		t.Errorf("%+v", got)
	}
	if got.Properties["age"] != "30" {
		t.Errorf("properties age = %v", got.Properties["age"])
	}
	if got.Properties["role"] != "dev" {
		t.Errorf("properties role = %v", got.Properties["role"])
	}
	if got.CreatedAt != created.UnixMilli() {
		t.Errorf("createdAt: %d", got.CreatedAt)
	}
}

func TestEntityRow_EmptyProperties(t *testing.T) {
	r := entityRow{ID: "e1", Name: "n"}
	got := r.toEntity()
	if got.Properties != nil {
		t.Errorf("empty props should be nil map, got %v", got.Properties)
	}
}

func TestEntityTemporalRow_PreservesTemporal(t *testing.T) {
	vf := time.Unix(1700000000, 0)
	vu := time.Unix(1700000100, 0)
	r := entityTemporalRow{
		entityRow:  entityRow{ID: "e1", Name: "n"},
		ValidFrom:  &vf,
		ValidUntil: &vu,
	}
	got := r.toEntity()
	if got.ValidFrom == nil || !got.ValidFrom.Equal(vf) {
		t.Errorf("ValidFrom lost: %v", got.ValidFrom)
	}
	if got.ValidUntil == nil || !got.ValidUntil.Equal(vu) {
		t.Errorf("ValidUntil lost: %v", got.ValidUntil)
	}
}

func TestRelationRow_ToRelation(t *testing.T) {
	created := time.Unix(1700000000, 0)
	r := relationRow{
		ID: "r1", AgentID: "a", UserID: "u",
		SourceEntityID: "s", RelationType: "KNOWS", TargetEntityID: "t",
		Confidence: 0.8,
		Properties: json.RawMessage(`{"since":"2020","strength":"strong"}`),
		CreatedAt:  created,
	}
	got := r.toRelation()
	if got.RelationType != "KNOWS" || got.SourceEntityID != "s" {
		t.Errorf("%+v", got)
	}
	if got.Properties["since"] != "2020" {
		t.Errorf("properties: %v", got.Properties)
	}
	if got.CreatedAt != created.UnixMilli() {
		t.Errorf("createdAt: %d", got.CreatedAt)
	}
}

func TestRelationExportRow_PreservesTemporal(t *testing.T) {
	vf := time.Unix(1700000000, 0)
	r := relationExportRow{
		relationRow: relationRow{ID: "r1", RelationType: "T"},
		ValidFrom:   &vf,
	}
	got := r.toRelation()
	if got.ValidFrom == nil || !got.ValidFrom.Equal(vf) {
		t.Errorf("ValidFrom lost")
	}
}

func TestTraversalRow_ToTraversalResult(t *testing.T) {
	r := traversalRow{
		entityRow: entityRow{ID: "e1", Name: "n"},
		Depth:     3,
		Path:      pq.StringArray{"e0", "e1"},
		Via:       "KNOWS",
	}
	got := r.toTraversalResult()
	if got.Depth != 3 || got.Via != "KNOWS" {
		t.Errorf("%+v", got)
	}
	if len(got.Path) != 2 {
		t.Errorf("path = %v", got.Path)
	}
}

func TestDedupCandidateRow_ToDedupCandidate(t *testing.T) {
	now := time.Now()
	r := dedupCandidateRow{
		ID: "dc-1", Similarity: 0.95, Status: "pending", CreatedAt: now,
		AID: "ea", AName: "Alice", AType: "Person", AProps: json.RawMessage(`{"k":"v"}`),
		BID: "eb", BName: "Bob", BType: "Person",
		ACreatedAt: now, AUpdatedAt: now, BCreatedAt: now, BUpdatedAt: now,
	}
	got := r.toDedupCandidate()
	if got.ID != "dc-1" || got.Similarity != 0.95 {
		t.Errorf("%+v", got)
	}
	if got.EntityA.Name != "Alice" || got.EntityB.Name != "Bob" {
		t.Errorf("entities: %+v / %+v", got.EntityA, got.EntityB)
	}
	if got.EntityA.Properties["k"] != "v" {
		t.Errorf("A props: %v", got.EntityA.Properties)
	}
}

// --- mcp_scan_rows.go ---

func TestMCPAccessRequestRow_ToMCPAccessRequest(t *testing.T) {
	id := uuid.New()
	serverID := uuid.New()
	agentID := uuid.New()
	uid := "u1"
	reviewedBy := "admin"
	reviewedAt := time.Now()
	reviewNote := "looks good"
	r := mcpAccessRequestRow{
		ID: id, ServerID: serverID, AgentID: &agentID,
		UserID: &uid,
		Scope:  "read", Status: "approved", Reason: "need access",
		ToolAllow:   []byte(`["read_file"]`),
		RequestedBy: "dev1",
		ReviewedBy:  &reviewedBy,
		ReviewedAt:  &reviewedAt,
		ReviewNote:  &reviewNote,
		CreatedAt:   time.Now(),
	}
	got := r.toMCPAccessRequest()
	if got.ID != id || got.ServerID != serverID {
		t.Errorf("IDs mismatch")
	}
	if got.AgentID == nil || *got.AgentID != agentID {
		t.Errorf("AgentID: %v", got.AgentID)
	}
	if got.UserID != "u1" || got.ReviewedBy != "admin" || got.ReviewNote != "looks good" {
		t.Errorf("deref: %+v", got)
	}
	if string(got.ToolAllow) != `["read_file"]` {
		t.Errorf("ToolAllow = %s", got.ToolAllow)
	}
}

func TestMCPAccessRequestRow_NilOptionals(t *testing.T) {
	r := mcpAccessRequestRow{
		ID: uuid.New(), ServerID: uuid.New(),
		Scope: "r", Status: "pending", Reason: "why",
		RequestedBy: "u",
	}
	got := r.toMCPAccessRequest()
	if got.AgentID != nil {
		t.Errorf("AgentID should be nil, got %v", got.AgentID)
	}
	if got.UserID != "" || got.ReviewedBy != "" || got.ReviewNote != "" {
		t.Errorf("deref should all be empty: %+v", got)
	}
	if got.ToolAllow != nil {
		t.Errorf("empty ToolAllow should be nil, got %v", got.ToolAllow)
	}
}

// --- cron_run_log_scan_rows.go ---

func TestCronRunLogRow_ToCronRunLogEntry(t *testing.T) {
	ranAt := time.Unix(1700000000, 0)
	errMsg := "boom"
	sum := "ok"
	r := cronRunLogRow{
		JobID: uuid.New(), Status: "success",
		Error: &errMsg, Summary: &sum,
		RanAt: ranAt, DurationMS: 500,
		InputTokens: 100, OutputTokens: 50,
	}
	got := r.toCronRunLogEntry()
	if got.JobID != r.JobID.String() {
		t.Errorf("JobID")
	}
	if got.Status != "success" || got.Error != "boom" || got.Summary != "ok" {
		t.Errorf("%+v", got)
	}
	if got.Ts != ranAt.UnixMilli() {
		t.Errorf("Ts = %d", got.Ts)
	}
	if got.DurationMS != 500 {
		t.Errorf("DurationMS = %d", got.DurationMS)
	}
	if got.InputTokens != 100 || got.OutputTokens != 50 {
		t.Errorf("tokens")
	}
}

func TestCronRunLogRow_NilErrorAndSummary(t *testing.T) {
	r := cronRunLogRow{JobID: uuid.New(), Status: "ok", RanAt: time.Now()}
	got := r.toCronRunLogEntry()
	if got.Error != "" || got.Summary != "" {
		t.Errorf("nil pointers should deref empty: %+v", got)
	}
}

// --- skills helpers (parseDepsColumn, parseFrontmatterAuthor, marshalFrontmatter, buildSkillInfo) ---

func TestParseDepsColumn(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want []string
	}{
		{"empty returns nil", nil, nil},
		{"valid missing list", []byte(`{"missing":["tool1","tool2"]}`), []string{"tool1", "tool2"}},
		{"empty missing list", []byte(`{"missing":[]}`), nil},
		{"no missing key", []byte(`{"other":"x"}`), nil},
		{"malformed JSON", []byte(`{broken`), nil},
		{"null", []byte(`null`), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseDepsColumn(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseFrontmatterAuthor(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want string
	}{
		{"empty returns empty", nil, ""},
		{"valid author", []byte(`{"author":"Alice"}`), "Alice"},
		{"no author key", []byte(`{"title":"X"}`), ""},
		{"malformed", []byte(`{broken`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseFrontmatterAuthor(tc.raw)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMarshalFrontmatter(t *testing.T) {
	cases := []struct {
		name string
		fm   map[string]string
		want string
	}{
		{"nil returns empty object", nil, "{}"},
		{"empty map returns empty object", map[string]string{}, "{}"},
		{"single key", map[string]string{"author": "Bob"}, `{"author":"Bob"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(marshalFrontmatter(tc.fm))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSkillInfo(t *testing.T) {
	desc := "a skill"
	id := uuid.New().String()
	// With nil filePath → BaseDir computed from baseDir/slug/version.
	info := buildSkillInfo(id, "TestSkill", "test-skill", &desc, 3, "/skills", nil)
	if info.ID != id || info.Name != "TestSkill" || info.Slug != "test-skill" {
		t.Errorf("%+v", info)
	}
	if info.BaseDir != "/skills/test-skill/3" {
		t.Errorf("BaseDir = %q", info.BaseDir)
	}
	if info.Path != "/skills/test-skill/3/SKILL.md" {
		t.Errorf("Path = %q", info.Path)
	}
	if info.Description != "a skill" {
		t.Errorf("Description = %q", info.Description)
	}
	if info.Source != "managed" {
		t.Errorf("Source = %q", info.Source)
	}
	if info.Version != 3 {
		t.Errorf("Version = %d", info.Version)
	}
}

func TestBuildSkillInfo_FilePathOverridesBaseDir(t *testing.T) {
	fp := "/custom/path/to/skill"
	info := buildSkillInfo("id", "n", "s", nil, 1, "/ignored", &fp)
	if info.BaseDir != "/custom/path/to/skill" {
		t.Errorf("BaseDir should honor filePath override: %q", info.BaseDir)
	}
	if info.Path != "/custom/path/to/skill/SKILL.md" {
		t.Errorf("Path = %q", info.Path)
	}
	if info.Description != "" {
		t.Errorf("nil desc should be empty: %q", info.Description)
	}
}

func TestBuildSkillInfo_EmptyFilePathFallsBack(t *testing.T) {
	empty := ""
	info := buildSkillInfo("id", "n", "slug", nil, 2, "/base", &empty)
	// Empty string filePath still falls back to baseDir path.
	if info.BaseDir != "/base/slug/2" {
		t.Errorf("empty filePath should fall back, got %q", info.BaseDir)
	}
}

// --- skillInfoRow + customSkillExportRow ---

func TestSkillInfoRow_ToSkillInfo(t *testing.T) {
	desc := "d"
	r := skillInfoRow{
		ID: uuid.New(), Name: "n", Slug: "s", Desc: &desc,
		Visibility: "public",
		Tags:       pq.StringArray{"tag1", "tag2"},
		Version:    1, IsSystem: true, Status: "active", Enabled: true,
		DepsRaw: []byte(`{"missing":["m1"]}`),
	}
	info := r.toSkillInfo("/base")
	if info.Visibility != "public" || info.Status != "active" || !info.Enabled || !info.IsSystem {
		t.Errorf("%+v", info)
	}
	if len(info.Tags) != 2 || info.Tags[0] != "tag1" {
		t.Errorf("tags: %v", info.Tags)
	}
	if len(info.MissingDeps) != 1 || info.MissingDeps[0] != "m1" {
		t.Errorf("missingDeps: %v", info.MissingDeps)
	}
}

func TestSkillInfoRowWithFrontmatter_ToSkillInfo(t *testing.T) {
	desc := "d"
	r := skillInfoRowWithFrontmatter{
		skillInfoRow: skillInfoRow{
			ID: uuid.New(), Name: "n", Slug: "s", Desc: &desc,
			Visibility: "private", Version: 1,
		},
		FmRaw: []byte(`{"author":"Alice"}`),
	}
	info := r.toSkillInfo("/base")
	if info.Author != "Alice" {
		t.Errorf("Author = %q", info.Author)
	}
}

func TestCustomSkillExportRow_ToCustomSkillExport(t *testing.T) {
	id := uuid.New()
	desc := "a skill"
	fp := "/skills/test"
	r := customSkillExportRow{
		ID: id, Name: "n", Slug: "s", Description: &desc,
		Visibility: "public", Version: 2,
		FmRaw:    []byte(`{"author":"Alice"}`),
		Tags:     pq.StringArray{"a", "b"},
		DepsRaw:  []byte(`{"missing":[]}`),
		FilePath: &fp,
	}
	got := r.toCustomSkillExport()
	if got.ID != id.String() || got.Name != "n" {
		t.Errorf("%+v", got)
	}
	if got.Description == nil || *got.Description != "a skill" {
		t.Errorf("Description: %v", got.Description)
	}
	if len(got.Tags) != 2 {
		t.Errorf("tags: %v", got.Tags)
	}
	if string(got.Frontmatter) != `{"author":"Alice"}` {
		t.Errorf("Frontmatter: %s", got.Frontmatter)
	}
	if got.FilePath != "/skills/test" {
		t.Errorf("FilePath = %q", got.FilePath)
	}
}

func TestCustomSkillExportRow_EmptyOptionals(t *testing.T) {
	r := customSkillExportRow{ID: uuid.New(), Name: "n"}
	got := r.toCustomSkillExport()
	if got.Frontmatter != nil || got.Deps != nil {
		t.Errorf("empty raws should produce nil: %+v", got)
	}
	if got.FilePath != "" {
		t.Errorf("nil FilePath should empty, got %q", got.FilePath)
	}
}

// --- mergeEpisodicScores ---

func TestMergeEpisodicScores_WeightingAndMerge(t *testing.T) {
	fts := []episodicScored{
		{id: "a", sessionKey: "s-a", l0: "A", score: 1.0},
		{id: "b", sessionKey: "s-b", l0: "B", score: 0.5},
	}
	vec := []episodicScored{
		{id: "a", sessionKey: "s-a", l0: "A", score: 0.8},
		{id: "c", sessionKey: "s-c", l0: "C", score: 0.9},
	}
	merged := mergeEpisodicScores(fts, vec, 0.5, 0.5)
	if len(merged) != 3 {
		t.Fatalf("len = %d, want 3 (a+b+c unique)", len(merged))
	}
	byID := make(map[string]float64)
	for _, r := range merged {
		byID[r.id] = r.score
	}
	// a: in both → 1.0*0.5 + 0.8*0.5 = 0.5 + 0.4 = 0.9
	if got := byID["a"]; got < 0.89 || got > 0.91 {
		t.Errorf("a score = %v, want ~0.9", got)
	}
	// b: fts only → 0.5*0.5 = 0.25
	if got := byID["b"]; got < 0.24 || got > 0.26 {
		t.Errorf("b score = %v, want ~0.25", got)
	}
	// c: vec only → 0.9*0.5 = 0.45
	if got := byID["c"]; got < 0.44 || got > 0.46 {
		t.Errorf("c score = %v, want ~0.45", got)
	}
}

func TestMergeEpisodicScores_EmptyInputs(t *testing.T) {
	if got := mergeEpisodicScores(nil, nil, 0.5, 0.5); got != nil {
		t.Errorf("both empty should produce nil, got %v", got)
	}
}

// --- hybridMerge (memory_search.go) ---

func TestHybridMerge_PersonalBoostOverGlobal(t *testing.T) {
	uid := "u1"
	fts := []scoredChunk{
		{Path: "p", StartLine: 1, EndLine: 5, Text: "global text", Score: 1.0},
		{Path: "p", StartLine: 1, EndLine: 5, Text: "personal text", UserID: &uid, Score: 1.0},
	}
	got := hybridMerge(fts, nil, 1.0, 1.0, "u1")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (dedup on path+startLine)", len(got))
	}
	if got[0].Scope != "personal" {
		t.Errorf("scope = %q, want personal", got[0].Scope)
	}
	if got[0].Snippet != "personal text" {
		t.Errorf("snippet = %q, want personal text (user copy wins)", got[0].Snippet)
	}
}

func TestHybridMerge_SortsByScoreDescending(t *testing.T) {
	fts := []scoredChunk{
		{Path: "low", StartLine: 1, EndLine: 2, Text: "l", Score: 0.1},
		{Path: "high", StartLine: 1, EndLine: 2, Text: "h", Score: 0.9},
	}
	got := hybridMerge(fts, nil, 1.0, 1.0, "")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Path != "high" || got[1].Path != "low" {
		t.Errorf("sort order: %+v", got)
	}
}

func TestHybridMerge_FTSPlusVectorAddScores(t *testing.T) {
	fts := []scoredChunk{{Path: "p", StartLine: 1, EndLine: 5, Text: "t", Score: 0.4}}
	vec := []scoredChunk{{Path: "p", StartLine: 1, EndLine: 5, Text: "t", Score: 0.6}}
	got := hybridMerge(fts, vec, 1.0, 1.0, "")
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	// 0.4*1.0 + 0.6*1.0 = 1.0
	if got[0].Score < 0.99 || got[0].Score > 1.01 {
		t.Errorf("score = %v, want 1.0", got[0].Score)
	}
}

func TestHybridMerge_EmptyInputs(t *testing.T) {
	got := hybridMerge(nil, nil, 1.0, 1.0, "")
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
