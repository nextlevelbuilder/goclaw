package http

import (
	"archive/tar"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func canonicalizeChatGPTOAuthRoutingForResponse(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	agent := &store.AgentData{ChatGPTOAuthRouting: raw}
	routing := store.PublicChatGPTOAuthRouting(agent.ParseChatGPTOAuthRouting())
	if routing == nil {
		return nil
	}
	out, err := json.Marshal(routing)
	if err != nil {
		return raw
	}
	return out
}

func canonicalizeProviderSettingsForResponse(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return raw
	}
	providerSettings := store.ParseChatGPTOAuthProviderSettings(raw)
	if providerSettings == nil || providerSettings.CodexPool == nil {
		delete(settings, "codex_pool")
	} else {
		routing := store.PublicChatGPTOAuthRouting(providerSettings.CodexPool)
		settings["codex_pool"] = map[string]any{
			"strategy":             routing.Strategy,
			"extra_provider_names": routing.ExtraProviderNames,
		}
	}
	if len(settings) == 0 {
		return nil
	}
	out, err := json.Marshal(settings)
	if err != nil {
		return raw
	}
	return out
}

func canonicalizeAgentForResponse(ag *store.AgentData) store.AgentData {
	clone := *ag
	clone.ChatGPTOAuthRouting = canonicalizeChatGPTOAuthRoutingForResponse(ag.ChatGPTOAuthRouting)
	return clone
}

func canonicalizeProviderForResponse(p *store.LLMProviderData) store.LLMProviderData {
	clone := *p
	clone.Settings = canonicalizeProviderSettingsForResponse(p.Settings)
	return clone
}

// addToTar adds a single file to the tar archive with a standard header.
func addToTar(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o644,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// marshalJSONL encodes items as newline-delimited JSON (one object per line).
func marshalJSONL[T any](items []T) ([]byte, error) {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return nil, err
		}
	}
	return []byte(sb.String()), nil
}

// marshalAgentConfig serializes an agent with sensitive fields (tenant_id, owner_id) stripped.
func marshalAgentConfig(ag *store.AgentData) ([]byte, error) {
	type exportableAgent struct {
		AgentKey          string          `json:"agent_key"`
		DisplayName       string          `json:"display_name,omitempty"`
		Frontmatter       string          `json:"frontmatter,omitempty"`
		Provider          string          `json:"provider"`
		Model             string          `json:"model"`
		ContextWindow     int             `json:"context_window"`
		MaxToolIterations int             `json:"max_tool_iterations"`
		AgentType         string          `json:"agent_type"`
		Status            string          `json:"status"`
		ToolsConfig       json.RawMessage `json:"tools_config,omitempty"`
		SandboxConfig     json.RawMessage `json:"sandbox_config,omitempty"`
		SubagentsConfig   json.RawMessage `json:"subagents_config,omitempty"`
		MemoryConfig      json.RawMessage `json:"memory_config,omitempty"`
		CompactionConfig  json.RawMessage `json:"compaction_config,omitempty"`
		ContextPruning    json.RawMessage `json:"context_pruning,omitempty"`
		OtherConfig       json.RawMessage `json:"other_config,omitempty"`
		// Promoted config fields
		Emoji               string          `json:"emoji,omitempty"`
		AgentDescription    string          `json:"agent_description,omitempty"`
		ThinkingLevel       string          `json:"thinking_level,omitempty"`
		MaxTokens           int             `json:"max_tokens,omitempty"`
		SelfEvolve          bool            `json:"self_evolve,omitempty"`
		SkillEvolve         bool            `json:"skill_evolve,omitempty"`
		SkillNudgeInterval  int             `json:"skill_nudge_interval,omitempty"`
		ReasoningConfig     json.RawMessage `json:"reasoning_config,omitempty"`
		WorkspaceSharing    json.RawMessage `json:"workspace_sharing,omitempty"`
		ChatGPTOAuthRouting json.RawMessage `json:"chatgpt_oauth_routing,omitempty"`
		ShellDenyGroups     json.RawMessage `json:"shell_deny_groups,omitempty"`
		KGDedupConfig       json.RawMessage `json:"kg_dedup_config,omitempty"`
	}
	return json.MarshalIndent(exportableAgent{
		AgentKey:            ag.AgentKey,
		DisplayName:         ag.DisplayName,
		Frontmatter:         ag.Frontmatter,
		Provider:            ag.Provider,
		Model:               ag.Model,
		ContextWindow:       ag.ContextWindow,
		MaxToolIterations:   ag.MaxToolIterations,
		AgentType:           ag.AgentType,
		Status:              ag.Status,
		ToolsConfig:         ag.ToolsConfig,
		SandboxConfig:       ag.SandboxConfig,
		SubagentsConfig:     ag.SubagentsConfig,
		MemoryConfig:        ag.MemoryConfig,
		CompactionConfig:    ag.CompactionConfig,
		ContextPruning:      ag.ContextPruning,
		OtherConfig:         ag.OtherConfig,
		Emoji:               ag.Emoji,
		AgentDescription:    ag.AgentDescription,
		ThinkingLevel:       ag.ThinkingLevel,
		MaxTokens:           ag.MaxTokens,
		SelfEvolve:          ag.SelfEvolve,
		SkillEvolve:         ag.SkillEvolve,
		SkillNudgeInterval:  ag.SkillNudgeInterval,
		ReasoningConfig:     ag.ReasoningConfig,
		WorkspaceSharing:    ag.WorkspaceSharing,
		ChatGPTOAuthRouting: canonicalizeChatGPTOAuthRoutingForResponse(ag.ChatGPTOAuthRouting),
		ShellDenyGroups:     ag.ShellDenyGroups,
		KGDedupConfig:       ag.KGDedupConfig,
	}, "", "  ")
}

// jsonIndent marshals v to indented JSON bytes.
func jsonIndent(v any) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// sanitizeName replaces characters that could cause path traversal in tar entries.
// Use for single-segment names (file names, agent keys). NOT for relative paths with slashes.
func sanitizeName(name string) string {
	r := strings.NewReplacer("/", "_", "..", "__", "\\", "_")
	return r.Replace(name)
}

// sanitizeRelPath sanitizes each segment of a relative path while preserving directory structure.
// Removes ".." traversal and backslashes but keeps forward slashes between segments.
func sanitizeRelPath(relPath string) string {
	parts := strings.Split(relPath, "/")
	clean := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		clean = append(clean, strings.ReplaceAll(p, "\\", "_"))
	}
	return strings.Join(clean, "/")
}

// limitedWriter wraps an io.Writer and returns an error once limit bytes are exceeded.
type limitedWriter struct {
	w       io.Writer
	written int64
	limit   int64
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written+int64(len(p)) > lw.limit {
		return 0, errors.New("export size limit exceeded")
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}
