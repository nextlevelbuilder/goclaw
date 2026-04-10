package http

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bootstrap"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// RegenerateAgent updates context files based on an edit prompt.
// Reads existing files, sends them + edit instructions to LLM, stores results.
// Synchronous — caller should run in goroutine if needed.
func (s *AgentSummoner) RegenerateAgent(agentID uuid.UUID, tenantID uuid.UUID, providerName, model, editPrompt string) {
	ctx, cancel := context.WithTimeout(store.WithTenantID(context.Background(), tenantID), 300*time.Second)
	defer cancel()

	s.ensureBackfillFiles(ctx, agentID)

	s.emitEvent(agentID, tenantID, SummonEventStarted, "", "")

	// Read existing files for context
	existing, err := s.agents.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		slog.Warn("summoning: failed to read existing files", "agent", agentID, "error", err)
		s.emitEvent(agentID, tenantID, SummonEventFailed, "", err.Error())
		s.setAgentStatus(context.Background(), tenantID, agentID, store.AgentStatusSummonFailed)
		return
	}

	prompt := s.buildEditPrompt(existing, editPrompt)

	files, err := s.generateFiles(ctx, providerName, model, prompt)
	if err != nil {
		slog.Warn("summoning: regeneration failed", "agent", agentID, "error", err)
		s.emitEvent(agentID, tenantID, SummonEventFailed, "", err.Error())
		// Use fresh context — the original may have timed out, but we still need to update status.
		s.setAgentStatus(context.Background(), tenantID, agentID, store.AgentStatusSummonFailed)
		return
	}

	s.storeFiles(ctx, agentID, tenantID, files)

	// Update frontmatter + display_name if IDENTITY.md was regenerated
	updates := map[string]any{}
	if fm, ok := files[frontmatterKey]; ok && fm != "" {
		updates["frontmatter"] = fm
	}
	if name := extractIdentityName(files[bootstrap.IdentityFile]); name != "" {
		agent, _ := s.agents.GetByID(ctx, agentID)
		if agent != nil && agent.DisplayName != "" {
			// User already set a custom name — preserve it, sync IDENTITY.md to match
			if name != agent.DisplayName {
				updated := bootstrap.UpdateIdentityField(files[bootstrap.IdentityFile], "Name", agent.DisplayName)
				if updated != files[bootstrap.IdentityFile] {
					_ = s.agents.SetAgentContextFile(ctx, agentID, bootstrap.IdentityFile, updated)
				}
			}
		} else {
			// No custom name — use LLM-generated name
			updates["display_name"] = name
		}
	}
	if len(updates) > 0 {
		if err := s.agents.Update(ctx, agentID, updates); err != nil {
			slog.Warn("summoning: failed to save agent metadata", "agent", agentID, "error", err)
		}
	}

	s.setAgentStatus(ctx, tenantID, agentID, store.AgentStatusActive)
	s.emitEvent(agentID, tenantID, SummonEventCompleted, "", "")

	slog.Info("summoning: regeneration completed", "agent", agentID, "files", len(files))
}

// isRetryableError returns true for timeout and context-cancellation errors
// that warrant falling back to the 2-call approach.
func isRetryableError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

// isGenerated checks if a context file has been generated (differs from the default template).
func (s *AgentSummoner) isGenerated(existingMap map[string]string, fileName string) bool {
	content, ok := existingMap[fileName]
	if !ok || content == "" {
		return false
	}
	template, err := bootstrap.ReadTemplate(fileName)
	if err != nil {
		return false
	}
	return content != template
}

