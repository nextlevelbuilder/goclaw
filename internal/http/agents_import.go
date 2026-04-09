package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
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

// parseImportSections parses the ?include= query param (comma-separated section names).
// Defaults to all sections if empty.
func parseImportSections(raw string) map[string]bool {
	all := map[string]bool{
		"config":          true,
		"context_files":   true,
		"memory":          true,
		"knowledge_graph": true,
		"cron":            true,
		"user_profiles":   true,
		"user_overrides":  true,
		"workspace":       true,
		"team":            true,
		"episodic":        true,
		"evolution":       true,
		"vault":           true,
	}
	if raw == "" {
		return all
	}
	out := make(map[string]bool)
	for s := range strings.SplitSeq(raw, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out[s] = true
		}
	}
	return out
}

// canImport checks if userID has permission to import agents (system owner only for now).
func (h *AgentsHandler) canImport(userID string) bool {
	return h.isOwnerUser(userID)
}

// handleImportPreview parses the archive manifest and returns it without importing.
func (h *AgentsHandler) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	if !h.canImport(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "import"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"manifest":              arc.manifest,
		"context_files":         len(arc.contextFiles),
		"user_context_files":    len(arc.userContextFiles),
		"memory_docs":           len(arc.memoryGlobal) + countUserMemory(arc.memoryUsers),
		"kg_entities":           len(arc.kgEntities),
		"kg_relations":          len(arc.kgRelations),
		"cron_jobs":             len(arc.cronJobs),
		"user_profiles":         len(arc.userProfiles),
		"user_overrides":        len(arc.userOverrides),
		"workspace_files":       len(arc.workspaceFiles),
		"episodic_summaries":    len(arc.episodicSummaries),
		"evolution_metrics":     len(arc.evolutionMetrics),
		"evolution_suggestions": len(arc.evolutionSuggestions),
		"vault_documents":       len(arc.vaultDocuments),
		"vault_links":           len(arc.vaultLinks),
		"team":                  arc.teamMeta != nil,
	})
}

// handleImport creates a new agent from an uploaded archive.
func (h *AgentsHandler) handleImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	if !h.canImport(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "import agent"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, importErr := h.doImportNewAgent(r.Context(), r, arc, progressFn)
		if importErr != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "import", Status: "error", Detail: importErr.Error()})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doImportNewAgent(r.Context(), r, arc, nil)
	if err != nil {
		slog.Error("agents.import", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

// handleMergeImport merges archive data into an existing agent.
func (h *AgentsHandler) handleMergeImport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())

	ag, status, err := h.lookupAccessibleAgent(r)
	if err != nil {
		writeError(w, status, protocol.ErrNotFound, err.Error())
		return
	}
	// Require agent owner or system owner
	if ag.OwnerID != userID && !h.isOwnerUser(userID) {
		writeError(w, http.StatusForbidden, protocol.ErrUnauthorized, i18n.T(locale, i18n.MsgNoAccess, "merge import"))
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodySize)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "multipart parse: "+err.Error()))
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "missing 'file' field"))
		return
	}
	defer f.Close()

	arc, err := readImportArchive(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidRequest, "archive parse: "+err.Error()))
		return
	}

	sections := parseImportSections(r.URL.Query().Get("include"))
	stream := r.URL.Query().Get("stream") == "true"

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
			return
		}
		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		summary, mergeErr := h.doMergeImport(r.Context(), ag, arc, sections, progressFn)
		if mergeErr != nil {
			slog.Error("agents.merge_import.sse", "agent_id", ag.ID, "error", mergeErr)
			sendSSE(w, flusher, "error", map[string]any{"phase": "merge", "detail": mergeErr.Error(), "rolled_back": false})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doMergeImport(r.Context(), ag, arc, sections, nil)
	if err != nil {
		slog.Error("agents.merge_import", "agent_id", ag.ID, "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

// doImportNewAgent creates a new agent from the archive, returning import summary.
func (h *AgentsHandler) doImportNewAgent(ctx context.Context, r *http.Request, arc *importArchive, progressFn func(ProgressEvent)) (*ImportSummary, error) {
	tenantID := store.TenantIDFromContext(ctx)
	userID := store.UserIDFromContext(ctx)

	// Build agent record from archive config + optional overrides
	agentKey := r.FormValue("agent_key")
	displayName := r.FormValue("display_name")

	if agentKey == "" && arc.agentConfig["agent_key"] != nil {
		json.Unmarshal(arc.agentConfig["agent_key"], &agentKey) //nolint:errcheck
	}
	if displayName == "" && arc.agentConfig["display_name"] != nil {
		json.Unmarshal(arc.agentConfig["display_name"], &displayName) //nolint:errcheck
	}
	if agentKey == "" && displayName != "" {
		agentKey = config.NormalizeAgentID(displayName)
	}
	if agentKey == "" {
		return nil, errors.New("agent_key is required (not found in archive or request)")
	}

	// Dedup: suffix with -N if key already exists
	agentKey = h.dedupAgentKey(ctx, agentKey)

	ag := h.buildAgentFromArchive(arc.agentConfig, agentKey, displayName, tenantID, userID)

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "running"})
	}

	if err := h.agents.Create(ctx, ag); err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "done", Current: 1, Total: 1})
	}

	sections := map[string]bool{
		"context_files":   true,
		"memory":          true,
		"knowledge_graph": true,
		"cron":            true,
		"user_profiles":   true,
		"user_overrides":  true,
		"workspace":       true,
		"team":            true,
		"episodic":        true,
		"evolution":       true,
		"vault":           true,
	}
	summary, err := h.doMergeImport(ctx, ag, arc, sections, progressFn)
	if err != nil {
		// Best-effort: agent already created, log but return partial summary
		slog.Error("agents.import.merge_data", "agent_id", ag.ID, "error", err)
		return &ImportSummary{AgentID: ag.ID.String(), AgentKey: ag.AgentKey}, err
	}
	summary.AgentID = ag.ID.String()
	summary.AgentKey = ag.AgentKey
	return summary, nil
}

