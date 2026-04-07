package pg

import (
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// mcpAccessRequestRow is an sqlx scan struct for mcp_access_requests SELECT queries.
// Nullable fields (AgentID, UserID, ReviewedBy, ReviewNote) use pointer types.
type mcpAccessRequestRow struct {
	ID          uuid.UUID  `db:"id"`
	ServerID    uuid.UUID  `db:"server_id"`
	AgentID     *uuid.UUID `db:"agent_id"`
	UserID      *string    `db:"user_id"`
	Scope       string     `db:"scope"`
	Status      string     `db:"status"`
	Reason      string     `db:"reason"`
	ToolAllow   []byte     `db:"tool_allow"`
	RequestedBy string     `db:"requested_by"`
	ReviewedBy  *string    `db:"reviewed_by"`
	ReviewedAt  *time.Time `db:"reviewed_at"`
	ReviewNote  *string    `db:"review_note"`
	CreatedAt   time.Time  `db:"created_at"`
}

// toMCPAccessRequest converts a mcpAccessRequestRow to store.MCPAccessRequest.
func (r *mcpAccessRequestRow) toMCPAccessRequest() store.MCPAccessRequest {
	req := store.MCPAccessRequest{
		ID:          r.ID,
		ServerID:    r.ServerID,
		AgentID:     r.AgentID,
		Scope:       r.Scope,
		Status:      r.Status,
		Reason:      r.Reason,
		RequestedBy: r.RequestedBy,
		ReviewedAt:  r.ReviewedAt,
		CreatedAt:   r.CreatedAt,
	}
	if len(r.ToolAllow) > 0 {
		req.ToolAllow = r.ToolAllow
	}
	req.UserID = derefStr(r.UserID)
	req.ReviewedBy = derefStr(r.ReviewedBy)
	req.ReviewNote = derefStr(r.ReviewNote)
	return req
}
