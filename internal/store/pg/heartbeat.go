package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGHeartbeatStore implements store.HeartbeatStore backed by Postgres.
type PGHeartbeatStore struct {
	db      *sql.DB
	mu      sync.Mutex
	onEvent func(store.HeartbeatEvent)

	// Due cache: reduces ListDue polling.
	dueCache    []store.AgentHeartbeat
	cacheLoaded bool
	cacheTime   time.Time
	cacheTTL    time.Duration
}

const defaultHeartbeatCacheTTL = 30 * time.Second

func NewPGHeartbeatStore(db *sql.DB) *PGHeartbeatStore {
	return &PGHeartbeatStore{db: db, cacheTTL: defaultHeartbeatCacheTTL}
}

func (s *PGHeartbeatStore) SetOnEvent(fn func(store.HeartbeatEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = fn
}

func (s *PGHeartbeatStore) emitEvent(event store.HeartbeatEvent) {
	s.mu.Lock()
	fn := s.onEvent
	s.mu.Unlock()
	if fn != nil {
		fn(event)
	}
}

// InvalidateCache clears the due cache (called on config changes).
func (s *PGHeartbeatStore) InvalidateCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cacheLoaded = false
}

func (s *PGHeartbeatStore) Get(ctx context.Context, agentID uuid.UUID) (*store.AgentHeartbeat, error) {
	var hb store.AgentHeartbeat
	var metadata []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, enabled, interval_sec, prompt, provider_id, model,
		        isolated_session, light_context, ack_max_chars, max_retries,
		        active_hours_start, active_hours_end, timezone,
		        channel, chat_id,
		        next_run_at, last_run_at, last_status, last_error,
		        run_count, suppress_count, metadata, created_at, updated_at
		 FROM agent_heartbeats WHERE agent_id = $1`, agentID,
	).Scan(
		&hb.ID, &hb.AgentID, &hb.Enabled, &hb.IntervalSec, &hb.Prompt, &hb.ProviderID, &hb.Model,
		&hb.IsolatedSession, &hb.LightContext, &hb.AckMaxChars, &hb.MaxRetries,
		&hb.ActiveHoursStart, &hb.ActiveHoursEnd, &hb.Timezone,
		&hb.Channel, &hb.ChatID,
		&hb.NextRunAt, &hb.LastRunAt, &hb.LastStatus, &hb.LastError,
		&hb.RunCount, &hb.SuppressCount, &metadata, &hb.CreatedAt, &hb.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	hb.Metadata = metadata
	return &hb, nil
}

func (s *PGHeartbeatStore) Upsert(ctx context.Context, hb *store.AgentHeartbeat) error {
	meta := hb.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	now := time.Now()
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO agent_heartbeats (agent_id, enabled, interval_sec, prompt, provider_id, model,
		        isolated_session, light_context, ack_max_chars, max_retries,
		        active_hours_start, active_hours_end, timezone,
		        channel, chat_id, next_run_at, metadata, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$18)
		 ON CONFLICT (agent_id) DO UPDATE SET
		        enabled = EXCLUDED.enabled,
		        interval_sec = EXCLUDED.interval_sec,
		        prompt = EXCLUDED.prompt,
		        provider_id = EXCLUDED.provider_id,
		        model = EXCLUDED.model,
		        isolated_session = EXCLUDED.isolated_session,
		        light_context = EXCLUDED.light_context,
		        ack_max_chars = EXCLUDED.ack_max_chars,
		        max_retries = EXCLUDED.max_retries,
		        active_hours_start = EXCLUDED.active_hours_start,
		        active_hours_end = EXCLUDED.active_hours_end,
		        timezone = EXCLUDED.timezone,
		        channel = EXCLUDED.channel,
		        chat_id = EXCLUDED.chat_id,
		        next_run_at = EXCLUDED.next_run_at,
		        metadata = EXCLUDED.metadata,
		        updated_at = EXCLUDED.updated_at
		 RETURNING id, created_at, updated_at`,
		hb.AgentID, hb.Enabled, hb.IntervalSec, hb.Prompt, hb.ProviderID, hb.Model,
		hb.IsolatedSession, hb.LightContext, hb.AckMaxChars, hb.MaxRetries,
		hb.ActiveHoursStart, hb.ActiveHoursEnd, hb.Timezone,
		hb.Channel, hb.ChatID, hb.NextRunAt, meta, now,
	).Scan(&hb.ID, &hb.CreatedAt, &hb.UpdatedAt)
	if err != nil {
		return err
	}
	s.InvalidateCache()
	return nil
}

