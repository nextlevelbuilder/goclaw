package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	ChannelMemoryRunPending   = "pending"
	ChannelMemoryRunRunning   = "running"
	ChannelMemoryRunCompleted = "completed"
	ChannelMemoryRunFailed    = "failed"

	ChannelMemoryItemPendingReview = "pending_review"
	ChannelMemoryItemApproved      = "approved"
	ChannelMemoryItemRejected      = "rejected"
	ChannelMemoryItemWritten       = "written"
	ChannelMemoryItemDeleted       = "deleted"
)

type ChannelMemoryExtractionRun struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	TenantID          uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	ChannelInstanceID uuid.UUID       `json:"channel_instance_id" db:"channel_instance_id"`
	ChannelName       string          `json:"channel_name" db:"channel_name"`
	AgentID           uuid.UUID       `json:"agent_id" db:"agent_id"`
	UserID            string          `json:"user_id" db:"user_id"`
	HistoryKey        string          `json:"history_key" db:"history_key"`
	Trigger           string          `json:"trigger" db:"trigger"`
	Status            string          `json:"status" db:"status"`
	SourceStartID     string          `json:"source_start_id" db:"source_start_id"`
	SourceEndID       string          `json:"source_end_id" db:"source_end_id"`
	SourceStartAt     *time.Time      `json:"source_start_at,omitempty" db:"source_start_at"`
	SourceEndAt       *time.Time      `json:"source_end_at,omitempty" db:"source_end_at"`
	MessageCount      int             `json:"message_count" db:"message_count"`
	RedactionCount    int             `json:"redaction_count" db:"redaction_count"`
	RedactionTypes    json.RawMessage `json:"redaction_types" db:"redaction_types"`
	ItemCount         int             `json:"item_count" db:"item_count"`
	ErrorMessage      string          `json:"error_message,omitempty" db:"error_message"`
	StartedAt         *time.Time      `json:"started_at,omitempty" db:"started_at"`
	CompletedAt       *time.Time      `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type ChannelMemoryExtractionItem struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	TenantID          uuid.UUID       `json:"tenant_id" db:"tenant_id"`
	RunID             uuid.UUID       `json:"run_id" db:"run_id"`
	ChannelInstanceID uuid.UUID       `json:"channel_instance_id" db:"channel_instance_id"`
	AgentID           uuid.UUID       `json:"agent_id" db:"agent_id"`
	UserID            string          `json:"user_id" db:"user_id"`
	ItemHash          string          `json:"item_hash" db:"item_hash"`
	ItemType          string          `json:"item_type" db:"item_type"`
	Summary           string          `json:"summary" db:"summary"`
	Topics            json.RawMessage `json:"topics" db:"topics"`
	Entities          json.RawMessage `json:"entities" db:"entities"`
	Confidence        float64         `json:"confidence" db:"confidence"`
	SourceID          string          `json:"source_id" db:"source_id"`
	Status            string          `json:"status" db:"status"`
	ApprovedBy        string          `json:"approved_by,omitempty" db:"approved_by"`
	ApprovedAt        *time.Time      `json:"approved_at,omitempty" db:"approved_at"`
	RejectedBy        string          `json:"rejected_by,omitempty" db:"rejected_by"`
	RejectedAt        *time.Time      `json:"rejected_at,omitempty" db:"rejected_at"`
	DeletedAt         *time.Time      `json:"deleted_at,omitempty" db:"deleted_at"`
	WrittenAt         *time.Time      `json:"written_at,omitempty" db:"written_at"`
	EpisodicID        string          `json:"episodic_id,omitempty" db:"episodic_id"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

type ChannelMemoryRunListOptions struct {
	ChannelInstanceID uuid.UUID
	HistoryKey        string
	Status            string
	Limit             int
}

type ChannelMemoryItemListOptions struct {
	ChannelInstanceID uuid.UUID
	RunID             uuid.UUID
	Status            string
	Limit             int
}

type ChannelMemoryExtractionStore interface {
	CreateRun(ctx context.Context, run *ChannelMemoryExtractionRun) error
	GetRun(ctx context.Context, id uuid.UUID) (*ChannelMemoryExtractionRun, error)
	ListRuns(ctx context.Context, opts ChannelMemoryRunListOptions) ([]ChannelMemoryExtractionRun, error)
	UpdateRun(ctx context.Context, id uuid.UUID, updates map[string]any) error

	CreateItem(ctx context.Context, item *ChannelMemoryExtractionItem) error
	GetItem(ctx context.Context, id uuid.UUID) (*ChannelMemoryExtractionItem, error)
	ListItems(ctx context.Context, opts ChannelMemoryItemListOptions) ([]ChannelMemoryExtractionItem, error)
	UpdateItem(ctx context.Context, id uuid.UUID, updates map[string]any) error
}