// doMergeImport upserts sections of the archive into the target agent.
func (h *AgentsHandler) doMergeImport(ctx context.Context, ag *store.AgentData, arc *importArchive, sections map[string]bool, progressFn func(ProgressEvent)) (*ImportSummary, error) {
	summary := &ImportSummary{AgentID: ag.ID.String(), AgentKey: ag.AgentKey}

	// Section: context_files
	if sections["context_files"] {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "context_files", Status: "running", Total: len(arc.contextFiles)})
		}
		for _, cf := range arc.contextFiles {
			if err := h.agents.SetAgentContextFile(ctx, ag.ID, cf.fileName, cf.content); err != nil {
				return nil, fmt.Errorf("set context file %s: %w", cf.fileName, err)
			}
			summary.ContextFiles++
		}
		for _, ucf := range arc.userContextFiles {
			if err := h.agents.SetUserContextFile(ctx, ag.ID, ucf.userID, ucf.fileName, ucf.content); err != nil {
				return nil, fmt.Errorf("set user context file %s/%s: %w", ucf.userID, ucf.fileName, err)
			}
			summary.UserContextFiles++
		}
		if progressFn != nil {
			total := summary.ContextFiles + summary.UserContextFiles
			progressFn(ProgressEvent{Phase: "context_files", Status: "done", Current: total, Total: total})
		}
	}

	// Section: memory
	if sections["memory"] && h.memoryStore != nil {
		totalDocs := len(arc.memoryGlobal) + countUserMemory(arc.memoryUsers)
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "memory", Status: "running", Total: totalDocs})
		}
		for _, doc := range arc.memoryGlobal {
			if err := h.memoryStore.PutDocument(ctx, ag.ID.String(), "", doc.Path, doc.Content); err != nil {
				return nil, fmt.Errorf("put memory doc %s: %w", doc.Path, err)
			}
			summary.MemoryDocs++
		}
		for uid, docs := range arc.memoryUsers {
			for _, doc := range docs {
				if err := h.memoryStore.PutDocument(ctx, ag.ID.String(), uid, doc.Path, doc.Content); err != nil {
					return nil, fmt.Errorf("put memory doc %s/%s: %w", uid, doc.Path, err)
				}
				summary.MemoryDocs++
			}
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "memory", Status: "done", Current: summary.MemoryDocs, Total: totalDocs})
		}
		// Async re-index
		// Extract paths before goroutine to allow arc GC
		paths := collectMemoryPaths(arc)
		go h.reindexMemoryPaths(context.WithoutCancel(ctx), ag.ID.String(), paths)
	}

	// Section: knowledge_graph
	if sections["knowledge_graph"] && h.kgStore != nil && len(arc.kgEntities) > 0 {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph", Status: "running", Total: len(arc.kgEntities)})
		}
		if err := h.ingestKGByUser(ctx, ag.ID.String(), arc); err != nil {
			return nil, fmt.Errorf("ingest kg: %w", err)
		}
		summary.KGEntities = len(arc.kgEntities)
		summary.KGRelations = len(arc.kgRelations)
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph", Status: "done", Current: len(arc.kgEntities), Total: len(arc.kgEntities)})
		}
	}

	// Section: cron — always imported as disabled; UPDATE existing by name, INSERT new
	if sections["cron"] && len(arc.cronJobs) > 0 {
		tid := importTenantID(ctx)
		for _, j := range arc.cronJobs {
			// cron_jobs has no UNIQUE constraint on (agent_id, name), so use SELECT+UPDATE/INSERT
			var exists bool
			_ = h.db.QueryRowContext(ctx,
				`SELECT EXISTS(SELECT 1 FROM cron_jobs WHERE agent_id = $1 AND name = $2 AND tenant_id = $3)`,
				ag.ID, j.Name, tid,
			).Scan(&exists)
			if exists {
				_, err := h.db.ExecContext(ctx,
					`UPDATE cron_jobs
					 SET schedule_kind=$1, cron_expression=$2, interval_ms=$3,
					     run_at=$4, timezone=$5, payload=$6, delete_after_run=$7
					 WHERE agent_id=$8 AND name=$9 AND tenant_id=$10`,
					j.ScheduleKind, j.CronExpression, j.IntervalMS,
					nullStr(j.RunAt), j.Timezone, j.Payload, j.DeleteAfterRun,
					ag.ID, j.Name, tid,
				)
				if err != nil {
					slog.Warn("agents.import.cron_job.update", "agent_id", ag.ID, "name", j.Name, "error", err)
				}
			} else {
				_, err := h.db.ExecContext(ctx,
					`INSERT INTO cron_jobs
					   (agent_id, name, enabled, schedule_kind, cron_expression, interval_ms, run_at, timezone, payload, delete_after_run, tenant_id)
					 VALUES ($1, $2, false, $3, $4, $5, $6, $7, $8, $9, $10)`,
					ag.ID, j.Name, j.ScheduleKind,
					j.CronExpression, j.IntervalMS, nullStr(j.RunAt), j.Timezone,
					j.Payload, j.DeleteAfterRun, tid,
				)
				if err != nil {
					slog.Warn("agents.import.cron_job.insert", "agent_id", ag.ID, "name", j.Name, "error", err)
				}
			}
			summary.CronJobs++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "cron", Status: "done", Current: summary.CronJobs, Total: len(arc.cronJobs)})
		}
	}

	// Section: user_profiles — insert if not exists, workspace=NULL for portability
	// (workspace is auto-created via GetOrCreateUserProfile on first user access)
	if sections["user_profiles"] && len(arc.userProfiles) > 0 {
		tid := importTenantID(ctx)
		for _, p := range arc.userProfiles {
			_, err := h.db.ExecContext(ctx,
				`INSERT INTO user_agent_profiles (agent_id, user_id, workspace, tenant_id)
				 VALUES ($1, $2, NULL, $3)
				 ON CONFLICT DO NOTHING`,
				ag.ID, p.UserID, tid,
			)
			if err != nil {
				slog.Warn("agents.import.user_profile", "agent_id", ag.ID, "user_id", p.UserID, "error", err)
				continue
			}
			summary.UserProfiles++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_profiles", Status: "done", Current: summary.UserProfiles, Total: len(arc.userProfiles)})
		}
	}

	// Section: user_overrides — UPSERT: update provider/model/settings on conflict(agent_id, user_id)
	if sections["user_overrides"] && len(arc.userOverrides) > 0 {
		tid := importTenantID(ctx)
		for _, o := range arc.userOverrides {
			_, err := h.db.ExecContext(ctx,
				`INSERT INTO user_agent_overrides (agent_id, user_id, provider, model, settings, tenant_id)
				 VALUES ($1, $2, $3, $4, $5, $6)
				 ON CONFLICT (agent_id, user_id) DO UPDATE SET
				     provider = EXCLUDED.provider,
				     model = EXCLUDED.model,
				     settings = EXCLUDED.settings,
				     updated_at = NOW()`,
				ag.ID, o.UserID, o.Provider, o.Model, coalesceJSON(o.Settings), tid,
			)
			if err != nil {
				slog.Warn("agents.import.user_override", "agent_id", ag.ID, "user_id", o.UserID, "error", err)
				continue
			}
			summary.UserOverrides++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_overrides", Status: "done", Current: summary.UserOverrides, Total: len(arc.userOverrides)})
		}
	}

	// Section: workspace files
	if sections["workspace"] && len(arc.workspaceFiles) > 0 {
		wsPath := config.ExpandHome(fmt.Sprintf("%s/%s", h.defaultWorkspace, ag.AgentKey))
		imported, wsErr := extractWorkspaceFiles(wsPath, arc.workspaceFiles, false)
		if wsErr != nil {
			slog.Warn("import: workspace extraction failed", "path", wsPath, "error", wsErr)
		}
		summary.WorkspaceFiles = imported
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "workspace", Status: "done", Current: imported, Total: len(arc.workspaceFiles)})
		}
	}

	// Section: team
	if sections["team"] && arc.teamMeta != nil {
		if err := h.importTeamSection(ctx, ag, arc, progressFn); err != nil {
			slog.Warn("import: team section failed", "agent_id", ag.ID, "error", err)
		} else {
			summary.TeamImported = true
		}
	}

	// Section: evolution metrics + suggestions — raw SQL (no store interface required)
	// Metrics are time-series: re-import duplicates are acceptable for v1.
	// Suggestions: dedup by (agent_id, suggestion_type, suggestion) via SELECT EXISTS.
	if sections["evolution"] {
		tid := importTenantID(ctx)
		if len(arc.evolutionMetrics) > 0 {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "evolution_metrics", Status: "running", Total: len(arc.evolutionMetrics)})
			}
			for _, m := range arc.evolutionMetrics {
				_, err := h.db.ExecContext(ctx,
					`INSERT INTO agent_evolution_metrics
					   (agent_id, session_key, metric_type, metric_key, value, created_at, tenant_id)
					 VALUES ($1, $2, $3, $4, $5, $6::timestamptz, $7)`,
					ag.ID, m.SessionKey, m.MetricType, m.MetricKey,
					nullJSON(m.Value), m.CreatedAt, tid,
				)
				if err != nil {
					slog.Warn("agents.import.evolution_metric", "agent_id", ag.ID, "error", err)
					continue
				}
				summary.EvolutionMetrics++
			}
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "evolution_metrics", Status: "done", Current: summary.EvolutionMetrics, Total: len(arc.evolutionMetrics)})
			}
		}
		if len(arc.evolutionSuggestions) > 0 {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "evolution_suggestions", Status: "running", Total: len(arc.evolutionSuggestions)})
			}
			for _, s := range arc.evolutionSuggestions {
				var exists bool
				_ = h.db.QueryRowContext(ctx,
					`SELECT EXISTS(SELECT 1 FROM agent_evolution_suggestions
					  WHERE agent_id = $1 AND suggestion_type = $2 AND suggestion = $3 AND tenant_id = $4)`,
					ag.ID, s.SuggestionType, s.Suggestion, tid,
				).Scan(&exists)
				if exists {
					continue
				}
				_, err := h.db.ExecContext(ctx,
					`INSERT INTO agent_evolution_suggestions
					   (agent_id, suggestion_type, suggestion, rationale, parameters,
					    status, reviewed_by, reviewed_at, created_at, tenant_id)
					 VALUES ($1, $2, $3, $4, $5, $6, $7, $8::timestamptz, $9::timestamptz, $10)`,
					ag.ID, s.SuggestionType, s.Suggestion, s.Rationale,
					nullJSON(s.Parameters), s.Status,
					nullStrVal(s.ReviewedBy), nullStr(s.ReviewedAt),
					s.CreatedAt, tid,
				)
				if err != nil {
					slog.Warn("agents.import.evolution_suggestion", "agent_id", ag.ID, "type", s.SuggestionType, "error", err)
					continue
				}
				summary.EvolutionSuggestions++
			}
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "evolution_suggestions", Status: "done", Current: summary.EvolutionSuggestions, Total: len(arc.evolutionSuggestions)})
			}
		}
	}

	// Section: episodic summaries (Tier 2 memory) — PG only, nil-guarded
	if sections["episodic"] && h.episodicStore != nil && len(arc.episodicSummaries) > 0 {
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "episodic", Status: "running", Total: len(arc.episodicSummaries)})
		}
		tid := importTenantID(ctx)
		for _, ep := range arc.episodicSummaries {
			exists, _ := h.episodicStore.ExistsBySourceID(ctx, ag.ID.String(), ep.UserID, ep.SourceID)
			if exists {
				continue
			}
			epSum := &store.EpisodicSummary{
				TenantID:   tid,
				AgentID:    ag.ID,
				UserID:     ep.UserID,
				SessionKey: ep.SessionKey,
				Summary:    ep.Summary,
				KeyTopics:  ep.KeyTopics,
				L0Abstract: ep.L0Abstract,
				SourceType: ep.SourceType,
				SourceID:   ep.SourceID,
				TurnCount:  ep.TurnCount,
				TokenCount: ep.TokenCount,
				// Embedding: nil (not exported; re-index separately)
				// ExpiresAt: not restored (summaries have no expiry on import)
			}
			if err := h.episodicStore.Create(ctx, epSum); err != nil {
				slog.Warn("agents.import.episodic", "agent_id", ag.ID, "source_id", ep.SourceID, "error", err)
				continue
			}
			summary.EpisodicSummaries++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "episodic", Status: "done", Current: summary.EpisodicSummaries, Total: len(arc.episodicSummaries)})
		}
	}

	// Section: vault (Knowledge Vault documents + links)
	if sections["vault"] && h.vaultStore != nil && len(arc.vaultDocuments) > 0 {
		tid := importTenantID(ctx)
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "vault_documents", Status: "running", Total: len(arc.vaultDocuments)})
		}
		for _, d := range arc.vaultDocuments {
			doc := &store.VaultDocument{
				TenantID:    tid.String(),
				AgentID:     ag.ID.String(),
				TeamID:      nil, // team_id not portable
				Scope:       d.Scope,
				CustomScope: d.CustomScope,
				Path:        d.Path,
				Title:       d.Title,
				DocType:     d.DocType,
				ContentHash: d.ContentHash,
				Summary:     d.Summary,
				// Embedding nil — re-indexed by vault FS sync
			}
			if len(d.Metadata) > 0 {
				if err := json.Unmarshal(d.Metadata, &doc.Metadata); err != nil {
					doc.Metadata = nil
				}
			}
			if err := h.vaultStore.UpsertDocument(ctx, doc); err != nil {
				slog.Warn("agents.import.vault_doc", "agent_id", ag.ID, "path", d.Path, "error", err)
				continue
			}
			summary.VaultDocuments++
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "vault_documents", Status: "done", Current: summary.VaultDocuments, Total: len(arc.vaultDocuments)})
		}

		// Two-pass link import: build pathToID map first
		if len(arc.vaultLinks) > 0 {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "vault_links", Status: "running", Total: len(arc.vaultLinks)})
			}
			pathToID := make(map[string]string)
			rows, qErr := h.db.QueryContext(ctx,
				`SELECT id, path FROM vault_documents WHERE agent_id = $1 AND tenant_id = $2`,
				ag.ID, tid,
			)
			if qErr == nil {
				for rows.Next() {
					var id, path string
					if err := rows.Scan(&id, &path); err == nil {
						pathToID[path] = id
					}
				}
				if err := rows.Err(); err != nil {
					slog.Warn("agents.import.vault_link_resolution", "error", err)
				}
				rows.Close()
			}

			for _, l := range arc.vaultLinks {
				fromID, ok1 := pathToID[l.FromDocPath]
				toID, ok2 := pathToID[l.ToDocPath]
				if !ok1 || !ok2 {
					// Target doc not found — skip gracefully
					continue
				}
				link := &store.VaultLink{
					FromDocID: fromID,
					ToDocID:   toID,
					LinkType:  l.LinkType,
					Context:   l.Context,
				}
				if err := h.vaultStore.CreateLink(ctx, link); err != nil {
					slog.Warn("agents.import.vault_link", "agent_id", ag.ID, "from", l.FromDocPath, "to", l.ToDocPath, "error", err)
					continue
				}
				summary.VaultLinks++
			}
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "vault_links", Status: "done", Current: summary.VaultLinks, Total: len(arc.vaultLinks)})
			}
		}
	}

	return summary, nil
}

