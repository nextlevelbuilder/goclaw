package http

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// doMergeImport upserts sections of the archive into the target agent.
func (h *AgentsHandler) doMergeImport(ctx context.Context, ag *store.AgentData, arc *importArchive, sections map[string]bool, progressFn func(ProgressEvent)) (*ImportSummary, error) {
	summary := &ImportSummary{AgentID: ag.ID.String(), AgentKey: ag.AgentKey}

	// Section: context_files
	if sections["context_files"] {
		if err := h.importContextFiles(ctx, ag, arc, summary, progressFn); err != nil {
			return nil, err
		}
	}

	// Section: memory
	if sections["memory"] && h.memoryStore != nil {
		if err := h.importMemory(ctx, ag, arc, summary, progressFn); err != nil {
			return nil, err
		}
	}

	// Section: knowledge_graph
	if sections["knowledge_graph"] && h.kgStore != nil && len(arc.kgEntities) > 0 {
		if err := h.importKG(ctx, ag, arc, summary, progressFn); err != nil {
			return nil, err
		}
	}

	// Section: cron — always imported as disabled
	if sections["cron"] && len(arc.cronJobs) > 0 {
		h.importCron(ctx, ag, arc, summary, progressFn)
	}

	// Section: user_profiles
	if sections["user_profiles"] && len(arc.userProfiles) > 0 {
		h.importUserProfiles(ctx, ag, arc, summary, progressFn)
	}

	// Section: user_overrides
	if sections["user_overrides"] && len(arc.userOverrides) > 0 {
		h.importUserOverrides(ctx, ag, arc, summary, progressFn)
	}

	// Section: workspace files
	if sections["workspace"] && len(arc.workspaceFiles) > 0 {
		h.importWorkspace(ctx, ag, arc, summary, progressFn)
	}

	// Section: team
	if sections["team"] && arc.teamMeta != nil {
		if err := h.importTeamSection(ctx, ag, arc, progressFn); err != nil {
			slog.Warn("import: team section failed", "agent_id", ag.ID, "error", err)
		} else {
			summary.TeamImported = true
		}
	}

	// Section: evolution metrics + suggestions
	if sections["evolution"] {
		h.importEvolution(ctx, ag, arc, summary, progressFn)
	}

	// Section: episodic summaries (Tier 2 memory) — PG only, nil-guarded
	if sections["episodic"] && h.episodicStore != nil && len(arc.episodicSummaries) > 0 {
		h.importEpisodic(ctx, ag, arc, summary, progressFn)
	}

	// Section: vault (Knowledge Vault documents + links)
	if sections["vault"] && h.vaultStore != nil && len(arc.vaultDocuments) > 0 {
		h.importVault(ctx, ag, arc, summary, progressFn)
	}

	return summary, nil
}

func (h *AgentsHandler) importContextFiles(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) error {
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "context_files", Status: "running", Total: len(arc.contextFiles)})
	}
	for _, cf := range arc.contextFiles {
		if err := h.agents.SetAgentContextFile(ctx, ag.ID, cf.fileName, cf.content); err != nil {
			return fmt.Errorf("set context file %s: %w", cf.fileName, err)
		}
		summary.ContextFiles++
	}
	for _, ucf := range arc.userContextFiles {
		if err := h.agents.SetUserContextFile(ctx, ag.ID, ucf.userID, ucf.fileName, ucf.content); err != nil {
			return fmt.Errorf("set user context file %s/%s: %w", ucf.userID, ucf.fileName, err)
		}
		summary.UserContextFiles++
	}
	if progressFn != nil {
		total := summary.ContextFiles + summary.UserContextFiles
		progressFn(ProgressEvent{Phase: "context_files", Status: "done", Current: total, Total: total})
	}
	return nil
}

func (h *AgentsHandler) importMemory(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) error {
	totalDocs := len(arc.memoryGlobal) + countUserMemory(arc.memoryUsers)
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "memory", Status: "running", Total: totalDocs})
	}
	for _, doc := range arc.memoryGlobal {
		if err := h.memoryStore.PutDocument(ctx, ag.ID.String(), "", doc.Path, doc.Content); err != nil {
			return fmt.Errorf("put memory doc %s: %w", doc.Path, err)
		}
		summary.MemoryDocs++
	}
	for uid, docs := range arc.memoryUsers {
		for _, doc := range docs {
			if err := h.memoryStore.PutDocument(ctx, ag.ID.String(), uid, doc.Path, doc.Content); err != nil {
				return fmt.Errorf("put memory doc %s/%s: %w", uid, doc.Path, err)
			}
			summary.MemoryDocs++
		}
	}
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "memory", Status: "done", Current: summary.MemoryDocs, Total: totalDocs})
	}
	// Async re-index — extract paths before goroutine to allow arc GC
	paths := collectMemoryPaths(arc)
	go h.reindexMemoryPaths(context.WithoutCancel(ctx), ag.ID.String(), paths)
	return nil
}

func (h *AgentsHandler) importKG(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) error {
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "knowledge_graph", Status: "running", Total: len(arc.kgEntities)})
	}
	if err := h.ingestKGByUser(ctx, ag.ID.String(), arc); err != nil {
		return fmt.Errorf("ingest kg: %w", err)
	}
	summary.KGEntities = len(arc.kgEntities)
	summary.KGRelations = len(arc.kgRelations)
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "knowledge_graph", Status: "done", Current: len(arc.kgEntities), Total: len(arc.kgEntities)})
	}
	return nil
}

func (h *AgentsHandler) importCron(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
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

func (h *AgentsHandler) importUserProfiles(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
	// workspace=NULL for portability (auto-created via GetOrCreateUserProfile on first user access)
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

func (h *AgentsHandler) importUserOverrides(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
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

func (h *AgentsHandler) importWorkspace(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
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

func (h *AgentsHandler) importEpisodic(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
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

func (h *AgentsHandler) importEvolution(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
	tid := importTenantID(ctx)

	// Metrics are time-series: re-import duplicates are acceptable for v1.
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

	// Suggestions: dedup by (agent_id, suggestion_type, suggestion) via SELECT EXISTS.
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

func (h *AgentsHandler) importVault(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, progressFn func(ProgressEvent)) {
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
		h.importVaultLinks(ctx, ag, arc, summary, tid, progressFn)
	}
}

func (h *AgentsHandler) importVaultLinks(ctx context.Context, ag *store.AgentData, arc *importArchive, summary *ImportSummary, tid uuid.UUID, progressFn func(ProgressEvent)) {
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
