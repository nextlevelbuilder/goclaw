package http

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamExportManifest describes the contents of a team export archive.
type TeamExportManifest struct {
	Version    int            `json:"version"`
	Format     string         `json:"format"`
	ExportedAt string         `json:"exported_at"`
	ExportedBy string         `json:"exported_by"`
	TeamName   string         `json:"team_name"`
	TeamID     string         `json:"team_id"`
	AgentKeys  []string       `json:"agent_keys"`
	Sections   map[string]any `json:"sections"`
}

// handleTeamExportPreview returns team export counts without building the archive.
func (h *AgentsHandler) handleTeamExportPreview(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	// Auth already enforced by adminMiddleware on the route.

	teamIDStr := r.PathValue("id")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "team"))
		return
	}

	teamMeta, err := pg.ExportTeamByID(r.Context(), h.db, teamID)
	if err != nil || teamMeta == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "team", teamIDStr))
		return
	}

	tasks, members, links, _ := pg.ExportTeamPreviewCountsByID(r.Context(), h.db, teamID)

	agentMembers, err := pg.GetTeamMemberAgents(r.Context(), h.db, teamID)
	if err != nil {
		slog.Warn("team.export.preview: get members failed", "team_id", teamID, "error", err)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"team_name":   teamMeta.Name,
		"team_id":     teamIDStr,
		"tasks":       tasks,
		"members":     members,
		"agent_links": links,
		"agent_count": len(agentMembers),
	})
}

// handleTeamExport builds a team export archive and streams or SSE-wraps it.
func (h *AgentsHandler) handleTeamExport(w http.ResponseWriter, r *http.Request) {
	userID := store.UserIDFromContext(r.Context())
	locale := store.LocaleFromContext(r.Context())
	// Auth already enforced by adminMiddleware on the route.

	teamIDStr := r.PathValue("id")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, protocol.ErrInvalidRequest, i18n.T(locale, i18n.MsgInvalidID, "team"))
		return
	}

	teamMeta, err := pg.ExportTeamByID(r.Context(), h.db, teamID)
	if err != nil || teamMeta == nil {
		writeError(w, http.StatusNotFound, protocol.ErrNotFound, i18n.T(locale, i18n.MsgNotFound, "team", teamIDStr))
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	fileName := fmt.Sprintf("team-%s-%s.tar.gz",
		sanitizeName(teamMeta.Name),
		time.Now().UTC().Format("20060102"),
	)

	if stream {
		flusher := initSSE(w)
		if flusher == nil {
			writeError(w, http.StatusInternalServerError, protocol.ErrInternal, "streaming not supported")
			return
		}

		tmpFile, err := os.CreateTemp("", "goclaw-team-export-*.tar.gz")
		if err != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "init", Status: "error", Detail: "failed to create temp file"})
			return
		}
		tmpPath := tmpFile.Name()

		progressFn := func(ev ProgressEvent) { sendSSE(w, flusher, "progress", ev) }
		buildErr := h.writeTeamExportArchive(r.Context(), tmpFile, teamID, teamMeta, progressFn)
		tmpFile.Close()

		if buildErr != nil {
			slog.Error("team.export.sse", "team_id", teamID, "error", buildErr)
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "archive", Status: "error", Detail: buildErr.Error()})
			os.Remove(tmpPath)
			return
		}

		token := h.generateExportToken(teamID.String(), userID, tmpPath, fileName)
		sendSSE(w, flusher, "complete", map[string]string{
			"download_url": "/v1/export/download/" + token,
		})
		return
	}

	// Direct streaming response
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	if err := h.writeTeamExportArchive(r.Context(), w, teamID, teamMeta, nil); err != nil {
		slog.Error("team.export.direct", "team_id", teamID, "error", err)
	}
}

