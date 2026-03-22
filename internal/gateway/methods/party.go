package methods

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/party"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// PartyMethods handles party.* WebSocket RPC methods.
type PartyMethods struct {
	partyStore   store.PartyStore
	agentStore   store.AgentStore
	providerReg  *providers.Registry
	msgBus       *bus.MessageBus
}

// NewPartyMethods creates a new PartyMethods handler.
func NewPartyMethods(partyStore store.PartyStore, agentStore store.AgentStore, providerReg *providers.Registry, msgBus *bus.MessageBus) *PartyMethods {
	return &PartyMethods{
		partyStore:  partyStore,
		agentStore:  agentStore,
		providerReg: providerReg,
		msgBus:      msgBus,
	}
}

// Register registers all party.* methods on the router.
func (m *PartyMethods) Register(router *gateway.MethodRouter) {
	router.Register(protocol.MethodPartyStart, m.handleStart)
	router.Register(protocol.MethodPartyRound, m.handleRound)
	router.Register(protocol.MethodPartyQuestion, m.handleQuestion)
	router.Register(protocol.MethodPartyAddContext, m.handleAddContext)
	router.Register(protocol.MethodPartySummary, m.handleSummary)
	router.Register(protocol.MethodPartyExit, m.handleExit)
	router.Register(protocol.MethodPartyList, m.handleList)
}

// getEngine returns a party engine using the best available provider.
// Prefers providers with a non-empty DefaultModel (DB providers with settings),
// falling back to the first name alphabetically for deterministic selection.
func (m *PartyMethods) getEngine() (*party.Engine, error) {
	names := m.providerReg.List()
	if len(names) == 0 {
		return nil, fmt.Errorf("no LLM providers available")
	}

	// Prefer a provider with DefaultModel set (typically DB providers with settings.default_model)
	var bestName string
	for _, name := range names {
		p, err := m.providerReg.Get(name)
		if err != nil {
			continue
		}
		if p.DefaultModel() != "" {
			if bestName == "" || name < bestName {
				bestName = name
			}
		}
	}
	// Fallback: pick first alphabetically for determinism
	if bestName == "" {
		sort.Strings(names)
		bestName = names[0]
	}

	provider, err := m.providerReg.Get(bestName)
	if err != nil {
		return nil, fmt.Errorf("provider %s: %w", bestName, err)
	}
	return party.NewEngine(m.partyStore, m.agentStore, provider), nil
}

// emitterForClient creates an EventEmitter that broadcasts to all connected WS clients.
func (m *PartyMethods) emitterForClient(client *gateway.Client) party.EventEmitter {
	return func(event protocol.EventFrame) {
		client.SendEvent(event)
	}
}

type partyStartParams struct {
	Topic       string   `json:"topic"`
	TeamPreset  string   `json:"team_preset,omitempty"`
	Personas    []string `json:"personas,omitempty"`
	ContextURLs []string `json:"context_urls,omitempty"`
}

func (m *PartyMethods) handleStart(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params partyStartParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	if params.Topic == "" {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "topic is required"))
		return
	}

	// Resolve personas from preset or custom list
	personaKeys := params.Personas
	if params.TeamPreset != "" {
		for _, preset := range party.PresetTeams() {
			if preset.Key == params.TeamPreset {
				personaKeys = preset.Personas
				break
			}
		}
	}
	if len(personaKeys) == 0 {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "no personas selected"))
		return
	}

	engine, err := m.getEngine()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Load persona info from DB
	personas, err := engine.LoadPersonas(ctx, personaKeys)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Marshal persona keys for storage
	personasJSON, _ := json.Marshal(personaKeys)

	// Create session
	sess := &store.PartySessionData{
		Topic:      params.Topic,
		TeamPreset: params.TeamPreset,
		Status:     store.PartyStatusDiscussing,
		Mode:       store.PartyModeStandard,
		MaxRounds:  10,
		UserID:     client.UserID(),
		Personas:   personasJSON,
	}
	if err := m.partyStore.CreateSession(ctx, sess); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	slog.Info("party: session started", "session_id", sess.ID, "topic", params.Topic, "personas", len(personas))

	// Emit started event
	emit := m.emitterForClient(client)
	personaInfos := make([]map[string]string, len(personas))
	for i, p := range personas {
		personaInfos[i] = map[string]string{
			"agent_key":    p.AgentKey,
			"display_name": p.DisplayName,
			"emoji":        p.Emoji,
			"movie_ref":    p.MovieRef,
		}
	}
	emit(*protocol.NewEvent(protocol.EventPartyStarted, map[string]any{
		"session_id": sess.ID,
		"topic":      params.Topic,
		"personas":   personaInfos,
	}))

	// Generate introductions for each persona
	for _, p := range personas {
		intro := fmt.Sprintf("%s %s reporting in. Ready to discuss: %s", p.Emoji, p.DisplayName, params.Topic)
		emit(*protocol.NewEvent(protocol.EventPartyPersonaIntro, map[string]any{
			"session_id": sess.ID,
			"persona":    p.AgentKey,
			"emoji":      p.Emoji,
			"content":    intro,
		}))
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"session_id": sess.ID,
		"personas":   personaInfos,
		"status":     sess.Status,
	}))
}

