package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/i18n"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *KnowledgeGraphHandler) handleListEntities(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")
	entityType := r.URL.Query().Get("type")
	query := r.URL.Query().Get("q")

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 {
		limit = 50
	}

	// If query is provided, use search
	if query != "" {
		entities, err := h.store.SearchEntities(r.Context(), agentID, userID, query, limit)
		if err != nil {
			slog.Warn("kg.search_entities failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if entities == nil {
			entities = []store.Entity{}
		}
		writeJSON(w, http.StatusOK, entities)
		return
	}

	entities, err := h.store.ListEntities(r.Context(), agentID, userID, store.EntityListOptions{
		EntityType: entityType,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		slog.Warn("kg.list_entities failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entities == nil {
		entities = []store.Entity{}
	}
	writeJSON(w, http.StatusOK, entities)
}

func (h *KnowledgeGraphHandler) handleGetEntity(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")
	entityID := r.PathValue("entityID")
	userID := r.URL.Query().Get("user_id")

	entity, err := h.store.GetEntity(r.Context(), agentID, userID, entityID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": i18n.T(locale, i18n.MsgNotFound, "entity", entityID)})
		return
	}

	relations, err := h.store.ListRelations(r.Context(), agentID, userID, entityID)
	if err != nil {
		relations = []store.Relation{}
	}
	if relations == nil {
		relations = []store.Relation{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entity":    entity,
		"relations": relations,
	})
}

func (h *KnowledgeGraphHandler) handleUpsertEntity(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var entity store.Entity
	if err := json.NewDecoder(r.Body).Decode(&entity); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	entity.AgentID = agentID

	if entity.ExternalID == "" || entity.Name == "" || entity.EntityType == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgEntityFieldsRequired)})
		return
	}
	if entity.Confidence <= 0 {
		entity.Confidence = 1.0
	}

	if err := h.store.UpsertEntity(r.Context(), &entity); err != nil {
		slog.Warn("kg.upsert_entity failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *KnowledgeGraphHandler) handleDeleteEntity(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	entityID := r.PathValue("entityID")
	userID := r.URL.Query().Get("user_id")

	if err := h.store.DeleteEntity(r.Context(), agentID, userID, entityID); err != nil {
		slog.Warn("kg.delete_entity failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *KnowledgeGraphHandler) handleTraverse(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		EntityID string `json:"entity_id"`
		UserID   string `json:"user_id"`
		MaxDepth int    `json:"max_depth"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.EntityID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgEntityIDRequired)})
		return
	}
	if body.MaxDepth <= 0 {
		body.MaxDepth = 2
	}
	if body.MaxDepth > 3 {
		body.MaxDepth = 3
	}

	results, err := h.store.Traverse(r.Context(), agentID, body.UserID, body.EntityID, body.MaxDepth)
	if err != nil {
		slog.Warn("kg.traverse failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if results == nil {
		results = []store.TraversalResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

func (h *KnowledgeGraphHandler) handleExtract(w http.ResponseWriter, r *http.Request) {
	locale := extractLocale(r)
	agentID := r.PathValue("agentID")

	var body struct {
		Text     string  `json:"text"`
		UserID   string  `json:"user_id"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		MinConf  float64 `json:"min_confidence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidJSON)})
		return
	}
	if body.Text == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgTextRequired)})
		return
	}
	if body.Provider == "" || body.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgProviderModelRequired)})
		return
	}

	extractor := h.NewExtractor(body.Provider, body.Model, body.MinConf)
	if extractor == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": i18n.T(locale, i18n.MsgInvalidProviderOrModel)})
		return
	}

	result, err := extractor.Extract(r.Context(), body.Text)
	if err != nil {
		slog.Warn("kg.extract failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Ingest extracted entities and relations into the store
	for i := range result.Entities {
		result.Entities[i].AgentID = agentID
		result.Entities[i].UserID = body.UserID
	}
	for i := range result.Relations {
		result.Relations[i].AgentID = agentID
		result.Relations[i].UserID = body.UserID
	}

	if err := h.store.IngestExtraction(r.Context(), agentID, body.UserID, result.Entities, result.Relations); err != nil {
		slog.Warn("kg.ingest_extraction failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entities":  len(result.Entities),
		"relations": len(result.Relations),
	})
}

func (h *KnowledgeGraphHandler) handleStats(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")

	stats, err := h.store.Stats(r.Context(), agentID, userID)
	if err != nil {
		slog.Warn("kg.stats failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleGraph returns all entities + relations for graph visualization.
func (h *KnowledgeGraphHandler) handleGraph(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agentID")
	userID := r.URL.Query().Get("user_id")

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 200
	}

	entities, err := h.store.ListEntities(r.Context(), agentID, userID, store.EntityListOptions{Limit: limit})
	if err != nil {
		slog.Warn("kg.graph entities failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entities == nil {
		entities = []store.Entity{}
	}

	relations, err := h.store.ListAllRelations(r.Context(), agentID, userID, limit*3)
	if err != nil {
		slog.Warn("kg.graph relations failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if relations == nil {
		relations = []store.Relation{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"entities":  entities,
		"relations": relations,
	})
}
