package party

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// EventEmitter sends party events to connected clients.
type EventEmitter func(event protocol.EventFrame)

// Engine orchestrates party mode discussions.
type Engine struct {
	partyStore store.PartyStore
	agentStore store.AgentStore
	provider   providers.Provider
}

// NewEngine creates a new party engine.
func NewEngine(partyStore store.PartyStore, agentStore store.AgentStore, provider providers.Provider) *Engine {
	return &Engine{
		partyStore: partyStore,
		agentStore: agentStore,
		provider:   provider,
	}
}

// LoadPersonas loads persona info from agent DB for the given keys.
func (e *Engine) LoadPersonas(ctx context.Context, keys []string) ([]PersonaInfo, error) {
	var personas []PersonaInfo
	for _, key := range keys {
		agent, err := e.agentStore.GetByKey(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("persona %s not found: %w", key, err)
		}
		pi := PersonaInfo{
			AgentKey:    key,
			DisplayName: agent.DisplayName,
		}
		// Extract persona metadata from other_config
		var cfg map[string]json.RawMessage
		if json.Unmarshal(agent.OtherConfig, &cfg) == nil {
			if personaJSON, ok := cfg["persona"]; ok {
				var pm struct {
					Emoji           string             `json:"emoji"`
					MovieRef        string             `json:"movie_ref"`
					SpeakingStyle   string             `json:"speaking_style"`
					ExpertiseWeight map[string]float64 `json:"expertise_weight"`
				}
				if json.Unmarshal(personaJSON, &pm) == nil {
					pi.Emoji = pm.Emoji
					pi.MovieRef = pm.MovieRef
					pi.SpeakingStyle = pm.SpeakingStyle
					pi.ExpertiseWeight = pm.ExpertiseWeight
				}
			}
		}
		if pi.Emoji == "" {
			pi.Emoji = "🤖"
		}
		personas = append(personas, pi)
	}
	return personas, nil
}

// RunStandardRound executes a standard mode round (1 LLM call, all personas).
func (e *Engine) RunStandardRound(ctx context.Context, session *store.PartySessionData, personas []PersonaInfo, emit EventEmitter) (*RoundResult, error) {
	slog.Info("party: standard round", "session", session.ID, "round", session.Round)

	systemPrompt := "You are a party mode facilitator. Generate responses for each persona in character."
	userPrompt := BuildStandardRoundPrompt(session, personas)

	resp, err := e.llmCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("standard round LLM call: %w", err)
	}

	messages := parsePersonaMessages(resp, personas)
	for _, m := range messages {
		emit(*protocol.NewEvent(protocol.EventPartyPersonaSpoke, map[string]any{
			"session_id": session.ID, "persona": m.PersonaKey,
			"emoji": m.Emoji, "content": m.Content,
		}))
	}

	return &RoundResult{Round: session.Round, Mode: store.PartyModeStandard, Messages: messages}, nil
}

// RunDeepRound executes Deep Mode: parallel thinking → cross-talk.
func (e *Engine) RunDeepRound(ctx context.Context, session *store.PartySessionData, personas []PersonaInfo, emit EventEmitter) (*RoundResult, error) {
	slog.Info("party: deep round (parallel)", "session", session.ID, "round", session.Round, "personas", len(personas))

	// Step 1: Parallel thinking
	thoughts, err := e.runParallelThinking(ctx, session, personas, emit)
	if err != nil {
		return nil, fmt.Errorf("parallel thinking: %w", err)
	}

	// Step 2: Cross-talk (1 LLM call)
	systemPrompt := "You are a party mode facilitator generating cross-talk between personas."
	userPrompt := BuildCrossTalkPrompt(session, personas, thoughts)

	resp, err := e.llmCall(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("cross-talk LLM call: %w", err)
	}

	messages := parsePersonaMessages(resp, personas)
	for i := range messages {
		// Attach thinking from Step 1
		for _, t := range thoughts {
			if t.PersonaKey == messages[i].PersonaKey {
				messages[i].Thinking = t.Content
				break
			}
		}
		emit(*protocol.NewEvent(protocol.EventPartyPersonaSpoke, map[string]any{
			"session_id": session.ID, "persona": messages[i].PersonaKey,
			"emoji": messages[i].Emoji, "content": messages[i].Content,
		}))
	}

	return &RoundResult{Round: session.Round, Mode: store.PartyModeDeep, Messages: messages}, nil
}

