//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteHeartbeatStore implements store.HeartbeatStore backed by SQLite.
type SQLiteHeartbeatStore struct {
	db      *sql.DB
	mu      sync.Mutex
	onEvent func(store.HeartbeatEvent)

	dueCache    []store.AgentHeartbeat
	cacheLoaded bool
	cacheTime   time.Time
	cacheTTL    time.Duration
}

const defaultHeartbeatCacheTTL = 30 * time.Second

func NewSQLiteHeartbeatStore(db *sql.DB) *SQLiteHeartbeatStore {
	return &SQLiteHeartbeatStore{db: db, cacheTTL: defaultHeartbeatCacheTTL}
}

func (s *SQLiteHeartbeatStore) SetOnEvent(fn func(store.HeartbeatEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

func (s *SQLiteHeartbeatStore) emitEvent(event store.HeartbeatEvent) {
	s.mu.Lock()
	fn := s.onEvent
	s.mu.Unlock()
	if fn != nil {
		fn(event)
	}
}

func (s *SQLiteHeartbeatStore) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheLoaded = false
}

const heartbeatSelectCols = `id, agent_id, enabled, interval_sec, prompt, provider_id, model,
	isolated_session, light_context, ack_max_chars, max_retries,
	active_hours_start, active_hours_end, timezone,
	channel, chat_id,
	next_run_at, last_run_at, last_status, last_error,
	run_count, suppress_count, metadata, created_at, updated_at`

func scanHeartbeat(row interface {
	Scan(...any) error
}) (*store.AgentHeartbeat, error) {
	var hb store.AgentHeartbeat
	var metadata []byte
	var nextRunAt, lastRunAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()
	err := row.Scan(
		&hb.ID, &hb.AgentID, &hb.Enabled, &hb.IntervalSec, &hb.Prompt, &hb.ProviderID, &hb.Model,
		&hb.IsolatedSession, &hb.LightContext, &hb.AckMaxChars, &hb.MaxRetries,
		&hb.ActiveHoursStart, &hb.ActiveHoursEnd, &hb.Timezone,
		&hb.Channel, &hb.ChatID,
		&nextRunAt, &lastRunAt, &hb.LastStatus, &hb.LastError,
		&hb.RunCount, &hb.SuppressCount, &metadata, createdAt, updatedAt,
	)
	if err != nil {
		return nil, err
	}
	hb.CreatedAt = createdAt.Time
	hb.UpdatedAt = updatedAt.Time
	if nextRunAt.Valid {
		hb.NextRunAt = &nextRunAt.Time
	}
	if lastRunAt.Valid {
		hb.LastRunAt = &lastRunAt.Time
	}
	hb.Metadata = metadata
	return &hb, nil
}

func (s *SQLiteHeartbeatStore) Get(ctx context.Context, agentID uuid.UUID) (*store.AgentHeartbeat, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+heartbeatSelectCols+` FROM agent_heartbeats WHERE agent_id = ?`, agentID,
	)
	return scanHeartbeat(row)
}