// generateFiles calls the LLM and parses the XML-tagged response into file map.
func (s *AgentSummoner) generateFiles(ctx context.Context, providerName, model, prompt string) (map[string]string, error) {
	provider, err := s.resolveProvider(ctx, providerName)
	if err != nil {
		return nil, fmt.Errorf("resolve provider: %w", err)
	}

	// Use a unique session key so CLI-based providers get an isolated workdir
	// (prevents polluting/reading CLAUDE.md from active chat sessions).
	summonSessionKey := "summon-" + uuid.New().String()

	slog.Info("summoning: calling LLM", "provider", providerName, "model", model, "prompt_len", len(prompt))

	resp, err := provider.Chat(ctx, providers.ChatRequest{
		Messages: []providers.Message{
			{Role: "system", Content: "You are a file generator. Output ONLY the requested XML-tagged files. No extra commentary."},
			{Role: "user", Content: prompt},
		},
		Model: model,
		Options: map[string]any{
			"max_tokens":              8192,
			"temperature":             0.7,
			providers.OptSessionKey:   summonSessionKey,
			providers.OptDisableTools: true,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", providerName, err)
	}

	slog.Info("summoning: raw LLM response", "provider", providerName, "length", len(resp.Content),
		"preview_start", truncateUTF8(resp.Content, 500),
		"preview_end", truncateUTF8(suffixString(resp.Content, 500), 500))

	files := parseFileResponse(resp.Content)
	if len(files) == 0 {
		return nil, fmt.Errorf("LLM returned no parseable files (response length: %d)", len(resp.Content))
	}

	return files, nil
}

// storeFiles saves generated files to agent_context_files and emits progress events.
func (s *AgentSummoner) storeFiles(ctx context.Context, agentID, tenantID uuid.UUID, files map[string]string) {
	for _, name := range summoningFiles {
		content, ok := files[name]
		if !ok || content == "" {
			continue
		}
		if err := s.agents.SetAgentContextFile(ctx, agentID, name, content); err != nil {
			slog.Warn("summoning: failed to store file", "agent", agentID, "file", name, "error", err)
			continue
		}
		s.emitEvent(agentID, tenantID, SummonEventFileGenerated, name, "")
	}
}

func (s *AgentSummoner) resolveProvider(ctx context.Context, name string) (providers.Provider, error) {
	if s.providerReg == nil {
		return nil, fmt.Errorf("no provider registry")
	}

	provider, err := s.providerReg.Get(ctx, name)
	if err != nil {
		// Fallback to first available provider
		names := s.providerReg.List(ctx)
		if len(names) == 0 {
			return nil, fmt.Errorf("no providers configured")
		}
		provider, err = s.providerReg.Get(ctx, names[0])
		if err != nil {
			return nil, err
		}
		slog.Warn("summoning: provider not found, using fallback", "wanted", name, "using", names[0])
	}
	return provider, nil
}

// ensureBackfillFiles seeds template files that may be missing for agents created
// before these features were introduced. Single DB query for all backfill checks.
func (s *AgentSummoner) ensureBackfillFiles(ctx context.Context, agentID uuid.UUID) {
	existing, err := s.agents.GetAgentContextFiles(ctx, agentID)
	if err != nil {
		return
	}
	has := make(map[string]bool, len(existing))
	for _, f := range existing {
		has[f.FileName] = true
	}
	backfill := []string{bootstrap.UserPredefinedFile, bootstrap.CapabilitiesFile}
	for _, name := range backfill {
		if has[name] {
			continue
		}
		tpl, err := bootstrap.ReadTemplate(name)
		if err != nil {
			continue
		}
		if err := s.agents.SetAgentContextFile(ctx, agentID, name, tpl); err != nil {
			slog.Warn("summoning: backfill file seed failed", "file", name, "agent", agentID, "error", err)
		}
	}
}

func (s *AgentSummoner) setAgentStatus(ctx context.Context, tenantID, agentID uuid.UUID, status string) {
	// Summoning frequently calls this after a timeout/cancel path with context.Background().
	// Re-attach tenant scope so AgentStore.Update targets the right tenant row.
	if store.TenantIDFromContext(ctx) == uuid.Nil && !store.IsCrossTenant(ctx) {
		if tenantID == uuid.Nil {
			tenantID = store.MasterTenantID
		}
		ctx = store.WithTenantID(ctx, tenantID)
	}
	if err := s.agents.Update(ctx, agentID, map[string]any{"status": status}); err != nil {
		slog.Warn("summoning: failed to update agent status", "agent", agentID, "status", status, "error", err)
	}
}

func (s *AgentSummoner) emitEvent(agentID, tenantID uuid.UUID, eventType, fileName, errMsg string) {
	if s.msgBus == nil {
		return
	}
	payload := map[string]any{
		"type":     eventType,
		"agent_id": agentID.String(),
	}
	if fileName != "" {
		payload["file"] = fileName
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	bus.BroadcastForTenant(s.msgBus, protocol.EventAgentSummoning, tenantID, payload)
}
