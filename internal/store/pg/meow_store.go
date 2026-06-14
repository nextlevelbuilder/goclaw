package pg

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/base"
)

// PGMeowStore implements store.MeowStore backed by Postgres.
// PostgreSQL-only: there is no SQLite counterpart because the channel system
// is gated off in Lite/desktop. Every query is tenant-scoped.
type PGMeowStore struct {
	db *sql.DB
}

// NewPGMeowStore constructs the Meow store.
func NewPGMeowStore(db *sql.DB) *PGMeowStore {
	return &PGMeowStore{db: db}
}

const mpChannelCols = `id, tenant_id, handle, chat_id, brand_key, tz, has_mascot, launched,
	enabled, smm_tier, smm_caps, button_set, subs_baseline, created_at, updated_at`

// --- Channels ---

// UpsertChannel inserts a channel from the bundled registry or, on
// (tenant_id, handle) conflict, refreshes only the registry-authoritative
// fields (brand_key, tz, has_mascot, launched, button_set, subs_baseline).
// Runtime-managed fields — chat_id (resolved at runtime), enabled (operator
// toggle), smm_tier, smm_caps (operator config) — are PRESERVED on conflict so
// that re-seeding on every startup never erases live state. Those fields are
// mutated through dedicated methods, not this registry upsert.
func (s *PGMeowStore) UpsertChannel(ctx context.Context, ch *store.MpChannel) error {
	if err := base.RequireTenantID(ch.TenantID); err != nil {
		return err
	}
	tz := ch.TZ
	if tz == "" {
		tz = "Asia/Seoul"
	}
	return s.db.QueryRowContext(ctx,
		`INSERT INTO mp_channels
		   (tenant_id, handle, chat_id, brand_key, tz, has_mascot, launched, enabled,
		    smm_tier, smm_caps, button_set, subs_baseline)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		 ON CONFLICT (tenant_id, handle) DO UPDATE SET
		    brand_key = EXCLUDED.brand_key,
		    tz = EXCLUDED.tz,
		    has_mascot = EXCLUDED.has_mascot,
		    launched = EXCLUDED.launched,
		    button_set = EXCLUDED.button_set,
		    subs_baseline = EXCLUDED.subs_baseline,
		    updated_at = NOW()
		    -- chat_id, enabled, smm_tier, smm_caps intentionally NOT overwritten
		 RETURNING id, created_at, updated_at`,
		ch.TenantID, ch.Handle, ch.ChatID, ch.BrandKey, tz, ch.HasMascot, ch.Launched, ch.Enabled,
		ch.SMMTier, base.JsonOrEmpty(ch.SMMCaps), base.JsonOrEmptyArray(ch.ButtonSet), ch.SubsBaseline,
	).Scan(&ch.ID, &ch.CreatedAt, &ch.UpdatedAt)
}

func (s *PGMeowStore) GetChannel(ctx context.Context, tenantID, id uuid.UUID) (*store.MpChannel, error) {
	var ch store.MpChannel
	err := pkgSqlxDB.GetContext(ctx, &ch,
		`SELECT `+mpChannelCols+` FROM mp_channels WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrMeowChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *PGMeowStore) GetChannelByHandle(ctx context.Context, tenantID uuid.UUID, handle string) (*store.MpChannel, error) {
	var ch store.MpChannel
	err := pkgSqlxDB.GetContext(ctx, &ch,
		`SELECT `+mpChannelCols+` FROM mp_channels WHERE tenant_id = $1 AND handle = $2`, tenantID, handle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrMeowChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func (s *PGMeowStore) ListChannels(ctx context.Context, tenantID uuid.UUID) ([]store.MpChannel, error) {
	out := []store.MpChannel{}
	err := pkgSqlxDB.SelectContext(ctx, &out,
		`SELECT `+mpChannelCols+` FROM mp_channels WHERE tenant_id = $1 ORDER BY handle`, tenantID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// --- Posts ---

const mpPostCols = `id, tenant_id, channel_id, scheduled_date, scheduled_at_utc, status,
	ko_text, en_text, image_path, buttons, tg_message_id, tg_link,
	approved_by, approved_at, created_at, updated_at`

func (s *PGMeowStore) CreatePost(ctx context.Context, p *store.MpContentPost) error {
	if err := base.RequireTenantID(p.TenantID); err != nil {
		return err
	}
	status := p.Status
	if status == "" {
		status = store.MpPostDraft
	}
	// INSERT...SELECT gated on the channel belonging to the same tenant: a
	// foreign channel_id yields zero rows (ErrNoRows) instead of a post that
	// crosses tenants. The composite FK is the DB-level backstop.
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO mp_content_posts
		   (tenant_id, channel_id, scheduled_date, scheduled_at_utc, status,
		    ko_text, en_text, image_path, buttons, tg_message_id, tg_link, approved_by, approved_at)
		 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13
		 WHERE EXISTS (SELECT 1 FROM mp_channels WHERE id = $2 AND tenant_id = $1)
		 RETURNING id, created_at, updated_at`,
		p.TenantID, p.ChannelID, p.ScheduledDate, p.ScheduledAtUTC, status,
		p.KoText, p.EnText, p.ImagePath, base.JsonOrEmptyArray(p.Buttons),
		p.TgMessageID, p.TgLink, p.ApprovedBy, p.ApprovedAt,
	).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrMeowChannelNotFound
	}
	return err
}

