package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrMeowChannelNotFound is returned when a channel id/handle does not
	// resolve within the caller's tenant scope.
	ErrMeowChannelNotFound = errors.New("meow channel not found")
	// ErrMeowPostNotFound is returned when a post id does not resolve within
	// the caller's tenant scope.
	ErrMeowPostNotFound = errors.New("meow post not found")
	// ErrMeowNoClaimablePost is returned by ClaimPostForPublish when there is
	// no eligible post to publish for a channel-day (none approved, or one is
	// already publishing/published — a no-op, not an error condition).
	ErrMeowNoClaimablePost = errors.New("meow: no claimable post for channel-day")
)

// MpChannel is a managed Telegram channel in the Meow autopilot registry.
type MpChannel struct {
	ID           uuid.UUID       `json:"id" db:"id"`
	TenantID     uuid.UUID       `json:"tenantId" db:"tenant_id"`
	Handle       string          `json:"handle" db:"handle"`
	ChatID       *string         `json:"chatId,omitempty" db:"chat_id"`
	BrandKey     string          `json:"brandKey" db:"brand_key"`
	TZ           string          `json:"tz" db:"tz"`
	HasMascot    bool            `json:"hasMascot" db:"has_mascot"`
	Launched     bool            `json:"launched" db:"launched"`
	Enabled      bool            `json:"enabled" db:"enabled"`
	SMMTier      string          `json:"smmTier" db:"smm_tier"`
	SMMCaps      json.RawMessage `json:"smmCaps" db:"smm_caps"`
	ButtonSet    json.RawMessage `json:"buttonSet" db:"button_set"`
	SubsBaseline int             `json:"subsBaseline" db:"subs_baseline"`
	CreatedAt    time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time       `json:"updatedAt" db:"updated_at"`
}

// MpContentPost is a scheduled / draft / published post for a channel.
type MpContentPost struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	TenantID       uuid.UUID       `json:"tenantId" db:"tenant_id"`
	ChannelID      uuid.UUID       `json:"channelId" db:"channel_id"`
	ScheduledDate  time.Time       `json:"scheduledDate" db:"scheduled_date"`
	ScheduledAtUTC *time.Time      `json:"scheduledAtUtc,omitempty" db:"scheduled_at_utc"`
	Status         string          `json:"status" db:"status"`
	KoText         string          `json:"koText" db:"ko_text"`
	EnText         string          `json:"enText" db:"en_text"`
	ImagePath      string          `json:"imagePath" db:"image_path"`
	Buttons        json.RawMessage `json:"buttons" db:"buttons"`
	TgMessageID    *int64          `json:"tgMessageId,omitempty" db:"tg_message_id"`
	TgLink         string          `json:"tgLink" db:"tg_link"`
	ApprovedBy     string          `json:"approvedBy" db:"approved_by"`
	ApprovedAt     *time.Time      `json:"approvedAt,omitempty" db:"approved_at"`
	CreatedAt      time.Time       `json:"createdAt" db:"created_at"`
	UpdatedAt      time.Time       `json:"updatedAt" db:"updated_at"`
}

// Post status constants.
const (
	MpPostDraft      = "draft"
	MpPostApproved   = "approved"
	MpPostPublishing = "publishing"
	MpPostPublished  = "published"
	MpPostSkipped    = "skipped"
)

// MpChannelMetric is a daily subscriber-count sample for a channel.
type MpChannelMetric struct {
	ID              uuid.UUID `json:"id" db:"id"`
	TenantID        uuid.UUID `json:"tenantId" db:"tenant_id"`
	ChannelID       uuid.UUID `json:"channelId" db:"channel_id"`
	Date            time.Time `json:"date" db:"date"`
	SubscriberCount int       `json:"subscriberCount" db:"subscriber_count"`
	Delta           int       `json:"delta" db:"delta"`
	Stale           bool      `json:"stale" db:"stale"`
	AlertedForDate  bool      `json:"alertedForDate" db:"alerted_for_date"`
	CreatedAt       time.Time `json:"createdAt" db:"created_at"`
}

