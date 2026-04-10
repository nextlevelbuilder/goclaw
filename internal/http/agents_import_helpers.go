package http

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// readTarGzEntries decompresses a tar.gz and returns all entries as a map.
// Enforces per-entry limit (maxImportBodySize) and cumulative decompressed size limit.
func readTarGzEntries(r io.Reader) (map[string][]byte, error) {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip open: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	entries := make(map[string][]byte)
	var totalBytes int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxImportBodySize))
		if err != nil {
			return nil, fmt.Errorf("read entry %s: %w", hdr.Name, err)
		}
		totalBytes += int64(len(data))
		if totalBytes > maxImportBodySize {
			return nil, fmt.Errorf("archive exceeds maximum decompressed size (%d bytes)", maxImportBodySize)
		}
		entries[hdr.Name] = data
	}
	return entries, nil
}

// readImportArchive extracts the archive into an importArchive struct.
func readImportArchive(r io.Reader) (*importArchive, error) {
	entries, err := readTarGzEntries(r)
	if err != nil {
		return nil, err
	}

	arc := &importArchive{
		memoryUsers:    make(map[string][]MemoryExport),
		workspaceFiles: make(map[string][]byte),
	}

	if raw, ok := entries["manifest.json"]; ok {
		var m ExportManifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse manifest: %w", err)
		}
		arc.manifest = &m
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
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				arc.userContextFiles = append(arc.userContextFiles, importUserContextFile{
					userID:   parts[0],
					fileName: parts[1],
					content:  string(data),
				})
			}
		case name == "memory/global.jsonl":
			docs, err := parseJSONL[MemoryExport](data)
			if err != nil {
				return nil, fmt.Errorf("parse memory/global.jsonl: %w", err)
			}
			arc.memoryGlobal = docs
		case strings.HasPrefix(name, "memory/users/") && strings.HasSuffix(name, ".jsonl"):
			uid := strings.TrimSuffix(strings.TrimPrefix(name, "memory/users/"), ".jsonl")
			docs, err := parseJSONL[MemoryExport](data)
			if err != nil {
				return nil, fmt.Errorf("parse memory/users/%s.jsonl: %w", uid, err)
			}
			arc.memoryUsers[uid] = docs
		case name == "knowledge_graph/entities.jsonl":
			entities, err := parseJSONL[KGEntityExport](data)
			if err != nil {
				return nil, fmt.Errorf("parse kg entities: %w", err)
			}
			arc.kgEntities = entities
		case name == "knowledge_graph/relations.jsonl":
			relations, err := parseJSONL[KGRelationExport](data)
			if err != nil {
				return nil, fmt.Errorf("parse kg relations: %w", err)
			}
			arc.kgRelations = relations
		case strings.HasPrefix(name, "workspace/"):
			rel := strings.TrimPrefix(name, "workspace/")
			if rel != "" {
				arc.workspaceFiles[rel] = data
			}
		}
	}

	if err := readNewSections(arc, entries); err != nil {
		return nil, err
	}

	return arc, nil
}

// parseJSONL decodes newline-delimited JSON into a slice of T.
func parseJSONL[T any](data []byte) ([]T, error) {
	var result []T
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1<<20), 10<<20)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, scanner.Err()
}

// countUserMemory returns the total number of per-user memory docs.
func countUserMemory(m map[string][]MemoryExport) int {
	n := 0
	for _, docs := range m {
		n += len(docs)
	}
	return n
}

// unmarshalField decodes a JSON raw value into dest if present.
func unmarshalField[T any](cfg map[string]json.RawMessage, key string, dest *T) {
	if raw, ok := cfg[key]; ok && len(raw) > 0 {
		json.Unmarshal(raw, dest) //nolint:errcheck
	}
}

// rawOrNil returns the raw message if non-empty, else nil.
func rawOrNil(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// importTenantID returns the tenant UUID from context, falling back to MasterTenantID.
func importTenantID(ctx context.Context) uuid.UUID {
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return store.MasterTenantID
	}
	return tid
}

// nullJSON returns nil if raw is empty (for JSONB nullable columns), otherwise returns raw.
func nullJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// coalesceJSON returns the raw message or '{}' if empty — for NOT NULL JSONB columns.
func coalesceJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