// writeTeamExportArchive builds the team tar.gz archive: team/ metadata + agents/{key}/ per member.
func (h *AgentsHandler) writeTeamExportArchive(ctx context.Context, w io.Writer, teamID uuid.UUID, teamMeta *pg.TeamExport, progressFn func(ProgressEvent)) error {
	lw := &limitedWriter{w: w, limit: maxExportSize}
	gw := gzip.NewWriter(lw)
	tw := tar.NewWriter(gw)

	manifest := &TeamExportManifest{
		Version:    1,
		Format:     "goclaw-team-export",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		ExportedBy: store.UserIDFromContext(ctx),
		TeamName:   teamMeta.Name,
		TeamID:     teamID.String(),
		AgentKeys:  []string{},
		Sections:   make(map[string]any),
	}

	// team/team.json
	teamJSON, err := jsonIndent(teamMeta)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal team: %w", err)
	}
	if err := addToTar(tw, "team/team.json", teamJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write team/team.json: %w", err)
	}

	// team/members.jsonl
	allMembers, err := pg.ExportTeamMembersAll(ctx, h.db, teamID)
	if err != nil {
		slog.Warn("team.export: members query failed", "team_id", teamID, "error", err)
	}
	if len(allMembers) > 0 {
		data, err := marshalJSONL(allMembers)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal members: %w", err)
		}
		if err := addToTar(tw, "team/members.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write team/members.jsonl: %w", err)
		}
	}

	// team/tasks.jsonl, team/comments.jsonl, team/events.jsonl
	tasksExport, err := pg.ExportTeamTasks(ctx, h.db, teamID)
	if err != nil {
		slog.Warn("team.export: tasks query failed", "team_id", teamID, "error", err)
		tasksExport = &pg.TeamTasksExport{}
	}
	if len(tasksExport.Tasks) > 0 {
		data, err := marshalJSONL(tasksExport.Tasks)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal tasks: %w", err)
		}
		if err := addToTar(tw, "team/tasks.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write team/tasks.jsonl: %w", err)
		}
	}

	comments, _ := pg.ExportTeamComments(ctx, h.db, teamID, tasksExport.TaskUIDs)
	if len(comments) > 0 {
		data, err := marshalJSONL(comments)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal comments: %w", err)
		}
		if err := addToTar(tw, "team/comments.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write team/comments.jsonl: %w", err)
		}
	}

	events, _ := pg.ExportTeamEvents(ctx, h.db, teamID, tasksExport.TaskUIDs)
	if len(events) > 0 {
		data, err := marshalJSONL(events)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal events: %w", err)
		}
		if err := addToTar(tw, "team/events.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write team/events.jsonl: %w", err)
		}
	}

	// team/links.jsonl
	links, _ := pg.ExportTeamLinksForTeam(ctx, h.db, teamID)
	if len(links) > 0 {
		data, err := marshalJSONL(links)
		if err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("marshal links: %w", err)
		}
		if err := addToTar(tw, "team/links.jsonl", data); err != nil {
			tw.Close()
			gw.Close()
			return fmt.Errorf("write team/links.jsonl: %w", err)
		}
	}

	// team/workspace/
	if h.dataDir != "" {
		wsPath := filepath.Join(config.ExpandHome(h.dataDir), "teams", teamID.String())
		h.exportTeamWorkspaceFiles(ctx, tw, wsPath, progressFn) //nolint:errcheck
	}

	manifest.Sections["team"] = map[string]any{
		"members":  len(allMembers),
		"tasks":    len(tasksExport.Tasks),
		"comments": len(comments),
		"events":   len(events),
		"links":    len(links),
	}
	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "team", Status: "done", Current: len(tasksExport.Tasks), Total: len(tasksExport.Tasks)})
	}

	// agents/{key}/ — export each member agent's data
	agentMembers, err := pg.GetTeamMemberAgents(ctx, h.db, teamID)
	if err != nil {
		slog.Warn("team.export: get member agents failed", "team_id", teamID, "error", err)
	}

	sections := map[string]bool{
		"context_files":   true,
		"memory":          true,
		"knowledge_graph": true,
		"cron":            true,
		"user_profiles":   true,
		"user_overrides":  true,
		"workspace":       true,
	}

	for _, member := range agentMembers {
		manifest.AgentKeys = append(manifest.AgentKeys, member.AgentKey)
		prefix := "agents/" + sanitizeName(member.AgentKey) + "/"

		// agent.json
		agentJSON, err := pg.ExportTeamAgentJSON(ctx, h.db, member.ID)
		if err != nil {
			slog.Warn("team.export: agent json failed", "agent_id", member.ID, "error", err)
			continue
		}
		if err := addToTar(tw, prefix+"agent.json", agentJSON); err != nil {
			slog.Warn("team.export: write agent.json failed", "key", member.AgentKey, "error", err)
			continue
		}

		// Write all agent sections into archive under prefix
		agData, _ := h.agents.GetByID(ctx, member.ID)
		var counts agentSectionCounts
		if agData != nil {
			if progressFn != nil {
				progressFn(ProgressEvent{Phase: member.AgentKey, Status: "running"})
			}
			var secErr error
			counts, secErr = h.writeAgentSectionsToTar(ctx, tw, agData, prefix, sections)
			if secErr != nil {
				slog.Warn("team.export: agent sections failed", "key", member.AgentKey, "error", secErr)
			}
		}

		if progressFn != nil {
			progressFn(ProgressEvent{Phase: member.AgentKey, Status: "done", Detail: counts.summary()})
		}
	}

	manifest.Sections["agents"] = map[string]any{"count": len(agentMembers)}

	// manifest.json last
	manifestJSON, err := jsonIndent(manifest)
	if err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("marshal manifest: %w", err)
	}
	if err := addToTar(tw, "manifest.json", manifestJSON); err != nil {
		tw.Close()
		gw.Close()
		return fmt.Errorf("write manifest.json: %w", err)
	}

	if err := tw.Close(); err != nil {
		gw.Close()
		return fmt.Errorf("close tar: %w", err)
	}
	return gw.Close()
}