// ingestKGByUser groups KG entities/relations by user_id and calls IngestExtraction per group.
func (h *AgentsHandler) ingestKGByUser(ctx context.Context, agentID string, arc *importArchive) error {
	// Group entities by user_id
	type userGroup struct {
		entities  []store.Entity
		relations []store.Relation
	}
	groups := make(map[string]*userGroup)
	for _, e := range arc.kgEntities {
		uid := e.UserID
		if groups[uid] == nil {
			groups[uid] = &userGroup{}
		}
		groups[uid].entities = append(groups[uid].entities, store.Entity{
			AgentID:     agentID,
			UserID:      uid,
			ExternalID:  e.ExternalID,
			Name:        e.Name,
			EntityType:  e.EntityType,
			Description: e.Description,
			Properties:  e.Properties,
			Confidence:  e.Confidence,
			ValidFrom:   e.ValidFrom,
			ValidUntil:  e.ValidUntil,
		})
	}
	for _, rel := range arc.kgRelations {
		uid := rel.UserID
		if groups[uid] == nil {
			groups[uid] = &userGroup{}
		}
		groups[uid].relations = append(groups[uid].relations, store.Relation{
			AgentID:        agentID,
			UserID:         uid,
			SourceEntityID: rel.SourceExternalID,
			TargetEntityID: rel.TargetExternalID,
			RelationType:   rel.RelationType,
			Confidence:     rel.Confidence,
			Properties:     rel.Properties,
			ValidFrom:      rel.ValidFrom,
			ValidUntil:     rel.ValidUntil,
		})
	}
	for uid, g := range groups {
		if _, err := h.kgStore.IngestExtraction(ctx, agentID, uid, g.entities, g.relations); err != nil {
			return fmt.Errorf("user %s: %w", uid, err)
		}
	}
	return nil
}