func (s *PGHeartbeatStore) ListDue(ctx context.Context, now time.Time) ([]store.AgentHeartbeat, error) {
	s.mu.Lock()
	if s.cacheLoaded && time.Since(s.cacheTime) < s.cacheTTL {
		cached := s.dueCache
		s.mu.Unlock()
		// Filter in-memory for due items.
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
		`SELECT id, agent_id, enabled, interval_sec, prompt, provider_id, model,
		        isolated_session, light_context, ack_max_chars, max_retries,
		        active_hours_start, active_hours_end, timezone,
		        channel, chat_id,
		        next_run_at, last_run_at, last_status, last_error,
		        run_count, suppress_count, metadata, created_at, updated_at
		 FROM agent_heartbeats
		 WHERE enabled = true AND next_run_at IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []store.AgentHeartbeat
	for rows.Next() {
		var hb store.AgentHeartbeat
		var metadata []byte
		if err := rows.Scan(
			&hb.ID, &hb.AgentID, &hb.Enabled, &hb.IntervalSec, &hb.Prompt, &hb.ProviderID, &hb.Model,
			&hb.IsolatedSession, &hb.LightContext, &hb.AckMaxChars, &hb.MaxRetries,
			&hb.ActiveHoursStart, &hb.ActiveHoursEnd, &hb.Timezone,
			&hb.Channel, &hb.ChatID,
			&hb.NextRunAt, &hb.LastRunAt, &hb.LastStatus, &hb.LastError,
			&hb.RunCount, &hb.SuppressCount, &metadata, &hb.CreatedAt, &hb.UpdatedAt,
		); err != nil {
			return nil, err
		}
		hb.Metadata = metadata
		all = append(all, hb)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Update cache.
	s.mu.Lock()
	s.dueCache = all
	s.cacheLoaded = true
	s.cacheTime = time.Now()
	s.mu.Unlock()

	// Filter for due.
	var due []store.AgentHeartbeat
	for _, hb := range all {
		if hb.NextRunAt != nil && !hb.NextRunAt.After(now) {
			due = append(due, hb)
		}
	}
	return due, nil
}

func (s *PGHeartbeatStore) UpdateState(ctx context.Context, id uuid.UUID, state store.HeartbeatState) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_heartbeats SET
		        next_run_at = $2, last_run_at = $3, last_status = $4, last_error = $5,
		        run_count = $6, suppress_count = $7, updated_at = NOW()
		 WHERE id = $1`,
		id, state.NextRunAt, state.LastRunAt, state.LastStatus, state.LastError,
		state.RunCount, state.SuppressCount,
	)
	if err == nil {
		s.InvalidateCache() // ensure ListDue picks up new next_run_at immediately
	}
	return err
}

func (s *PGHeartbeatStore) Delete(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM agent_heartbeats WHERE agent_id = $1`, agentID)
	if err == nil {
		s.InvalidateCache()
	}
	return err
}

// InsertLog records a heartbeat run execution.
func (s *PGHeartbeatStore) InsertLog(ctx context.Context, log *store.HeartbeatRunLog) error {
	meta := log.Metadata
	if meta == nil {
		meta = json.RawMessage("{}")
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO heartbeat_run_logs (heartbeat_id, agent_id, status, summary, error,
		        duration_ms, input_tokens, output_tokens, skip_reason, metadata, ran_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		log.HeartbeatID, log.AgentID, log.Status, log.Summary, log.Error,
		log.DurationMS, log.InputTokens, log.OutputTokens, log.SkipReason, meta, log.RanAt,
	)
	return err
}

func (s *PGHeartbeatStore) ListLogs(ctx context.Context, agentID uuid.UUID, limit, offset int) ([]store.HeartbeatRunLog, int, error) {
	if limit <= 0 {
		limit = 20
	}

	var total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM heartbeat_run_logs WHERE agent_id = $1`, agentID,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, heartbeat_id, agent_id, status, summary, error,
		        duration_ms, input_tokens, output_tokens, skip_reason, metadata, ran_at, created_at
		 FROM heartbeat_run_logs WHERE agent_id = $1
		 ORDER BY ran_at DESC LIMIT $2 OFFSET $3`,
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
		if err := rows.Scan(
			&l.ID, &l.HeartbeatID, &l.AgentID, &l.Status, &l.Summary, &l.Error,
			&l.DurationMS, &l.InputTokens, &l.OutputTokens, &l.SkipReason, &metadata, &l.RanAt, &l.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		l.Metadata = metadata
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// ListDeliveryTargets returns known delivery targets (channel, chatID, title, kind) from channel_contacts.
// Queries contacts with contact_type IN ('group','topic') for the given tenant.
// For topic contacts, chatID is built as senderID + ":topic:" + threadID.
func (s *PGHeartbeatStore) ListDeliveryTargets(ctx context.Context, tenantID uuid.UUID) ([]store.DeliveryTarget, error) {
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
		 WHERE cc.tenant_id = $1
		   AND cc.contact_type IN ('group', 'topic')
		 ORDER BY cc.channel_instance, cc.display_name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []store.DeliveryTarget
	seen := make(map[string]bool)
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
