package pg

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// --- Intermediate scan structs for sqlx SELECT queries ---
// Used where the domain struct has incompatible field types (e.g. *[]byte for JSONB, map for JSON).

// agentBackfillRow is used by BackfillAgentEmbeddings to avoid
// allocating a full AgentData for the minimal 3-column query.
type agentBackfillRow struct {
	ID          uuid.UUID `db:"id"`
	DisplayName string    `db:"display_name"`
	Frontmatter string    `db:"frontmatter"`
}

// agentShareRow maps the 6-column agent_shares SELECT query.
// AgentShareData embeds BaseModel (id, created_at, updated_at) but the query
// only selects id, agent_id, user_id, role, granted_by, created_at — so we use
// a flat intermediate to avoid scan mismatches on missing updated_at.
type agentShareRow struct {
	ID        uuid.UUID `db:"id"`
	AgentID   uuid.UUID `db:"agent_id"`
	UserID    string    `db:"user_id"`
	Role      string    `db:"role"`
	GrantedBy string    `db:"granted_by"`
	CreatedAt time.Time `db:"created_at"`
}

func (r agentShareRow) toAgentShareData() store.AgentShareData {
	d := store.AgentShareData{}
	d.ID = r.ID
	d.AgentID = r.AgentID
	d.UserID = r.UserID
	d.Role = r.Role
	d.GrantedBy = r.GrantedBy
	d.CreatedAt = r.CreatedAt
	return d
}

// userInstanceRow is an intermediate for ListUserInstances.
// UserInstanceData.Metadata is map[string]string with db:"-", so we scan the raw JSON separately.
type userInstanceRow struct {
	UserID      string  `db:"user_id"`
	FirstSeenAt *string `db:"first_seen_at"`
	LastSeenAt  *string `db:"last_seen_at"`
	FileCount   int     `db:"file_count"`
	MetadataRaw []byte  `db:"metadata"`
}

func (r userInstanceRow) toUserInstanceData() store.UserInstanceData {
	d := store.UserInstanceData{
		UserID:      r.UserID,
		FirstSeenAt: r.FirstSeenAt,
		LastSeenAt:  r.LastSeenAt,
		FileCount:   r.FileCount,
	}
	if len(r.MetadataRaw) > 0 {
		json.Unmarshal(r.MetadataRaw, &d.Metadata) //nolint:errcheck
	}
	return d
}

// teamMemberAgentRow is used by GetTeamMemberAgents which returns an anonymous struct.
// We avoid anonymous structs in sqlx since they can't carry db tags.
type teamMemberAgentRow struct {
	ID       uuid.UUID `db:"id"`
	AgentKey string    `db:"agent_key"`
}