func (s *SQLiteHeartbeatStore) Upsert(ctx context.Context, hb *store.AgentHeartbeat) error {
	meta := hb.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	createdAt, updatedAt := scanTimePair()
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO agent_heartbeats
		        (agent_id, enabled, interval_sec, prompt, provider_id, model,
		         isolated_session, light_context, ack_max_chars, max_retries,
		         active_hours_start, active_hours_end, timezone,
		         channel, chat_id, next_run_at, metadata, created_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT (agent_id) DO UPDATE SET
		        enabled = excluded.enabled,
		        interval_sec = excluded.interval_sec,
		        prompt = excluded.prompt,
		        provider_id = excluded.provider_id,
		        model = excluded.model,
		        isolated_session = excluded.isolated_session,
		        light_context = excluded.light_context,
		        ack_max_chars = excluded.ack_max_chars,
		        max_retries = excluded.max_retries,
		        active_hours_start = excluded.active_hours_start,
		        active_hours_end = excluded.active_hours_end,
		        timezone = excluded.timezone,
		        channel = excluded.channel,
		        chat_id = excluded.chat_id,
		        next_run_at = excluded.next_run_at,
		        metadata = excluded.metadata,
		        updated_at = excluded.updated_at
		 RETURNING id, created_at, updated_at`,
		hb.AgentID, hb.Enabled, hb.IntervalSec, hb.Prompt, hb.ProviderID, hb.Model,
		hb.IsolatedSession, hb.LightContext, hb.AckMaxChars, hb.MaxRetries,
		hb.ActiveHoursStart, hb.ActiveHoursEnd, hb.Timezone,
		hb.Channel, hb.ChatID, hb.NextRunAt, meta, now, now,
	).Scan(&hb.ID, createdAt, updatedAt)
	if err != nil {
		return err
	}
	hb.CreatedAt = createdAt.Time
	hb.UpdatedAt = updatedAt.Time
	s.InvalidateCache()
	return nil
}

func (s *SQLiteHeartbeatStore) ListDue(ctx context.Context, now time.Time) ([]store.AgentHeartbeat, error) {
	s.mu.Lock()
	if s.cacheLoaded && time.Since(s.cacheTime) < s.cacheTTL {
		cached := s.dueCache
		s.mu.Unlock()
		var due []store.AgentHeartbeat
		for _, hb := range cached {
			if hb.NextRunAt != nil && !hb.NextRunAt.After(now) {
				due = append(due, hb)
			}
		}
		return due, nil
	}
	s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+heartbeatSelectCols+`
		 FROM agent_heartbeats
		 WHERE enabled = 1 AND next_run_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []store.AgentHeartbeat
	for rows.Next() {
		hb, err := scanHeartbeat(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, *hb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.dueCache = all
	s.cacheLoaded = true
	s.cacheTime = time.Now()
	s.mu.Unlock()

	var due []store.AgentHeartbeat
	for _, hb := range all {
		if hb.NextRunAt != nil && !hb.NextRunAt.After(now) {
			due = append(due, hb)
		}
	}
	return due, nil
}

func (s *SQLiteHeartbeatStore) UpdateState(ctx context.Context, id uuid.UUID, state store.HeartbeatState) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_heartbeats SET
		        next_run_at = ?, last_run_at = ?, last_status = ?, last_error = ?,
		        run_count = ?, suppress_count = ?, updated_at = ?
		 WHERE id = ?`,
		state.NextRunAt, state.LastRunAt, state.LastStatus, state.LastError,
		state.RunCount, state.SuppressCount, time.Now().UTC(), id,
	)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *SQLiteHeartbeatStore) Delete(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_heartbeats WHERE agent_id = ?`, agentID)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

func (s *SQLiteHeartbeatStore) InsertLog(ctx context.Context, log *store.HeartbeatRunLog) error {
	meta := log.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO heartbeat_run_logs
		        (heartbeat_id, agent_id, status, summary, error,
		         duration_ms, input_tokens, output_tokens, skip_reason, metadata, ran_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		log.HeartbeatID, log.AgentID, log.Status, log.Summary, log.Error,
		log.DurationMS, log.InputTokens, log.OutputTokens, log.SkipReason, meta, log.RanAt,
	)
	return err
}

func (s *SQLiteHeartbeatStore) ListLogs(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]store.HeartbeatRunLog, int, error) {
	if limit <= 0 {
		limit = 20
	}

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM heartbeat_run_logs WHERE agent_id = ?`, agentID,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, heartbeat_id, agent_id, status, summary, error,
		        duration_ms, input_tokens, output_tokens, skip_reason, metadata, ran_at, created_at
		 FROM heartbeat_run_logs WHERE agent_id = ?
		 ORDER BY ran_at DESC LIMIT ? OFFSET ?`,
		agentID, limit, offset,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []store.HeartbeatRunLog
	for rows.Next() {
		var l store.HeartbeatRunLog
		var metadata []byte
		var ranAt, createdAt sqliteTime
		if err := rows.Scan(
			&l.ID, &l.HeartbeatID, &l.AgentID, &l.Status, &l.Summary, &l.Error,
			&l.DurationMS, &l.InputTokens, &l.OutputTokens, &l.SkipReason, &metadata, &ranAt, &createdAt,
		); err != nil {
			return nil, 0, err
		}
		l.RanAt = ranAt.Time
		l.CreatedAt = createdAt.Time
		l.Metadata = metadata
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// ListDeliveryTargets returns known delivery targets (channel, chatID, title, kind) from channel_contacts.
// Queries contacts with contact_type IN ('group','topic','user') for the given tenant.
// For topic contacts, chatID is built as senderID + ":topic:" + threadID.
func (s *SQLiteHeartbeatStore) ListDeliveryTargets(ctx context.Context, tenantID uuid.UUID) ([]store.DeliveryTarget, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT cc.sender_id,
		        cc.thread_id,
		        cc.thread_type,
		        cc.channel_instance AS channel,
		        COALESCE(cc.display_name, cc.sender_id) AS title,
		        CASE WHEN cc.contact_type = 'topic' THEN 'topic'
		             WHEN cc.peer_kind = 'group' THEN 'group'
		             ELSE 'dm' END AS kind
		 FROM channel_contacts cc
		 WHERE cc.tenant_id = ?
		   AND cc.contact_type IN ('group', 'topic', 'user')
		 ORDER BY cc.channel_instance, cc.display_name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var targets []store.DeliveryTarget
	for rows.Next() {
		var senderID, title, kind string
		var threadID, threadType, channel *string
		if err := rows.Scan(&senderID, &threadID, &threadType, &channel, &title, &kind); err != nil {
			return nil, err
		}
		if channel == nil {
			continue
		}
		chatID := senderID
		displayTitle := title
		if threadID != nil && *threadID != "" {
			chatID = senderID + ":topic:" + *threadID
			displayTitle = title + " > topic:" + *threadID
		}
		key := *channel + ":" + chatID
		if !seen[key] {
			seen[key] = true
			targets = append(targets, store.DeliveryTarget{
				Channel: *channel,
				ChatID:  chatID,
				Title:   displayTitle,
				Kind:    kind,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return targets, nil
}