// memoryPathEntry is a lightweight reference for background re-indexing (avoids holding full arc).
type memoryPathEntry struct {
	userID string
	path   string
}

// collectMemoryPaths extracts just the paths from arc so the full archive can be GC'd.
func collectMemoryPaths(arc *importArchive) []memoryPathEntry {
	var paths []memoryPathEntry
	for _, doc := range arc.memoryGlobal {
		paths = append(paths, memoryPathEntry{path: doc.Path})
	}
	for uid, docs := range arc.memoryUsers {
		for _, doc := range docs {
			paths = append(paths, memoryPathEntry{userID: uid, path: doc.Path})
		}
	}
	return paths
}

// reindexMemoryPaths re-indexes imported memory documents in background.
func (h *AgentsHandler) reindexMemoryPaths(ctx context.Context, agentID string, paths []memoryPathEntry) {
	for _, p := range paths {
		if err := h.memoryStore.IndexDocument(ctx, agentID, p.userID, p.path); err != nil {
			slog.Warn("agents.import.reindex", "agent_id", agentID, "user_id", p.userID, "path", p.path, "error", err)
		}
	}
}

// buildAgentFromArchive constructs an AgentData from the parsed archive config map.
func (h *AgentsHandler) buildAgentFromArchive(cfg map[string]json.RawMessage, agentKey, displayName string, tenantID uuid.UUID, ownerID string) *store.AgentData {
	ag := &store.AgentData{
		AgentKey:    agentKey,
		DisplayName: displayName,
		TenantID:    tenantID,
		OwnerID:     ownerID,
		Status:      store.AgentStatusActive,
	}
	unmarshalField(cfg, "frontmatter", &ag.Frontmatter)
	unmarshalField(cfg, "provider", &ag.Provider)
	unmarshalField(cfg, "model", &ag.Model)
	unmarshalField(cfg, "agent_type", &ag.AgentType)
	unmarshalField(cfg, "context_window", &ag.ContextWindow)
	unmarshalField(cfg, "max_tool_iterations", &ag.MaxToolIterations)

	ag.ToolsConfig = rawOrNil(cfg["tools_config"])
	ag.SandboxConfig = rawOrNil(cfg["sandbox_config"])
	ag.SubagentsConfig = rawOrNil(cfg["subagents_config"])
	ag.MemoryConfig = rawOrNil(cfg["memory_config"])
	ag.CompactionConfig = rawOrNil(cfg["compaction_config"])
	ag.ContextPruning = rawOrNil(cfg["context_pruning"])
	ag.OtherConfig = rawOrNil(cfg["other_config"])

	// Promoted config fields — try top-level first, fall back to other_config for legacy exports
	unmarshalField(cfg, "emoji", &ag.Emoji)
	unmarshalField(cfg, "agent_description", &ag.AgentDescription)
	unmarshalField(cfg, "thinking_level", &ag.ThinkingLevel)
	unmarshalField(cfg, "max_tokens", &ag.MaxTokens)
	unmarshalField(cfg, "self_evolve", &ag.SelfEvolve)
	unmarshalField(cfg, "skill_evolve", &ag.SkillEvolve)
	unmarshalField(cfg, "skill_nudge_interval", &ag.SkillNudgeInterval)
	ag.ReasoningConfig = rawOrNil(cfg["reasoning_config"])
	ag.WorkspaceSharing = rawOrNil(cfg["workspace_sharing"])
	ag.ChatGPTOAuthRouting = rawOrNil(cfg["chatgpt_oauth_routing"])
	ag.ShellDenyGroups = rawOrNil(cfg["shell_deny_groups"])
	ag.KGDedupConfig = rawOrNil(cfg["kg_dedup_config"])

	// Backward compat: extract promoted fields from other_config for pre-migration exports
	if len(ag.OtherConfig) > 2 {
		var oc map[string]json.RawMessage
		if json.Unmarshal(ag.OtherConfig, &oc) == nil {
			extractLegacy := func(key string, dest *string) {
				if *dest == "" {
					if v, ok := oc[key]; ok {
						var s string
						if json.Unmarshal(v, &s) == nil {
							*dest = s
						}
					}
				}
			}
			extractLegacy("emoji", &ag.Emoji)
			extractLegacy("description", &ag.AgentDescription)
			extractLegacy("thinking_level", &ag.ThinkingLevel)
			if ag.ReasoningConfig == nil {
				ag.ReasoningConfig = rawOrNil(oc["reasoning"])
			}
			if ag.WorkspaceSharing == nil {
				ag.WorkspaceSharing = rawOrNil(oc["workspace_sharing"])
			}
			if ag.ChatGPTOAuthRouting == nil {
				ag.ChatGPTOAuthRouting = rawOrNil(oc["chatgpt_oauth_routing"])
			}
			if ag.ShellDenyGroups == nil {
				ag.ShellDenyGroups = rawOrNil(oc["shell_deny_groups"])
			}
			if ag.KGDedupConfig == nil {
				ag.KGDedupConfig = rawOrNil(oc["kg_dedup_config"])
			}
		}
	}

	if ag.AgentType == "" {
		ag.AgentType = store.AgentTypeOpen
	}
	if ag.ContextWindow <= 0 {
		ag.ContextWindow = config.DefaultContextWindow
	}
	if ag.MaxToolIterations <= 0 {
		ag.MaxToolIterations = config.DefaultMaxIterations
	}
	ag.Workspace = fmt.Sprintf("%s/%s", h.defaultWorkspace, ag.AgentKey)
	ag.RestrictToWorkspace = true

	if len(ag.CompactionConfig) == 0 {
		ag.CompactionConfig = json.RawMessage(`{}`)
	}
	if len(ag.MemoryConfig) == 0 {
		ag.MemoryConfig = json.RawMessage(`{"enabled":true}`)
	}
	return ag
}

// dedupAgentKey appends -2, -3, ... until the key is unique in the store.
func (h *AgentsHandler) dedupAgentKey(ctx context.Context, base string) string {
	key := base
	for i := 2; i <= 100; i++ {
		if existing, _ := h.agents.GetByKey(ctx, key); existing == nil {
			return key
		}
		key = fmt.Sprintf("%s-%d", base, i)
	}
	return key
}
