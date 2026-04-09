package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// writeExportArchive builds a tar.gz archive into w, calling progressFn after each section.
// progressFn may be nil (direct mode).
func (h *AgentsHandler) writeExportArchive(ctx context.Context, w io.Writer, ag *store.AgentData, sections map[string]bool, progressFn func(ProgressEvent)) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	gw := gzip.NewWriter(lw)
	tw := tar.NewWriter(gw)

	manifest := &ExportManifest{
		Version:    1,
		Format:     "goclaw-agent-export",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		ExportedBy: store.UserIDFromContext(ctx),
		AgentKey:   ag.AgentKey,
		AgentID:    ag.ID.String(),
		Sections:   make(map[string]any),
	}

	// Section: config (always included)
	agentJSON, err := marshalAgentConfig(ag)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal agent config: %w", err)
	}
	if err := addToTar(tw, "agent.json", agentJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write agent.json: %w", err)
	}
	manifest.Sections["config"] = map[string]int{"count": 1}
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "config", Status: "done", Current: 1, Total: 1})
	}

	// Section: context_files (agent-level + per-user)
	if sections["context_files"] {
		files, err := pg.ExportAgentContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export context_files: %w", err)
		}
		for i, f := range files {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "context_files", Status: "running", Current: i + 1, Total: len(files)})
			}
			if err := addToTar(tw, "context_files/"+sanitizeName(f.FileName), []byte(f.Content)); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write context file %s: %w", f.FileName, err)
			}
		}
		manifest.Sections["context_files"] = map[string]int{"count": len(files)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "context_files", Status: "done", Current: len(files), Total: len(files), Detail: fmt.Sprintf("%d files", len(files))})
		}

		userFiles, err := pg.ExportUserContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export user_context_files: %w", err)
		}
		for i, f := range userFiles {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: "user_context_files", Status: "running", Current: i + 1, Total: len(userFiles)})
			}
			path := "user_context_files/" + sanitizeName(f.UserID) + "/" + sanitizeName(f.FileName)
			if err := addToTar(tw, path, []byte(f.Content)); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user context file %s: %w", f.FileName, err)
			}
		}
		manifest.Sections["user_context_files"] = map[string]int{"count": len(userFiles)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_context_files", Status: "done", Current: len(userFiles), Total: len(userFiles), Detail: fmt.Sprintf("%d files", len(userFiles))})
		}
	}

	// Section: memory
	if sections["memory"] {
		docs, err := pg.ExportMemoryDocuments(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export memory: %w", err)
		}

		globalDocs := make([]MemoryExport, 0)
		perUser := make(map[string][]MemoryExport)
		for _, d := range docs {
			me := MemoryExport{Path: d.Path, Content: d.Content, UserID: d.UserID}
			if d.UserID == "" {
				globalDocs = append(globalDocs, me)
			} else {
				perUser[d.UserID] = append(perUser[d.UserID], me)
			}
		}

		if len(globalDocs) > 0 {
			data, err := marshalJSONL(globalDocs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal global memory: %w", err)
			}
			if err := addToTar(tw, "memory/global.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write memory/global.jsonl: %w", err)
			}
		}
		for uid, udocs := range perUser {
			data, err := marshalJSONL(udocs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal memory for user %s: %w", uid, err)
			}
			if err := addToTar(tw, "memory/users/"+sanitizeName(uid)+".jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write memory/users/%s.jsonl: %w", uid, err)
			}
		}
		manifest.Sections["memory"] = map[string]int{
			"global":   len(globalDocs),
			"per_user": len(docs) - len(globalDocs),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "memory", Status: "done", Current: len(docs), Total: len(docs), Detail: fmt.Sprintf("%d docs", len(docs))})
		}
	}

	// Section: knowledge_graph
	if sections["knowledge_graph"] {
		entities, err := pg.ExportKGEntities(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export kg entities: %w", err)
		}

		// Build internal-id → external_id map for relation remapping
		idToExternal := make(map[string]string, len(entities))
		exportEntities := make([]KGEntityExport, 0, len(entities))
		for _, e := range entities {
			idToExternal[e.ID] = e.ExternalID
			exportEntities = append(exportEntities, KGEntityExport{
				ExternalID:  e.ExternalID,
				UserID:      e.UserID,
				Name:        e.Name,
				EntityType:  e.EntityType,
				Description: e.Description,
				Properties:  e.Properties,
				Confidence:  e.Confidence,
				ValidFrom:   e.ValidFrom,
				ValidUntil:  e.ValidUntil,
			})
		}

		if len(exportEntities) > 0 {
			data, err := marshalJSONL(exportEntities)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal kg entities: %w", err)
			}
			if err := addToTar(tw, "knowledge_graph/entities.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write kg entities: %w", err)
			}
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph_entities", Status: "done", Current: len(entities), Total: len(entities), Detail: fmt.Sprintf("%d entities", len(entities))})
		}

		relations, err := pg.ExportKGRelations(ctx, h.db, ag.ID)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("export kg relations: %w", err)
		}

		exportRelations := make([]KGRelationExport, 0, len(relations))
		for _, rel := range relations {
			exportRelations = append(exportRelations, KGRelationExport{
				SourceExternalID: idToExternal[rel.SourceEntityID],
				TargetExternalID: idToExternal[rel.TargetEntityID],
				UserID:           rel.UserID,
				RelationType:     rel.RelationType,
				Confidence:       rel.Confidence,
				Properties:       rel.Properties,
				ValidFrom:        rel.ValidFrom,
				ValidUntil:       rel.ValidUntil,
			})
		}

		if len(exportRelations) > 0 {
			data, err := marshalJSONL(exportRelations)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal kg relations: %w", err)
			}
			if err := addToTar(tw, "knowledge_graph/relations.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write kg relations: %w", err)
			}
		}
		manifest.Sections["knowledge_graph"] = map[string]int{
			"entities":  len(entities),
			"relations": len(relations),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "knowledge_graph_relations", Status: "done", Current: len(relations), Total: len(relations), Detail: fmt.Sprintf("%d relations", len(relations))})
		}
	}

	// Section: cron
	if sections["cron"] {
		jobs, qErr := pg.ExportCronJobs(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query cron jobs", "agent", ag.AgentKey, "error", qErr)
		}
		if len(jobs) > 0 {
			data, err := marshalJSONL(jobs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal cron jobs: %w", err)
			}
			if err := addToTar(tw, "cron/jobs.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write cron/jobs.jsonl: %w", err)
			}
		}
		manifest.Sections["cron"] = map[string]int{"count": len(jobs)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "cron", Status: "done", Detail: fmt.Sprintf("%d jobs", len(jobs))})
		}
	}

	// Section: user_profiles
	if sections["user_profiles"] {
		profiles, qErr := pg.ExportUserProfiles(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query user profiles", "agent", ag.AgentKey, "error", qErr)
		}
		if len(profiles) > 0 {
			data, err := marshalJSONL(profiles)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal user profiles: %w", err)
			}
			if err := addToTar(tw, "user_profiles.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user_profiles.jsonl: %w", err)
			}
		}
		manifest.Sections["user_profiles"] = map[string]int{"count": len(profiles)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_profiles", Status: "done", Detail: fmt.Sprintf("%d profiles", len(profiles))})
		}
	}

	// Section: user_overrides
	if sections["user_overrides"] {
		overrides, qErr := pg.ExportUserOverrides(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query user overrides", "agent", ag.AgentKey, "error", qErr)
		}
		if len(overrides) > 0 {
			data, err := marshalJSONL(overrides)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal user overrides: %w", err)
			}
			if err := addToTar(tw, "user_overrides.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write user_overrides.jsonl: %w", err)
			}
		}
		manifest.Sections["user_overrides"] = map[string]int{"count": len(overrides)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "user_overrides", Status: "done", Detail: fmt.Sprintf("%d overrides", len(overrides))})
		}
	}

	// Section: workspace files
	if sections["workspace"] && ag.Workspace != "" {
		wsPath := config.ExpandHome(ag.Workspace)
		fileCount, totalBytes, wsErr := h.exportWorkspaceFiles(ctx, tw, wsPath, progressFn)
		if wsErr != nil {
			slog.Warn("export: workspace walk failed", "path", wsPath, "error", wsErr)
		}
		manifest.Sections["workspace"] = map[string]any{"file_count": fileCount, "total_bytes": totalBytes}
	}

	// Section: episodic summaries (Tier 2 memory)
	if sections["episodic"] {
		summaries, qErr := pg.ExportEpisodicSummaries(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query episodic summaries", "agent", ag.AgentKey, "error", qErr)
		}
		if len(summaries) > 0 {
			data, err := marshalJSONL(summaries)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal episodic summaries: %w", err)
			}
			if err := addToTar(tw, "episodic/summaries.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write episodic/summaries.jsonl: %w", err)
			}
		}
		manifest.Sections["episodic"] = map[string]int{"count": len(summaries)}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "episodic", Status: "done", Detail: fmt.Sprintf("%d summaries", len(summaries))})
		}
	}

	// Section: evolution (metrics + suggestions, PG only — nil-guarded at import side)
	if sections["evolution"] {
		metrics, qErr := pg.ExportEvolutionMetrics(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query evolution metrics", "agent", ag.AgentKey, "error", qErr)
		}
		if len(metrics) > 0 {
			data, err := marshalJSONL(metrics)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal evolution metrics: %w", err)
			}
			if err := addToTar(tw, "evolution/metrics.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write evolution/metrics.jsonl: %w", err)
			}
		}

		suggestions, qErr := pg.ExportEvolutionSuggestions(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query evolution suggestions", "agent", ag.AgentKey, "error", qErr)
		}
		if len(suggestions) > 0 {
			data, err := marshalJSONL(suggestions)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal evolution suggestions: %w", err)
			}
			if err := addToTar(tw, "evolution/suggestions.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write evolution/suggestions.jsonl: %w", err)
			}
		}
		manifest.Sections["evolution"] = map[string]int{
			"metrics":     len(metrics),
			"suggestions": len(suggestions),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "evolution", Status: "done", Detail: fmt.Sprintf("%d metrics, %d suggestions", len(metrics), len(suggestions))})
		}
	}

	// Section: vault (Knowledge Vault documents + links)
	if sections["vault"] {
		vaultDocs, qErr := pg.ExportVaultDocuments(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query vault documents", "agent", ag.AgentKey, "error", qErr)
		}
		if len(vaultDocs) > 0 {
			data, err := marshalJSONL(vaultDocs)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal vault documents: %w", err)
			}
			if err := addToTar(tw, "vault/documents.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write vault/documents.jsonl: %w", err)
			}
		}

		vaultLinks, qErr := pg.ExportVaultLinks(ctx, h.db, ag.ID)
		if qErr != nil {
			slog.Warn("export: failed to query vault links", "agent", ag.AgentKey, "error", qErr)
		}
		if len(vaultLinks) > 0 {
			data, err := marshalJSONL(vaultLinks)
			if err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("marshal vault links: %w", err)
			}
			if err := addToTar(tw, "vault/links.jsonl", data); err != nil {
				tw.Close()
				gw.Close()
				return fmt.Errorf("write vault/links.jsonl: %w", err)
			}
		}
		manifest.Sections["vault"] = map[string]int{
			"documents": len(vaultDocs),
			"links":     len(vaultLinks),
		}
		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "vault", Status: "done", Detail: fmt.Sprintf("%d docs, %d links", len(vaultDocs), len(vaultLinks))})
		}
	}

	// Section: team
	if sections["team"] {
		if err := h.exportTeamSection(ctx, tw, ag.ID, manifest, progressFn); err != nil {
			slog.Warn("export: team section failed", "agent", ag.AgentKey, "error", err)
		}
	}

	// Manifest last — has accurate final counts
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addToTar(tw, "manifest.json", manifestJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write manifest: %w", err)
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return fmt.Errorf("close tar: %w", err)
	}
	return gw.Close()
}
