package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ContractKind enumerates the kinds of durable multi-agent collaboration
// records persisted in multi_agent_records. Values are stored as plain strings
// and are kept independent of any domain package so the store layer never
// imports internal/contract (no import cycle).
const (
	ContractRecordHandoff     = "handoff"
	ContractRecordJury        = "jury"
	ContractRecordCompetition = "competition"
	ContractRecordNegotiation = "negotiation"
)

// ContractRecordStatus enumerates the lifecycle states of a multi-agent record.
const (
	ContractRecordDraft  = "draft"
	ContractRecordActive = "active"
	ContractRecordClosed = "closed"
)

// ValidContractRecordStatus reports whether s is a known contract record status.
func ValidContractRecordStatus(s string) bool {
	switch s {
	case ContractRecordDraft, ContractRecordActive, ContractRecordClosed:
		return true
	}
	return false
}

// ContractRecord is one durable row of a multi-agent collaboration. Body holds
// the JSON-encoded contract plus any verdicts/counter-proposals produced during
// the run; it is stored as JSONB (PostgreSQL) or TEXT (SQLite) and treated as
// opaque by the store layer.
type ContractRecord struct {
	ID        uuid.UUID `json:"id" db:"id"`
	TenantID  uuid.UUID `json:"tenant_id" db:"tenant_id"`
	RunID     string    `json:"run_id,omitempty" db:"run_id"`
	Kind      string    `json:"kind" db:"kind"`
	Body      string    `json:"body,omitempty" db:"body"`
	Status    string    `json:"status,omitempty" db:"status"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ContractRecordListOpts scopes a contract record list read. Reads are
// tenant-scoped via context; RunID/Kind/Status narrow the result set.
type ContractRecordListOpts struct {
	RunID  string
	Kind   string
	Status string
	Limit  int
	Offset int
}

// ContractStore persists durable multi-agent collaboration records (handoff,
// jury, competition, negotiation). All reads and writes are scoped to the
// context tenant and fail closed when a tenant is required but absent.
type ContractStore interface {
	// CreateContractRecord inserts a new record, assigning an ID and CreatedAt
	// when left empty. Kind is required and validated.
	CreateContractRecord(ctx context.Context, rec *ContractRecord) error
	// GetContractRecord returns one record by id, scoped to the context tenant.
	GetContractRecord(ctx context.Context, id uuid.UUID) (*ContractRecord, error)
	// ListContractRecords returns records filtered by opts, scoped to the
	// context tenant. Newest first.
	ListContractRecords(ctx context.Context, opts ContractRecordListOpts) ([]ContractRecord, error)
	// UpdateContractRecordStatus transitions one record's status, scoped to the
	// context tenant. The target status must be a known status.
	UpdateContractRecordStatus(ctx context.Context, id uuid.UUID, status string) error
}