func (s *PGMeowStore) GetPost(ctx context.Context, tenantID, id uuid.UUID) (*store.MpContentPost, error) {
	var p store.MpContentPost
	err := pkgSqlxDB.GetContext(ctx, &p,
		`SELECT `+mpPostCols+` FROM mp_content_posts WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrMeowPostNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PGMeowStore) ListPostsByChannel(ctx context.Context, tenantID, channelID uuid.UUID) ([]store.MpContentPost, error) {
	out := []store.MpContentPost{}
	err := pkgSqlxDB.SelectContext(ctx, &out,
		`SELECT `+mpPostCols+` FROM mp_content_posts
		 WHERE tenant_id = $1 AND channel_id = $2 ORDER BY scheduled_date`, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// UpdatePostStatus transitions a post and records publish results. Transitions
// into 'publishing'/'published' are subject to the partial unique index
// (channel_id, scheduled_date) — a second concurrent publish for the same
// channel-day surfaces a unique-violation error from the driver.
func (s *PGMeowStore) UpdatePostStatus(ctx context.Context, tenantID, id uuid.UUID, status string, tgMessageID *int64, tgLink string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE mp_content_posts
		   SET status = $3, tg_message_id = $4, tg_link = $5, updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, status, tgMessageID, tgLink)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrMeowPostNotFound
	}
	return nil
}

// ClaimPostForPublish atomically claims one eligible post for a channel-day and
// flips it to 'publishing'. The inner SELECT skips the whole day if any post is
// already publishing/published (exactly-once: re-runs claim nothing), locks the
// chosen row (FOR UPDATE SKIP LOCKED) to serialize concurrent publishers, and
// the partial unique index is the final backstop.
func (s *PGMeowStore) ClaimPostForPublish(ctx context.Context, tenantID, channelID uuid.UUID, date time.Time, force bool) (*store.MpContentPost, error) {
	var p store.MpContentPost
	err := pkgSqlxDB.GetContext(ctx, &p,
		`UPDATE mp_content_posts SET status = 'publishing', updated_at = NOW()
		 WHERE id = (
		   SELECT id FROM mp_content_posts
		   WHERE tenant_id = $1 AND channel_id = $2 AND scheduled_date = $3::date
		     AND (status = 'approved' OR ($4::bool AND status = 'draft'))
		     AND NOT EXISTS (
		       SELECT 1 FROM mp_content_posts
		       WHERE tenant_id = $1 AND channel_id = $2 AND scheduled_date = $3::date
		         AND status IN ('publishing','published')
		     )
		   ORDER BY created_at
		   LIMIT 1
		   FOR UPDATE SKIP LOCKED
		 )
		 RETURNING `+mpPostCols,
		tenantID, channelID, date, force)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrMeowNoClaimablePost
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// --- Metrics ---

const mpMetricCols = `id, tenant_id, channel_id, date, subscriber_count, delta, stale, alerted_for_date, created_at`

func (s *PGMeowStore) UpsertMetric(ctx context.Context, m *store.MpChannelMetric) error {
	if err := base.RequireTenantID(m.TenantID); err != nil {
		return err
	}
	// INSERT...SELECT gated on the channel belonging to the same tenant.
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO mp_channel_metrics
		   (tenant_id, channel_id, date, subscriber_count, delta, stale, alerted_for_date)
		 SELECT $1,$2,$3,$4,$5,$6,$7
		 WHERE EXISTS (SELECT 1 FROM mp_channels WHERE id = $2 AND tenant_id = $1)
		 ON CONFLICT (channel_id, date) DO UPDATE SET
		    subscriber_count = EXCLUDED.subscriber_count,
		    delta = EXCLUDED.delta,
		    stale = EXCLUDED.stale,
		    alerted_for_date = EXCLUDED.alerted_for_date
		 RETURNING id, created_at`,
		m.TenantID, m.ChannelID, m.Date, m.SubscriberCount, m.Delta, m.Stale, m.AlertedForDate,
	).Scan(&m.ID, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrMeowChannelNotFound
	}
	return err
}

