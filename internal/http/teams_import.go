package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// teamImportArchive holds parsed contents of a team export archive.
type teamImportArchive struct {
	manifest      *TeamExportManifest
	teamMeta      *pg.TeamExport
	teamMembers   []pg.TeamMemberExport
	teamTasks     []pg.TeamTaskExport
	teamComments  []pg.TeamTaskCommentExport
	teamEvents    []pg.TeamTaskEventExport
	teamLinks     []pg.AgentLinkExport
	teamWorkspace map[string][]byte // relative path → content
	// agentArcs maps agent_key → importArchive for each member agent in the team archive
	agentArcs map[string]*importArchive
}

// handleTeamImport imports a team archive (POST /v1/teams/import).
func (h *AgentsHandler) handleTeamImport(w http.ResponseWriter, r *http.Request) {
	locale := store.LocaleFromContext(r.Context())
	// Auth already enforced by adminMiddleware on the route.

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

	teamArc, err := readTeamImportArchive(f)
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
		summary, importErr := h.doTeamImport(r.Context(), r, teamArc, progressFn)
		if importErr != nil {
			sendSSE(w, flusher, "error", ProgressEvent{Phase: "import", Status: "error", Detail: importErr.Error()})
			return
		}
		sendSSE(w, flusher, "complete", summary)
		return
	}

	summary, err := h.doTeamImport(r.Context(), r, teamArc, nil)
	if err != nil {
		slog.Error("team.import", "error", err)
		writeError(w, http.StatusInternalServerError, protocol.ErrInternal, i18n.T(locale, i18n.MsgInternalError, err.Error()))
		return
	}
	writeJSON(w, http.StatusCreated, summary)
}

// readTeamImportArchive parses a team tar.gz, separating team/ entries from agents/{key}/ entries.
func readTeamImportArchive(r io.Reader) (*teamImportArchive, error) {
	entries, err := readTarGzEntries(r)
	if err != nil {
		return nil, err
	}

	arc := &teamImportArchive{
		agentArcs: make(map[string]*importArchive),
	}

	// Parse manifest
	if raw, ok := entries["manifest.json"]; ok {
		var m TeamExportManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		arc.manifest = &m
	}

	// Parse team section
	if raw, ok := entries["team/team.json"]; ok {
		var t pg.TeamExport
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, fmt.Errorf("parse team/team.json: %w", err)
		}
		arc.teamMeta = &t
	}
	if arc.teamMeta == nil {
		return nil, fmt.Errorf("archive missing team/team.json")
	}

	if raw, ok := entries["team/members.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamMemberExport](raw)
		if err != nil {
			return nil, fmt.Errorf("parse team/members.jsonl: %w", err)
		}
		arc.teamMembers = items
	}

	if raw, ok := entries["team/tasks.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskExport](raw)
		if err != nil {
			slog.Warn("team.import: parse team/tasks.jsonl", "error", err)
		} else {
			arc.teamTasks = items
		}
	}

	if raw, ok := entries["team/comments.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskCommentExport](raw)
		if err != nil {
			slog.Warn("team.import: parse team/comments.jsonl", "error", err)
		} else {
			arc.teamComments = items
		}
	}

	if raw, ok := entries["team/events.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskEventExport](raw)
		if err != nil {
			slog.Warn("team.import: parse team/events.jsonl", "error", err)
		} else {
			arc.teamEvents = items
		}
	}

	if raw, ok := entries["team/links.jsonl"]; ok {
		items, err := parseJSONL[pg.AgentLinkExport](raw)
		if err != nil {
			slog.Warn("team.import: parse team/links.jsonl", "error", err)
		} else {
			arc.teamLinks = items
		}
	}

	// team/workspace/* files
	arc.teamWorkspace = make(map[string][]byte)
	for name, data := range entries {
		if !strings.HasPrefix(name, "team/workspace/") {
			continue
		}
		rel := strings.TrimPrefix(name, "team/workspace/")
		if rel != "" {
			arc.teamWorkspace[rel] = data
		}
	}

	// Group agents/{key}/* entries by agent key
	agentEntries := make(map[string]map[string][]byte)
	for name, data := range entries {
		if !strings.HasPrefix(name, "agents/") {
			continue
		}
		rest := strings.TrimPrefix(name, "agents/")
		before, after, ok := strings.Cut(rest, "/")
		if !ok {
			continue
		}
		agentKey := before
		relPath := after
		if agentKey == "" || relPath == "" {
			continue
		}
		if agentEntries[agentKey] == nil {
			agentEntries[agentKey] = make(map[string][]byte)
		}
		agentEntries[agentKey][relPath] = data
	}

	// Build per-agent importArchive from grouped entries
	for agentKey, agentEnts := range agentEntries {
		agArc, err := buildAgentArcFromEntries(agentEnts)
		if err != nil {
			slog.Warn("team.import: failed to parse agent archive", "key", agentKey, "error", err)
			continue
		}
		arc.agentArcs[agentKey] = agArc
	}

	// Also build importArchive stubs for members listed in team/members.jsonl
	// but not present as agents/ entries (graceful degradation)
	for _, m := range arc.teamMembers {
		if _, ok := arc.agentArcs[m.AgentKey]; !ok {
			arc.agentArcs[m.AgentKey] = &importArchive{
				memoryUsers:    make(map[string][]MemoryExport),
				workspaceFiles: make(map[string][]byte),
				teamWorkspace:  make(map[string][]byte),
			}
		}
	}

	return arc, nil
}

