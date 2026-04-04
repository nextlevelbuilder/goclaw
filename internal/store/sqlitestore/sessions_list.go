//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// buildSessionFilter builds a dynamic WHERE clause from SessionListOpts using ? placeholders.
func buildSessionFilter(opts store.SessionListOpts, tableAlias string) (string, []any) {
	prefix := ""
	if tableAlias != "" {
		prefix = tableAlias + "."
	}
	var conditions []string
	var args []any

	if opts.AgentID != "" {
		conditions = append(conditions, prefix+"session_key LIKE ?")
		args = append(args, "agent:"+opts.AgentID+":%")
	}
	if opts.Channel != "" {
		conditions = append(conditions, prefix+"session_key LIKE ?")
		args = append(args, "agent:%:"+opts.Channel+":%")
	}
	if opts.UserID != "" {
		conditions = append(conditions, prefix+"user_id = ?")
		args = append(args, opts.UserID)
	}
	if opts.TenantID != uuid.Nil {
		conditions = append(conditions, prefix+"tenant_id = ?")
		args = append(args, opts.TenantID)
	}

	if len(conditions) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conditions, " AND "), args
}

func (s *SQLiteSessionStore) List(ctx context.Context, agentID string) []store.SessionInfo {
	var conditions []string
	var args []any

	if agentID != "" {
		conditions = append(conditions, "session_key LIKE ?")
		args = append(args, "agent:"+agentID+":%")
	}
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid != uuid.Nil {
			conditions = append(conditions, "tenant_id = ?")
			args = append(args, tid)
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT session_key, messages, created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}') FROM sessions"+where+" ORDER BY updated_at DESC",
		args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []store.SessionInfo
	for rows.Next() {
		var key string
		var msgsJSON []byte
		stCreated, stUpdated := scanTimePair()
		var label, channel, userID *string
		var metaJSON []byte
		if err := rows.Scan(&key, &msgsJSON, stCreated, stUpdated, &label, &channel, &userID, &metaJSON); err != nil {
			continue
		}
		var msgs []providers.Message
		json.Unmarshal(msgsJSON, &msgs)
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfo{
			Key:          key,
			MessageCount: len(msgs),
			Created:      stCreated.Time,
			Updated:      stUpdated.Time,
			Label:        derefStr(label),
			Channel:      derefStr(channel),
			UserID:       derefStr(userID),
			Metadata:     meta,
		})
	}
	return result
}

func (s *SQLiteSessionStore) ListPaged(ctx context.Context, opts store.SessionListOpts) store.SessionListResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(opts.Offset, 0)

	where, whereArgs := buildSessionFilter(opts, "")

	var total int
	countQ := "SELECT COUNT(*) FROM sessions" + where
	if err := s.db.QueryRowContext(ctx, countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: 0}
	}

	// Use json_array_length (SQLite built-in) instead of jsonb_array_length.
	selectQ := fmt.Sprintf(`SELECT session_key, json_array_length(messages), created_at, updated_at, label, channel, user_id, COALESCE(metadata, '{}')
		FROM sessions%s ORDER BY updated_at DESC LIMIT ? OFFSET ?`, where)
	selectArgs := append(append([]any{}, whereArgs...), limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQ, selectArgs...)
	if err != nil {
		return store.SessionListResult{Sessions: []store.SessionInfo{}, Total: total}
	}
	defer rows.Close()

	var result []store.SessionInfo
	for rows.Next() {
		var key string
		var msgCount int
		stCreated, stUpdated := scanTimePair()
		var label, channel, userID *string
		var metaJSON []byte
		if err := rows.Scan(&key, &msgCount, stCreated, stUpdated, &label, &channel, &userID, &metaJSON); err != nil {
			continue
		}
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfo{
			Key:          key,
			MessageCount: msgCount,
			Created:      stCreated.Time,
			Updated:      stUpdated.Time,
			Label:        derefStr(label),
			Channel:      derefStr(channel),
			UserID:       derefStr(userID),
			Metadata:     meta,
		})
	}
	if result == nil {
		result = []store.SessionInfo{}
	}
	return store.SessionListResult{Sessions: result, Total: total}
}

// ListPagedRich returns enriched session info for API responses (includes model, tokens, agent name).
func (s *SQLiteSessionStore) ListPagedRich(ctx context.Context, opts store.SessionListOpts) store.SessionListRichResult {
	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(opts.Offset, 0)

	where, whereArgs := buildSessionFilter(opts, "s")

	var total int
	countQ := "SELECT COUNT(*) FROM sessions s" + where
	if err := s.db.QueryRowContext(ctx, countQ, whereArgs...).Scan(&total); err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: 0}
	}

	// Use json_array_length and length() instead of PG-specific functions.
	const richCols = `s.session_key, json_array_length(s.messages), s.created_at, s.updated_at,
		s.label, s.channel, s.user_id, COALESCE(s.metadata, '{}'),
		s.model, s.provider, s.input_tokens, s.output_tokens,
		COALESCE(a.display_name, ''),
		length(s.messages) / 4 + 12000,
		COALESCE(a.context_window, 200000),
		s.compaction_count`

	selectQ := fmt.Sprintf(`SELECT %s
		FROM sessions s LEFT JOIN agents a ON s.agent_id = a.id
		%s ORDER BY s.updated_at DESC LIMIT ? OFFSET ?`, richCols, where)
	selectArgs := append(append([]any{}, whereArgs...), limit, offset)

	rows, err := s.db.QueryContext(ctx, selectQ, selectArgs...)
	if err != nil {
		return store.SessionListRichResult{Sessions: []store.SessionInfoRich{}, Total: total}
	}
	defer rows.Close()

	var result []store.SessionInfoRich
	for rows.Next() {
		var key string
		var msgCount int
		stCreated, stUpdated := scanTimePair()
		var label, channel, userID *string
		var metaJSON []byte
		var model, provider *string
		var inputTokens, outputTokens int64
		var agentName string
		var estimatedTokens, contextWindow, compactionCount int
		if err := rows.Scan(&key, &msgCount, stCreated, stUpdated, &label, &channel, &userID, &metaJSON,
			&model, &provider, &inputTokens, &outputTokens, &agentName,
			&estimatedTokens, &contextWindow, &compactionCount); err != nil {
			continue
		}
		var meta map[string]string
		if len(metaJSON) > 0 {
			json.Unmarshal(metaJSON, &meta)
		}
		result = append(result, store.SessionInfoRich{
			SessionInfo: store.SessionInfo{
				Key:          key,
				MessageCount: msgCount,
				Created:      stCreated.Time,
				Updated:      stUpdated.Time,
				Label:        derefStr(label),
				Channel:      derefStr(channel),
				UserID:       derefStr(userID),
				Metadata:     meta,
			},
			Model:           derefStr(model),
			Provider:        derefStr(provider),
			InputTokens:     inputTokens,
			OutputTokens:    outputTokens,
			AgentName:       agentName,
			EstimatedTokens: estimatedTokens,
			ContextWindow:   contextWindow,
			CompactionCount: compactionCount,
		})
	}
	if result == nil {
		result = []store.SessionInfoRich{}
	}
	return store.SessionListRichResult{Sessions: result, Total: total}
}
