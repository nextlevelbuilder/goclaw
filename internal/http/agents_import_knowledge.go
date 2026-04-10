package http

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

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