// nullStr converts a *string pointer to nil interface if the pointer is nil.
func nullStr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// nullStrVal converts an empty string to nil interface for nullable text columns.
// Non-empty strings are returned as-is.
func nullStrVal(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// readNewSections parses extended archive entries into the importArchive.
// Skills/MCP/permissions sections from old archives are silently skipped (backward compat).
func readNewSections(arc *importArchive, entries map[string][]byte) error {
	if data, ok := entries["evolution/metrics.jsonl"]; ok {
		items, err := parseJSONL[pg.EvolutionMetricExport](data)
		if err != nil {
			return fmt.Errorf("parse evolution/metrics.jsonl: %w", err)
		}
		arc.evolutionMetrics = items
	}
	if data, ok := entries["evolution/suggestions.jsonl"]; ok {
		items, err := parseJSONL[pg.EvolutionSuggestionExport](data)
		if err != nil {
			return fmt.Errorf("parse evolution/suggestions.jsonl: %w", err)
		}
		arc.evolutionSuggestions = items
	}

	if data, ok := entries["episodic/summaries.jsonl"]; ok {
		items, err := parseJSONL[pg.EpisodicSummaryExport](data)
		if err != nil {
			return fmt.Errorf("parse episodic/summaries.jsonl: %w", err)
		}
		arc.episodicSummaries = items
	}
	if data, ok := entries["cron/jobs.jsonl"]; ok {
		items, err := parseJSONL[pg.CronJobExport](data)
		if err != nil {
			return fmt.Errorf("parse cron/jobs.jsonl: %w", err)
		}
		arc.cronJobs = items
	}
	if data, ok := entries["user_profiles.jsonl"]; ok {
		items, err := parseJSONL[pg.UserProfileExport](data)
		if err != nil {
			return fmt.Errorf("parse user_profiles.jsonl: %w", err)
		}
		arc.userProfiles = items
	}
	if data, ok := entries["user_overrides.jsonl"]; ok {
		items, err := parseJSONL[pg.UserOverrideExport](data)
		if err != nil {
			return fmt.Errorf("parse user_overrides.jsonl: %w", err)
		}
		arc.userOverrides = items
	}

	// Phase 4: team section
	if data, ok := entries["team/team.json"]; ok {
		var t pg.TeamExport
		if err := json.Unmarshal(data, &t); err != nil {
			return fmt.Errorf("parse team/team.json: %w", err)
		}
		arc.teamMeta = &t
	}
	if data, ok := entries["team/members.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamMemberExport](data)
		if err != nil {
			return fmt.Errorf("parse team/members.jsonl: %w", err)
		}
		arc.teamMembers = items
	}
	if data, ok := entries["team/tasks.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskExport](data)
		if err != nil {
			return fmt.Errorf("parse team/tasks.jsonl: %w", err)
		}
		arc.teamTasks = items
	}
	if data, ok := entries["team/comments.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskCommentExport](data)
		if err != nil {
			return fmt.Errorf("parse team/comments.jsonl: %w", err)
		}
		arc.teamComments = items
	}
	if data, ok := entries["team/events.jsonl"]; ok {
		items, err := parseJSONL[pg.TeamTaskEventExport](data)
		if err != nil {
			return fmt.Errorf("parse team/events.jsonl: %w", err)
		}
		arc.teamEvents = items
	}
	if data, ok := entries["team/links.jsonl"]; ok {
		items, err := parseJSONL[pg.AgentLinkExport](data)
		if err != nil {
			return fmt.Errorf("parse team/links.jsonl: %w", err)
		}
		arc.teamLinks = items
	}

	// Team workspace files: "team/workspace/<rel>" → arc.teamWorkspace
	arc.teamWorkspace = make(map[string][]byte)
	for name, data := range entries {
		if rel := strings.TrimPrefix(name, "team/workspace/"); rel != name && rel != "" {
			arc.teamWorkspace[rel] = data
		}
	}

	// Vault section: Knowledge Vault documents + links
	if data, ok := entries["vault/documents.jsonl"]; ok {
		items, err := parseJSONL[pg.VaultDocumentExport](data)
		if err != nil {
			return fmt.Errorf("parse vault/documents.jsonl: %w", err)
		}
		arc.vaultDocuments = items
	}
	if data, ok := entries["vault/links.jsonl"]; ok {
		items, err := parseJSONL[pg.VaultLinkExport](data)
		if err != nil {
			return fmt.Errorf("parse vault/links.jsonl: %w", err)
		}
		arc.vaultLinks = items
	}

	return nil
}