// RunTokenRingRound executes Token-Ring: parallel thinking → sequential turns.
func (e *Engine) RunTokenRingRound(ctx context.Context, session *store.PartySessionData, personas []PersonaInfo, emit EventEmitter) (*RoundResult, error) {
	slog.Info("party: token-ring round", "session", session.ID, "round", session.Round, "personas", len(personas))

	// Step 1: Parallel thinking
	thoughts, err := e.runParallelThinking(ctx, session, personas, emit)
	if err != nil {
		return nil, fmt.Errorf("parallel thinking: %w", err)
	}

	// Step 2: Sequential turns
	var messages []PersonaMessage
	var priorTurns []PersonaMessage

	for i, persona := range personas {
		isLast := i == len(personas)-1

		soulMD := e.loadPersonaSoulMD(ctx, persona.AgentKey)
		systemPrompt := BuildPersonaSystemPrompt(persona, session, soulMD)
		userPrompt := BuildTokenRingTurnPrompt(session, persona, thoughts, priorTurns, isLast)

		resp, err := e.llmCall(ctx, systemPrompt, userPrompt)
		if err != nil {
			slog.Warn("party: token-ring turn failed", "persona", persona.AgentKey, "error", err)
			continue
		}

		msg := PersonaMessage{
			PersonaKey:  persona.AgentKey,
			DisplayName: persona.DisplayName,
			Emoji:       persona.Emoji,
			Content:     strings.TrimSpace(resp),
		}
		messages = append(messages, msg)
		priorTurns = append(priorTurns, msg)

		// Emit immediately — user sees each persona respond in real-time
		emit(*protocol.NewEvent(protocol.EventPartyPersonaSpoke, map[string]any{
			"session_id": session.ID, "persona": msg.PersonaKey,
			"emoji": msg.Emoji, "content": msg.Content,
		}))
	}

	return &RoundResult{Round: session.Round, Mode: store.PartyModeTokenRing, Messages: messages}, nil
}

// runParallelThinking executes independent thinking for all personas in parallel.
func (e *Engine) runParallelThinking(ctx context.Context, session *store.PartySessionData, personas []PersonaInfo, emit EventEmitter) ([]PersonaThought, error) {
	thoughts := make([]PersonaThought, len(personas))
	errs := make([]error, len(personas))
	var wg sync.WaitGroup

	for i, persona := range personas {
		wg.Add(1)
		go func(idx int, p PersonaInfo) {
			defer wg.Done()

			emit(*protocol.NewEvent(protocol.EventPartyPersonaThinking, map[string]any{
				"session_id": session.ID, "persona": p.AgentKey, "emoji": p.Emoji,
			}))

			soulMD := e.loadPersonaSoulMD(ctx, p.AgentKey)
			systemPrompt := BuildPersonaSystemPrompt(p, session, soulMD)
			userPrompt := BuildThinkingPrompt(session, p)

			resp, err := e.llmCall(ctx, systemPrompt, userPrompt)
			if err != nil {
				errs[idx] = err
				return
			}
			thoughts[idx] = PersonaThought{PersonaKey: p.AgentKey, Emoji: p.Emoji, Content: strings.TrimSpace(resp)}
		}(i, persona)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			return nil, fmt.Errorf("persona %s thinking failed: %w", personas[i].AgentKey, err)
		}
	}

	return thoughts, nil
}

// GenerateSummary generates a discussion summary.
func (e *Engine) GenerateSummary(ctx context.Context, session *store.PartySessionData) (*SummaryResult, error) {
	prompt := BuildSummaryPrompt(session)
	resp, err := e.llmCall(ctx, "You are a discussion summarizer. Generate structured markdown summaries.", prompt)
	if err != nil {
		return nil, fmt.Errorf("summary LLM call: %w", err)
	}

	var personaKeys []string
	json.Unmarshal(session.Personas, &personaKeys)

	return &SummaryResult{
		Topic:    session.Topic,
		Rounds:   session.Round,
		Personas: personaKeys,
		Markdown: resp,
	}, nil
}

func (e *Engine) llmCall(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	req := providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Options: map[string]any{
			"max_tokens": 4096,
		},
	}
	resp, err := e.provider.Chat(ctx, req)
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func (e *Engine) loadPersonaSoulMD(ctx context.Context, agentKey string) string {
	agent, err := e.agentStore.GetByKey(ctx, agentKey)
	if err != nil {
		return ""
	}
	files, _ := e.agentStore.GetAgentContextFiles(ctx, agent.ID)
	for _, f := range files {
		if f.FileName == "SOUL.md" {
			return f.Content
		}
	}
	return ""
}

// parsePersonaMessages parses LLM output into individual persona messages.
func parsePersonaMessages(resp string, personas []PersonaInfo) []PersonaMessage {
	var messages []PersonaMessage
	lines := strings.Split(resp, "\n")

	var current *PersonaMessage
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		matched := false
		for _, p := range personas {
			if strings.HasPrefix(line, p.Emoji) {
				if current != nil {
					messages = append(messages, *current)
				}
				content := line
				prefix := p.Emoji + " " + p.DisplayName + ":"
				if strings.HasPrefix(line, prefix) {
					content = strings.TrimSpace(line[len(prefix):])
				}
				current = &PersonaMessage{
					PersonaKey:  p.AgentKey,
					DisplayName: p.DisplayName,
					Emoji:       p.Emoji,
					Content:     content,
				}
				matched = true
				break
			}
		}
		if !matched && current != nil {
			current.Content += "\n" + line
		}
	}
	if current != nil {
		messages = append(messages, *current)
	}
	return messages
}