type partyRoundParams struct {
	SessionID string `json:"session_id"`
	Mode      string `json:"mode,omitempty"`
}

func (m *PartyMethods) handleRound(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params partyRoundParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	sessID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid session_id"))
		return
	}

	sess, err := m.partyStore.GetSession(ctx, sessID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "session not found"))
		return
	}

	if sess.Status != store.PartyStatusDiscussing {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "session is not in discussing state"))
		return
	}

	// Increment round
	sess.Round++
	mode := params.Mode
	if mode == "" {
		mode = sess.Mode
	}

	engine, err := m.getEngine()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Load personas
	var personaKeys []string
	json.Unmarshal(sess.Personas, &personaKeys)
	personas, err := engine.LoadPersonas(ctx, personaKeys)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emit := m.emitterForClient(client)
	emit(*protocol.NewEvent(protocol.EventPartyRoundStarted, map[string]any{
		"session_id": sess.ID,
		"round":      sess.Round,
		"mode":       mode,
	}))

	// Run the round
	var result *party.RoundResult
	switch mode {
	case store.PartyModeDeep:
		result, err = engine.RunDeepRound(ctx, sess, personas, emit)
	case store.PartyModeTokenRing:
		result, err = engine.RunTokenRingRound(ctx, sess, personas, emit)
	default:
		result, err = engine.RunStandardRound(ctx, sess, personas, emit)
	}
	if err != nil {
		slog.Error("party: round failed", "session", sess.ID, "round", sess.Round, "mode", mode, "error", err)
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Append to history
	var history []party.RoundResult
	json.Unmarshal(sess.History, &history)
	history = append(history, *result)
	historyJSON, _ := json.Marshal(history)

	// Update session
	if err := m.partyStore.UpdateSession(ctx, sess.ID, map[string]any{
		"round":   sess.Round,
		"mode":    mode,
		"history": historyJSON,
	}); err != nil {
		slog.Warn("party: failed to update session", "error", err)
	}

	emit(*protocol.NewEvent(protocol.EventPartyRoundComplete, map[string]any{
		"session_id": sess.ID,
		"round":      sess.Round,
		"mode":       mode,
	}))

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"round":    sess.Round,
		"mode":     mode,
		"messages": result.Messages,
	}))
}

type partyQuestionParams struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

func (m *PartyMethods) handleQuestion(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params partyQuestionParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	sessID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid session_id"))
		return
	}

	sess, err := m.partyStore.GetSession(ctx, sessID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "session not found"))
		return
	}

	// Temporarily set topic to the question for this round
	originalTopic := sess.Topic
	sess.Topic = fmt.Sprintf("%s\n\nUser question: %s", originalTopic, params.Text)
	sess.Round++

	engine, err := m.getEngine()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	var personaKeys []string
	json.Unmarshal(sess.Personas, &personaKeys)
	personas, err := engine.LoadPersonas(ctx, personaKeys)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emit := m.emitterForClient(client)
	result, err := engine.RunStandardRound(ctx, sess, personas, emit)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Restore topic and update session
	sess.Topic = originalTopic
	var history []party.RoundResult
	json.Unmarshal(sess.History, &history)
	history = append(history, *result)
	historyJSON, _ := json.Marshal(history)

	if err := m.partyStore.UpdateSession(ctx, sess.ID, map[string]any{
		"round":   sess.Round,
		"history": historyJSON,
	}); err != nil {
		slog.Warn("party: failed to update session", "error", err)
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"round":    sess.Round,
		"messages": result.Messages,
	}))
}