// agentSectionCounts tracks how many items were exported per section for progress reporting.
type agentSectionCounts struct {
	contextFiles int
	memory       int
	kgEntities   int
	kgRelations  int
	cronJobs     int
	profiles     int
	overrides    int
	wsFiles      int
}

func (c agentSectionCounts) summary() string {
	var parts []string
	if c.contextFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d context files", c.contextFiles))
	}
	if c.memory > 0 {
		parts = append(parts, fmt.Sprintf("%d memory docs", c.memory))
	}
	if c.kgEntities > 0 || c.kgRelations > 0 {
		parts = append(parts, fmt.Sprintf("%d entities · %d relations", c.kgEntities, c.kgRelations))
	}
	if c.cronJobs > 0 {
		parts = append(parts, fmt.Sprintf("%d cron jobs", c.cronJobs))
	}
	if c.profiles > 0 {
		parts = append(parts, fmt.Sprintf("%d user profiles", c.profiles))
	}
	if c.overrides > 0 {
		parts = append(parts, fmt.Sprintf("%d overrides", c.overrides))
	}
	if c.wsFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d workspace files", c.wsFiles))
	}
	if len(parts) == 0 {
		return "config only"
	}
	return strings.Join(parts, " · ")
}

// writeAgentSectionsToTar writes agent data sections into an existing tar.Writer with the given prefix.
// This allows team export to embed per-agent data under agents/{key}/ without creating nested archives.
func (h *AgentsHandler) writeAgentSectionsToTar(ctx context.Context, tw *tar.Writer, ag *store.AgentData, prefix string, sections map[string]bool) (agentSectionCounts, error) {
	var c agentSectionCounts

	// context_files (agent-level + per-user)
	if sections["context_files"] {
		files, err := pg.ExportAgentContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.context_files", "agent_id", ag.ID, "error", err)
		}
		for _, f := range files {
			path := prefix + "context_files/" + sanitizeName(f.FileName)
			if err := addToTar(tw, path, []byte(f.Content)); err != nil {
				return c, fmt.Errorf("write %s: %w", path, err)
			}
		}
		c.contextFiles += len(files)
		userFiles, err := pg.ExportUserContextFiles(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.user_context_files", "agent_id", ag.ID, "error", err)
		}
		for _, f := range userFiles {
			path := prefix + "user_context_files/" + sanitizeName(f.UserID) + "/" + sanitizeName(f.FileName)
			if err := addToTar(tw, path, []byte(f.Content)); err != nil {
				return c, fmt.Errorf("write %s: %w", path, err)
			}
		}
		c.contextFiles += len(userFiles)
	}

	// memory (global + per-user)
	if sections["memory"] {
		docs, err := pg.ExportMemoryDocuments(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.memory", "agent_id", ag.ID, "error", err)
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
			data, _ := marshalJSONL(globalDocs)
			addToTar(tw, prefix+"memory/global.jsonl", data) //nolint:errcheck
		}
		for uid, udocs := range perUser {
			data, _ := marshalJSONL(udocs)
			addToTar(tw, prefix+"memory/users/"+sanitizeName(uid)+".jsonl", data) //nolint:errcheck
		}
		c.memory = len(docs)
	}

	// knowledge_graph (entities + relations)
	if sections["knowledge_graph"] {
		entities, err := pg.ExportKGEntities(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.kg_entities", "agent_id", ag.ID, "error", err)
		}
		idToExternal := make(map[string]string, len(entities))
		exportEntities := make([]KGEntityExport, 0, len(entities))
		for _, e := range entities {
			idToExternal[e.ID] = e.ExternalID
			exportEntities = append(exportEntities, KGEntityExport{
				ExternalID: e.ExternalID, UserID: e.UserID, Name: e.Name,
				EntityType: e.EntityType, Description: e.Description,
				Properties: e.Properties, Confidence: e.Confidence,
			})
		}
		if len(exportEntities) > 0 {
			data, _ := marshalJSONL(exportEntities)
			addToTar(tw, prefix+"knowledge_graph/entities.jsonl", data) //nolint:errcheck
		}
		c.kgEntities = len(entities)
		relations, err := pg.ExportKGRelations(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.kg_relations", "agent_id", ag.ID, "error", err)
		}
		exportRelations := make([]KGRelationExport, 0, len(relations))
		for _, rel := range relations {
			exportRelations = append(exportRelations, KGRelationExport{
				SourceExternalID: idToExternal[rel.SourceEntityID],
				TargetExternalID: idToExternal[rel.TargetEntityID],
				UserID:           rel.UserID, RelationType: rel.RelationType,
				Confidence: rel.Confidence, Properties: rel.Properties,
			})
		}
		if len(exportRelations) > 0 {
			data, _ := marshalJSONL(exportRelations)
			addToTar(tw, prefix+"knowledge_graph/relations.jsonl", data) //nolint:errcheck
		}
		c.kgRelations = len(relations)
	}

	// cron
	if sections["cron"] {
		jobs, err := pg.ExportCronJobs(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.cron", "agent_id", ag.ID, "error", err)
		}
		if len(jobs) > 0 {
			data, _ := marshalJSONL(jobs)
			addToTar(tw, prefix+"cron/jobs.jsonl", data) //nolint:errcheck
		}
		c.cronJobs = len(jobs)
	}

	// user_profiles
	if sections["user_profiles"] {
		profiles, err := pg.ExportUserProfiles(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.user_profiles", "agent_id", ag.ID, "error", err)
		}
		if len(profiles) > 0 {
			data, _ := marshalJSONL(profiles)
			addToTar(tw, prefix+"user_profiles.jsonl", data) //nolint:errcheck
		}
		c.profiles = len(profiles)
	}

	// user_overrides
	if sections["user_overrides"] {
		overrides, err := pg.ExportUserOverrides(ctx, h.db, ag.ID)
		if err != nil {
			slog.Warn("team.export.agent.user_overrides", "agent_id", ag.ID, "error", err)
		}
		if len(overrides) > 0 {
			data, _ := marshalJSONL(overrides)
			addToTar(tw, prefix+"user_overrides.jsonl", data) //nolint:errcheck
		}
		c.overrides = len(overrides)
	}

	// workspace
	if sections["workspace"] && ag.Workspace != "" {
		wsPath := config.ExpandHome(ag.Workspace)
		wsCount, _, _ := h.exportWorkspaceFilesWithPrefix(ctx, tw, wsPath, prefix+"workspace/", nil)
		c.wsFiles = wsCount
	}

	return c, nil
}
