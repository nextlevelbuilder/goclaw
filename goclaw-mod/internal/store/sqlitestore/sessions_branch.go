//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"maps"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *SQLiteSessionStore) BranchSession(ctx context.Context, sourceKey string, opts store.SessionBranchOpts) (*store.SessionData, int, error) {
	if opts.NewKey == "" || opts.UpToIndex < 0 {
		return nil, 0, store.ErrInvalidSessionBranch
	}
	if s.Get(ctx, opts.NewKey) != nil {
		return nil, 0, store.ErrSessionAlreadyExists
	}

	source := s.Get(ctx, sourceKey)
	if source == nil {
		return nil, 0, store.ErrSessionNotFound
	}

	s.mu.RLock()
	sourceSnapshot := *source
	sourceMsgs := make([]providers.Message, len(source.Messages))
	copy(sourceMsgs, source.Messages)
	sourceSnapshot.Messages = sourceMsgs
	sourceMeta := make(map[string]string, len(source.Metadata))
	maps.Copy(sourceMeta, source.Metadata)
	sourceSnapshot.Metadata = sourceMeta
	s.mu.RUnlock()

	if opts.UpToIndex > len(sourceSnapshot.Messages) {
		return nil, 0, store.ErrInvalidSessionBranch
	}

	now := time.Now().UTC()
	copied := make([]providers.Message, opts.UpToIndex)
	copy(copied, sourceSnapshot.Messages[:opts.UpToIndex])
	meta := make(map[string]string, len(sourceSnapshot.Metadata)+len(opts.Metadata)+3)
	maps.Copy(meta, sourceSnapshot.Metadata)
	maps.Copy(meta, opts.Metadata)
	meta["branched_from"] = sourceKey
	meta["branched_from_index"] = strconv.Itoa(opts.UpToIndex)
	meta["branched_at"] = now.Format(time.RFC3339Nano)

	branch := &store.SessionData{
		Key:           opts.NewKey,
		Messages:      copied,
		Summary:       sourceSnapshot.Summary,
		Created:       now,
		Updated:       now,
		AgentUUID:     sourceSnapshot.AgentUUID,
		UserID:        sourceSnapshot.UserID,
		Model:         sourceSnapshot.Model,
		Provider:      sourceSnapshot.Provider,
		Channel:       "branch",
		Label:         opts.Label,
		Metadata:      meta,
		ContextWindow: sourceSnapshot.ContextWindow,
	}
	if sourceSnapshot.TeamID != nil {
		teamID := *sourceSnapshot.TeamID
		branch.TeamID = &teamID
	}

	msgsJSON, _ := json.Marshal(branch.Messages)
	metaJSON := []byte("{}")
	if len(branch.Metadata) > 0 {
		metaJSON, _ = json.Marshal(branch.Metadata)
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, session_key, messages, summary, model, provider, channel,
			input_tokens, output_tokens, compaction_count,
			memory_flush_compaction_count, memory_flush_at,
			label, spawned_by, spawn_depth, agent_id, user_id, metadata, updated_at, team_id, tenant_id, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(session_key, tenant_id) DO NOTHING`,
		uuid.Must(uuid.NewV7()), branch.Key, msgsJSON,
		nilStr(branch.Summary), nilStr(branch.Model), nilStr(branch.Provider), nilStr(branch.Channel),
		branch.InputTokens, branch.OutputTokens, branch.CompactionCount,
		branch.MemoryFlushCompactionCount, branch.MemoryFlushAt,
		nilStr(branch.Label), nilStr(branch.SpawnedBy), branch.SpawnDepth,
		nilSessionUUID(branch.AgentUUID), nilStr(branch.UserID), metaJSON, branch.Updated,
		branch.TeamID, tenantIDForInsert(ctx), branch.Created,
	)
	if err != nil {
		return nil, 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, 0, store.ErrSessionAlreadyExists
	}

	s.mu.Lock()
	s.cache[sessionCacheKey(ctx, opts.NewKey)] = branch
	s.mu.Unlock()
	return branch, len(copied), nil
}