// MpSmmOrder is a single smmcity engagement order for a post+service.
type MpSmmOrder struct {
	ID         uuid.UUID `json:"id" db:"id"`
	TenantID   uuid.UUID `json:"tenantId" db:"tenant_id"`
	PostID     uuid.UUID `json:"postId" db:"post_id"`
	ServiceID  string    `json:"serviceId" db:"service_id"`
	Qty        int       `json:"qty" db:"qty"`
	SmmOrderID string    `json:"smmOrderId" db:"smm_order_id"`
	Status     string    `json:"status" db:"status"`
	Cost       float64   `json:"cost" db:"cost"`
	CreatedAt  time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt  time.Time `json:"updatedAt" db:"updated_at"`
}

// Order status constants.
const (
	MpOrderPending = "pending"
	MpOrderPlaced  = "placed"
	MpOrderDone    = "done"
	MpOrderError   = "error"
)

// Report period kinds.
const (
	MpReportDaily   = "daily"
	MpReportWeekly  = "weekly"
	MpReportMonthly = "monthly"
)

// MeowStore is the single persistence interface for the Meow autopilot
// (channels + posts + metrics + smm orders + report ledger). PostgreSQL-only:
// the channel system is gated off in Lite/desktop, so there is no SQLite impl.
// Every method is tenant-scoped; reads/writes by id also require the tenant so
// a foreign id resolves to NotFound rather than leaking across tenants.
type MeowStore interface {
	// Channels
	UpsertChannel(ctx context.Context, ch *MpChannel) error
	GetChannel(ctx context.Context, tenantID, id uuid.UUID) (*MpChannel, error)
	GetChannelByHandle(ctx context.Context, tenantID uuid.UUID, handle string) (*MpChannel, error)
	ListChannels(ctx context.Context, tenantID uuid.UUID) ([]MpChannel, error)

	// Posts
	CreatePost(ctx context.Context, p *MpContentPost) error
	// UpsertDraftPost inserts a draft for (channel, scheduled_date) or updates
	// the existing draft row for that channel-day in place (idempotent ingest).
	// Only touches status='draft' rows; never disturbs approved/published posts.
	UpsertDraftPost(ctx context.Context, p *MpContentPost) error
	GetPost(ctx context.Context, tenantID, id uuid.UUID) (*MpContentPost, error)
	ListPostsByChannel(ctx context.Context, tenantID, channelID uuid.UUID) ([]MpContentPost, error)
	// UpdatePostStatus transitions a post and records publish results. The
	// partial unique index (channel_id, scheduled_date) WHERE status IN
	// ('publishing','published') enforces exactly-once at the DB layer.
	UpdatePostStatus(ctx context.Context, tenantID, id uuid.UUID, status string, tgMessageID *int64, tgLink string) error
	// ClaimPostForPublish atomically transitions exactly one eligible post for
	// (channel_id, scheduled_date) to 'publishing' and returns it. Eligible =
	// status 'approved' (or 'approved'/'draft' when force). It claims nothing
	// (ErrMeowNoClaimablePost) if a post for that channel-day is already
	// publishing/published, so a re-run is a safe no-op (exactly-once).
	ClaimPostForPublish(ctx context.Context, tenantID, channelID uuid.UUID, date time.Time, force bool) (*MpContentPost, error)
	// ApprovePost transitions a draft post to 'approved', recording the approver
	// id and approval time. It acts only on a status='draft' row; a missing or
	// non-draft post yields ErrMeowPostNotFound (nothing to approve).
	ApprovePost(ctx context.Context, tenantID, id uuid.UUID, approvedBy string) error
	// SkipPost transitions a draft or approved post to 'skipped'. It never
	// touches a publishing/published row; a missing or terminal post yields
	// ErrMeowPostNotFound (nothing to skip).
	SkipPost(ctx context.Context, tenantID, id uuid.UUID) error

	// Metrics (UNIQUE(channel_id, date) — Upsert is the dedup primitive)
	UpsertMetric(ctx context.Context, m *MpChannelMetric) error
	GetMetric(ctx context.Context, tenantID, channelID uuid.UUID, date time.Time) (*MpChannelMetric, error)

	// smm orders (UNIQUE(post_id, service_id) — no double-spend)
	CreateOrder(ctx context.Context, o *MpSmmOrder) error
	GetOrderByPostService(ctx context.Context, tenantID, postID uuid.UUID, serviceID string) (*MpSmmOrder, error)

	// Report idempotency ledger
	RecordReportSent(ctx context.Context, tenantID uuid.UUID, periodKind, periodKey string) error
	ReportAlreadySent(ctx context.Context, tenantID uuid.UUID, periodKind, periodKey string) (bool, error)
}