func (s *PGMeowStore) GetMetric(ctx context.Context, tenantID, channelID uuid.UUID, date time.Time) (*store.MpChannelMetric, error) {
	var m store.MpChannelMetric
	err := pkgSqlxDB.GetContext(ctx, &m,
		`SELECT `+mpMetricCols+` FROM mp_channel_metrics
		 WHERE tenant_id = $1 AND channel_id = $2 AND date = $3::date`, tenantID, channelID, date)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// --- smm orders ---

const mpOrderCols = `id, tenant_id, post_id, service_id, qty, smm_order_id, status, cost, created_at, updated_at`

func (s *PGMeowStore) CreateOrder(ctx context.Context, o *store.MpSmmOrder) error {
	if err := base.RequireTenantID(o.TenantID); err != nil {
		return err
	}
	status := o.Status
	if status == "" {
		status = store.MpOrderPending
	}
	// INSERT...SELECT gated on the post belonging to the same tenant. A
	// duplicate (post_id, service_id) still surfaces the unique violation
	// (post exists, so the SELECT yields a row and the insert is attempted).
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO mp_smm_orders
		   (tenant_id, post_id, service_id, qty, smm_order_id, status, cost)
		 SELECT $1,$2,$3,$4,$5,$6,$7
		 WHERE EXISTS (SELECT 1 FROM mp_content_posts WHERE id = $2 AND tenant_id = $1)
		 RETURNING id, created_at, updated_at`,
		o.TenantID, o.PostID, o.ServiceID, o.Qty, o.SmmOrderID, status, o.Cost,
	).Scan(&o.ID, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrMeowPostNotFound
	}
	return err
}

func (s *PGMeowStore) GetOrderByPostService(ctx context.Context, tenantID, postID uuid.UUID, serviceID string) (*store.MpSmmOrder, error) {
	var o store.MpSmmOrder
	err := pkgSqlxDB.GetContext(ctx, &o,
		`SELECT `+mpOrderCols+` FROM mp_smm_orders
		 WHERE tenant_id = $1 AND post_id = $2 AND service_id = $3`, tenantID, postID, serviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &o, nil
}

// --- Report ledger ---

func (s *PGMeowStore) RecordReportSent(ctx context.Context, tenantID uuid.UUID, periodKind, periodKey string) error {
	if err := base.RequireTenantID(tenantID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO mp_reports_sent (tenant_id, period_kind, period_key)
		 VALUES ($1,$2,$3)
		 ON CONFLICT (tenant_id, period_kind, period_key) DO NOTHING`,
		tenantID, periodKind, periodKey)
	return err
}

func (s *PGMeowStore) ReportAlreadySent(ctx context.Context, tenantID uuid.UUID, periodKind, periodKey string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM mp_reports_sent
		   WHERE tenant_id = $1 AND period_kind = $2 AND period_key = $3)`,
		tenantID, periodKind, periodKey).Scan(&exists)
	return exists, err
}

// compile-time interface check
var _ store.MeowStore = (*PGMeowStore)(nil)