type partyAddContextParams struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Name      string `json:"name,omitempty"`
	Content   string `json:"content,omitempty"`
	URL       string `json:"url,omitempty"`
}

func (m *PartyMethods) handleAddContext(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params partyAddContextParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	sessID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid session_id"))
		return
	}

	sess, err := m.partyStore.GetSession(ctx, sessID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "session not found"))
		return
	}

	// Parse existing context
	var sessionCtx map[string]any
	if json.Unmarshal(sess.Context, &sessionCtx) != nil {
		sessionCtx = make(map[string]any)
	}

	// Add new context based on type
	switch params.Type {
	case "document":
		docs, _ := sessionCtx["documents"].([]any)
		docs = append(docs, map[string]string{"name": params.Name, "content": params.Content, "source": "upload"})
		sessionCtx["documents"] = docs
	case "meeting_notes":
		sessionCtx["meeting_notes"] = params.Content
	case "custom":
		sessionCtx["custom"] = params.Content
	default:
		sessionCtx[params.Type] = params.Content
	}

	contextJSON, _ := json.Marshal(sessionCtx)
	if err := m.partyStore.UpdateSession(ctx, sess.ID, map[string]any{
		"context": contextJSON,
	}); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	emit := m.emitterForClient(client)
	emit(*protocol.NewEvent(protocol.EventPartyContextAdded, map[string]any{
		"session_id": sess.ID,
		"name":       params.Name,
		"type":       params.Type,
	}))

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"ok":            true,
		"context_count": len(sessionCtx),
	}))
}

func (m *PartyMethods) handleSummary(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	sessID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid session_id"))
		return
	}

	sess, err := m.partyStore.GetSession(ctx, sessID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "session not found"))
		return
	}

	engine, err := m.getEngine()
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	summary, err := engine.GenerateSummary(ctx, sess)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	// Store summary in session
	summaryJSON, _ := json.Marshal(summary)
	m.partyStore.UpdateSession(ctx, sess.ID, map[string]any{
		"summary": summaryJSON,
		"status":  store.PartyStatusSummarizing,
	})

	emit := m.emitterForClient(client)
	emit(*protocol.NewEvent(protocol.EventPartySummaryReady, map[string]any{
		"session_id": sess.ID,
		"summary":    summary,
	}))

	client.SendResponse(protocol.NewOKResponse(req.ID, summary))
}

func (m *PartyMethods) handleExit(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		SessionID string `json:"session_id"`
		FollowUp  string `json:"follow_up,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	sessID, err := uuid.Parse(params.SessionID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid session_id"))
		return
	}

	sess, err := m.partyStore.GetSession(ctx, sessID)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, "session not found"))
		return
	}

	// Generate summary if not yet done
	var summary *party.SummaryResult
	if sess.Summary == nil || string(sess.Summary) == "null" {
		engine, err := m.getEngine()
		if err == nil {
			summary, _ = engine.GenerateSummary(ctx, sess)
		}
	} else {
		json.Unmarshal(sess.Summary, &summary)
	}

	// Close session
	updates := map[string]any{"status": store.PartyStatusClosed}
	if summary != nil {
		summaryJSON, _ := json.Marshal(summary)
		updates["summary"] = summaryJSON
	}
	m.partyStore.UpdateSession(ctx, sess.ID, updates)

	emit := m.emitterForClient(client)
	emit(*protocol.NewEvent(protocol.EventPartyClosed, map[string]any{
		"session_id": sess.ID,
	}))

	response := map[string]any{
		"session_id": sess.ID,
		"status":     store.PartyStatusClosed,
	}
	if summary != nil {
		response["summary"] = summary
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, response))
}

func (m *PartyMethods) handleList(ctx context.Context, client *gateway.Client, req *protocol.RequestFrame) {
	var params struct {
		Status string `json:"status,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInvalidRequest, "invalid params"))
		return
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 20
	}

	sessions, err := m.partyStore.ListSessions(ctx, client.UserID(), params.Status, limit)
	if err != nil {
		client.SendResponse(protocol.NewErrorResponse(req.ID, protocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(protocol.NewOKResponse(req.ID, map[string]any{
		"sessions": sessions,
		"count":    len(sessions),
	}))
}
