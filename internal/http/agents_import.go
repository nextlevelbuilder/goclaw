package http

import (
	"encoding/json"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

const maxImportBodySize = 500 << 20 // 500MB

// importCleanup tracks created context files for rollback on failure.
type importCleanup struct {
	mu    sync.Mutex
	files []string // agent context file names (for log only; DB rollback handles actual cleanup)
}

func (c *importCleanup) TrackFile(name string) {
	c.mu.Lock()
	c.files = append(c.files, name)
	c.mu.Unlock()
}

// importArchive is the parsed contents of an agent archive.
type importArchive struct {
	manifest         *ExportManifest
	agentConfig      map[string]json.RawMessage
	contextFiles     []importContextFile
	userContextFiles []importUserContextFile
	memoryGlobal     []MemoryExport
	memoryUsers      map[string][]MemoryExport // userID → docs
	kgEntities       []KGEntityExport
	kgRelations      []KGRelationExport
	cronJobs         []pg.CronJobExport
	userProfiles     []pg.UserProfileExport
	userOverrides    []pg.UserOverrideExport
	workspaceFiles   map[string][]byte // relative path → content
	// Episodic section: Tier 2 session summaries
	episodicSummaries []pg.EpisodicSummaryExport
	// Evolution section: self-evolution metrics + suggestions
	evolutionMetrics     []pg.EvolutionMetricExport
	evolutionSuggestions []pg.EvolutionSuggestionExport
	// Vault section: Knowledge Vault documents + links
	vaultDocuments []pg.VaultDocumentExport
	vaultLinks     []pg.VaultLinkExport
	// Team section (used by standalone team import)
	teamMeta      *pg.TeamExport
	teamMembers   []pg.TeamMemberExport
	teamTasks     []pg.TeamTaskExport
	teamComments  []pg.TeamTaskCommentExport
	teamEvents    []pg.TeamTaskEventExport
	teamLinks     []pg.AgentLinkExport
	teamWorkspace map[string][]byte // relative path → content
}

type importContextFile struct {
	fileName string
	content  string
}

type importUserContextFile struct {
	userID   string
	fileName string
	content  string
}

// ImportSummary is returned after a successful import.
type ImportSummary struct {
	AgentID              string `json:"agent_id"`
	AgentKey             string `json:"agent_key"`
	ContextFiles         int    `json:"context_files"`
	UserContextFiles     int    `json:"user_context_files"`
	MemoryDocs           int    `json:"memory_docs"`
	KGEntities           int    `json:"kg_entities"`
	KGRelations          int    `json:"kg_relations"`
	CronJobs             int    `json:"cron_jobs"`
	UserProfiles         int    `json:"user_profiles"`
	UserOverrides        int    `json:"user_overrides"`
	WorkspaceFiles       int    `json:"workspace_files"`
	EpisodicSummaries    int    `json:"episodic_summaries"`
	EvolutionMetrics     int    `json:"evolution_metrics"`
	EvolutionSuggestions int    `json:"evolution_suggestions"`
	VaultDocuments       int    `json:"vault_documents"`
	VaultLinks           int    `json:"vault_links"`
	TeamImported         bool   `json:"team_imported"`
}