// buildAgentArcFromEntries constructs an importArchive from a flat entry map (relative paths within agents/{key}/).
func buildAgentArcFromEntries(entries map[string][]byte) (*importArchive, error) {
	arc := &importArchive{
		memoryUsers:    make(map[string][]MemoryExport),
		workspaceFiles: make(map[string][]byte),
		teamWorkspace:  make(map[string][]byte),
	}

	if raw, ok := entries["agent.json"]; ok {
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("parse agent.json: %w", err)
		}
		arc.agentConfig = cfg
	}

	for name, data := range entries {
		switch {
		case strings.HasPrefix(name, "context_files/"):
			fileName := strings.TrimPrefix(name, "context_files/")
			if fileName != "" {
				arc.contextFiles = append(arc.contextFiles, importContextFile{
					fileName: fileName,
					content:  string(data),
				})
			}
		case strings.HasPrefix(name, "user_context_files/"):
			rest := strings.TrimPrefix(name, "user_context_files/")
			if idx := strings.Index(rest, "/"); idx > 0 {
				userID := rest[:idx]
				fileName := rest[idx+1:]
				if fileName != "" {
					arc.userContextFiles = append(arc.userContextFiles, importUserContextFile{
						userID:   userID,
						fileName: fileName,
						content:  string(data),
					})
				}
			}
		case name == "memory/global.jsonl":
			items, err := parseJSONL[MemoryExport](data)
			if err == nil {
				arc.memoryGlobal = items
			}
		case strings.HasPrefix(name, "memory/users/") && strings.HasSuffix(name, ".jsonl"):
			items, err := parseJSONL[MemoryExport](data)
			if err == nil {
				uid := strings.TrimSuffix(strings.TrimPrefix(name, "memory/users/"), ".jsonl")
				arc.memoryUsers[uid] = items
			}
		case name == "knowledge_graph/entities.jsonl":
			items, err := parseJSONL[KGEntityExport](data)
			if err == nil {
				arc.kgEntities = items
			}
		case name == "knowledge_graph/relations.jsonl":
			items, err := parseJSONL[KGRelationExport](data)
			if err == nil {
				arc.kgRelations = items
			}
		case name == "cron/jobs.jsonl":
			items, err := parseJSONL[pg.CronJobExport](data)
			if err == nil {
				arc.cronJobs = items
			}
		case name == "user_profiles.jsonl":
			items, err := parseJSONL[pg.UserProfileExport](data)
			if err == nil {
				arc.userProfiles = items
			}
		case name == "user_overrides.jsonl":
			items, err := parseJSONL[pg.UserOverrideExport](data)
			if err == nil {
				arc.userOverrides = items
			}
		case strings.HasPrefix(name, "workspace/"):
			rel := strings.TrimPrefix(name, "workspace/")
			if rel != "" {
				arc.workspaceFiles[rel] = data
			}
		}
	}

	return arc, nil
}

// TeamImportSummary is returned after a successful team import.
type TeamImportSummary struct {
	TeamName    string   `json:"team_name"`
	AgentsAdded int      `json:"agents_added"`
	AgentKeys   []string `json:"agent_keys"`
}

// doTeamImport imports each agent from the team archive then wires up the team structure.
func (h *AgentsHandler) doTeamImport(ctx context.Context, r *http.Request, teamArc *teamImportArchive, progressFn func(ProgressEvent)) (*TeamImportSummary, error) {
	summary := &TeamImportSummary{
		TeamName:  teamArc.teamMeta.Name,
		AgentKeys: []string{},
	}

	// Import order: use AgentKeys from manifest if present, else iterate agentArcs
	var agentKeys []string
	if teamArc.manifest != nil {
		agentKeys = teamArc.manifest.AgentKeys
	}
	if len(agentKeys) == 0 {
		for k := range teamArc.agentArcs {
			agentKeys = append(agentKeys, k)
		}
	}

	// First pass: import/find each agent
	importedAgents := make(map[string]string) // agent_key → imported agent_key (may be deduplicated)

	for _, agentKey := range agentKeys {
		agArc, ok := teamArc.agentArcs[agentKey]
		if !ok {
			continue
		}
		// Ensure agentConfig is populated (may be empty for member-only stubs)
		if agArc.agentConfig == nil {
			agArc.agentConfig = make(map[string]json.RawMessage)
		}
		// Inject agent_key from manifest if not in agentConfig
		if agArc.agentConfig["agent_key"] == nil {
			raw, _ := json.Marshal(agentKey)
			agArc.agentConfig["agent_key"] = raw
		}

		dedupedKey := h.dedupAgentKey(ctx, agentKey)
		tenantID := store.TenantIDFromContext(ctx)
		userID := store.UserIDFromContext(ctx)
		ag := h.buildAgentFromArchive(agArc.agentConfig, dedupedKey, "", tenantID, userID)

		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "agent", Status: "running", Detail: dedupedKey})
		}

		if err := h.agents.Create(ctx, ag); err != nil {
			slog.Warn("team.import: create agent failed", "key", dedupedKey, "error", err)
			continue
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
		if _, err := h.doMergeImport(ctx, ag, agArc, sections, progressFn); err != nil {
			slog.Warn("team.import: merge agent data failed", "key", dedupedKey, "error", err)
		}

		importedAgents[agentKey] = dedupedKey
		summary.AgentsAdded++
		summary.AgentKeys = append(summary.AgentKeys, dedupedKey)

		if progressFn != nil {
			progressFn(ProgressEvent{Phase: "agent", Status: "done", Detail: dedupedKey})
		}
	}

	if len(importedAgents) == 0 {
		return nil, fmt.Errorf("no agents could be imported from archive")
	}

	// Second pass: import the team itself, using the first imported agent as lead.
	// Build a synthetic importArchive with team data for importTeamSection.
	leadKey := agentKeys[0]
	leadImportedKey, ok := importedAgents[leadKey]
	if !ok {
		// Fallback: pick any imported agent
		for _, k := range importedAgents {
			leadImportedKey = k
			break
		}
	}

	leadAgent, err := h.agents.GetByKey(ctx, leadImportedKey)
	if err != nil || leadAgent == nil {
		slog.Warn("team.import: lead agent not found after import", "key", leadImportedKey)
		return summary, nil
	}

	// Build a synthetic importArchive containing team data
	syntheticArc := &importArchive{
		memoryUsers:    make(map[string][]MemoryExport),
		workspaceFiles: make(map[string][]byte),
		teamWorkspace:  teamArc.teamWorkspace,
		teamMeta:       teamArc.teamMeta,
		teamMembers:    teamArc.teamMembers,
		teamTasks:      teamArc.teamTasks,
		teamComments:   teamArc.teamComments,
		teamEvents:     teamArc.teamEvents,
		teamLinks:      teamArc.teamLinks,
	}

	if err := h.importTeamSection(ctx, leadAgent, syntheticArc, progressFn); err != nil {
		slog.Warn("team.import: team section failed", "error", err)
	}

	if progressFn != nil {
		progressFn(ProgressEvent{Phase: "team", Status: "done", Detail: teamArc.teamMeta.Name})
	}

	return summary, nil
}
