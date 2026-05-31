package channels

import (
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/config"
)

const (
	defaultQuickAckDelayMs = 1000
	defaultFinalSplitMin   = 1200
	defaultFinalSplitMax   = 3
	defaultFinalSplitDelay = 500
	defaultAckTemplate     = "Got it. Working on it..."

	QuickAckModeLLMGenerated  = "llm_generated"
	QuickAckModeFixedTemplate = "fixed_template"
	QuickAckModeOff           = "off"

	QuickAckSourceGenerated = "generated"
	QuickAckSourceTemplate  = "template"
	QuickAckSourceOff       = "off"
)

type ResolvedChatBehavior struct {
	Enabled    bool
	QuickAck   ResolvedQuickAckConfig
	FinalSplit ResolvedFinalSplitConfig
}

type ResolvedQuickAckConfig struct {
	Enabled    bool
	Mode       string
	MinDelayMs int
	Templates  []string
}

type ResolvedFinalSplitConfig struct {
	Enabled     bool
	MinChars    int
	MaxMessages int
	DelayMs     int
}

type ChatBehaviorPreviewOptions struct {
	Content      string
	IsStreaming  bool
	HasToolCalls bool
}

type ChatBehaviorPreview struct {
	Resolved ResolvedChatBehavior `json:"resolved"`
	Ack      AckPreview           `json:"ack"`
	Split    SplitPreview         `json:"split"`
}

type AckPreview struct {
	ShouldSend      bool   `json:"shouldSend"`
	Content         string `json:"content,omitempty"`
	FallbackContent string `json:"fallbackContent,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Source          string `json:"source,omitempty"`
}

type SplitPreview struct {
	Parts []string `json:"parts"`
}

func ResolveChatBehavior(global, override *config.ChatBehaviorConfig) ResolvedChatBehavior {
	resolved := ResolvedChatBehavior{
		QuickAck: ResolvedQuickAckConfig{
			Mode:       QuickAckModeLLMGenerated,
			MinDelayMs: defaultQuickAckDelayMs,
			Templates:  []string{defaultAckTemplate},
		},
		FinalSplit: ResolvedFinalSplitConfig{
			MinChars:    defaultFinalSplitMin,
			MaxMessages: defaultFinalSplitMax,
			DelayMs:     defaultFinalSplitDelay,
		},
	}
	applyChatBehavior(&resolved, global)
	applyChatBehavior(&resolved, override)
	if !resolved.Enabled {
		resolved.QuickAck.Enabled = false
		resolved.FinalSplit.Enabled = false
	}
	if resolved.FinalSplit.MaxMessages < 1 {
		resolved.FinalSplit.MaxMessages = 1
	}
	return resolved
}

func applyChatBehavior(dst *ResolvedChatBehavior, src *config.ChatBehaviorConfig) {
	if src == nil {
		return
	}
	if src.Enabled != nil {
		dst.Enabled = *src.Enabled
	}
	if src.QuickAck != nil {
		if src.QuickAck.Enabled != nil {
			dst.QuickAck.Enabled = *src.QuickAck.Enabled
		}
		if src.QuickAck.Mode != nil {
			dst.QuickAck.Mode = normalizeQuickAckMode(*src.QuickAck.Mode)
		}
		if src.QuickAck.MinDelayMs != nil {
			dst.QuickAck.MinDelayMs = max(0, *src.QuickAck.MinDelayMs)
		}
		if len(src.QuickAck.Templates) > 0 {
			dst.QuickAck.Templates = cleanTemplates(src.QuickAck.Templates)
		}
	}
	if src.FinalSplit != nil {
		if src.FinalSplit.Enabled != nil {
			dst.FinalSplit.Enabled = *src.FinalSplit.Enabled
		}
		if src.FinalSplit.MinChars != nil {
			dst.FinalSplit.MinChars = max(0, *src.FinalSplit.MinChars)
		}
		if src.FinalSplit.MaxMessages != nil {
			dst.FinalSplit.MaxMessages = max(1, *src.FinalSplit.MaxMessages)
		}
		if src.FinalSplit.DelayMs != nil {
			dst.FinalSplit.DelayMs = max(0, *src.FinalSplit.DelayMs)
		}
	}
}

func cleanTemplates(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{defaultAckTemplate}
	}
	return out
}

func PreviewChatBehavior(global, override *config.ChatBehaviorConfig, opts ChatBehaviorPreviewOptions) ChatBehaviorPreview {
	resolved := ResolveChatBehavior(global, override)
	return PreviewResolvedChatBehavior(resolved, opts)
}

func PreviewResolvedChatBehavior(resolved ResolvedChatBehavior, opts ChatBehaviorPreviewOptions) ChatBehaviorPreview {
	preview := ChatBehaviorPreview{
		Resolved: resolved,
		Split:    SplitPreview{Parts: SplitFinalMessages(opts.Content, resolved.FinalSplit)},
	}
	preview.Ack = buildAckPreview(resolved, opts.IsStreaming, opts.HasToolCalls)
	return preview
}

func ShouldSendQuickAck(behavior ResolvedChatBehavior, streaming bool) bool {
	if !behavior.Enabled || !behavior.QuickAck.Enabled || streaming {
		return false
	}
	switch effectiveQuickAckMode(behavior.QuickAck.Mode) {
	case QuickAckModeLLMGenerated, QuickAckModeFixedTemplate:
		return true
	default:
		return false
	}
}

func ShouldDeliverGeneratedProgress(behavior ResolvedChatBehavior, streaming bool) bool {
	return behavior.Enabled &&
		behavior.QuickAck.Enabled &&
		!streaming &&
		effectiveQuickAckMode(behavior.QuickAck.Mode) == QuickAckModeLLMGenerated
}

func ShouldSuppressInitialBlockReply(behavior ResolvedChatBehavior, streaming bool) bool {
	if streaming || !behavior.Enabled {
		return false
	}
	return !behavior.QuickAck.Enabled || effectiveQuickAckMode(behavior.QuickAck.Mode) == QuickAckModeOff
}

func normalizeQuickAckMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case QuickAckModeFixedTemplate:
		return QuickAckModeFixedTemplate
	case QuickAckModeOff:
		return QuickAckModeOff
	default:
		return QuickAckModeLLMGenerated
	}
}

func effectiveQuickAckMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return QuickAckModeLLMGenerated
	}
	return normalizeQuickAckMode(mode)
}

func buildAckPreview(behavior ResolvedChatBehavior, streaming, hasToolCalls bool) AckPreview {
	mode := effectiveQuickAckMode(behavior.QuickAck.Mode)
	preview := AckPreview{Mode: mode}
	if !behavior.Enabled || !behavior.QuickAck.Enabled || streaming {
		preview.Source = QuickAckSourceOff
		return preview
	}
	if mode == QuickAckModeOff {
		preview.Source = QuickAckSourceOff
		return preview
	}
	if len(behavior.QuickAck.Templates) == 0 {
		return preview
	}
	if mode == QuickAckModeFixedTemplate {
		preview.ShouldSend = true
		preview.Source = QuickAckSourceTemplate
		preview.Content = behavior.QuickAck.Templates[0]
		return preview
	}
	preview.ShouldSend = true
	if hasToolCalls {
		preview.Source = QuickAckSourceGenerated
		preview.FallbackContent = behavior.QuickAck.Templates[0]
		return preview
	}
	preview.Source = QuickAckSourceTemplate
	preview.Content = behavior.QuickAck.Templates[0]
	return preview
}

func SplitFinalMessages(content string, cfg ResolvedFinalSplitConfig) []string {
	if content == "" {
		return nil
	}
	if !cfg.Enabled || len(content) < cfg.MinChars || cfg.MaxMessages <= 1 || hasUnsafeSplitMarkdown(content) {
		return []string{content}
	}
	parts := splitParagraphs(content)
	if len(parts) <= 1 || len(parts) > cfg.MaxMessages {
		return []string{content}
	}
	return parts
}

func splitParagraphs(content string) []string {
	raw := strings.Split(content, "\n\n")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		p := strings.TrimSpace(part)
		if p == "" {
			continue
		}
		parts = append(parts, p)
	}
	return parts
}

func hasUnsafeSplitMarkdown(content string) bool {
	lines := strings.SplitSeq(content, "\n")
	for line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			return true
		case strings.HasPrefix(trimmed, ">"):
			return true
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			return true
		case len(trimmed) > 3 && trimmed[0] >= '0' && trimmed[0] <= '9' && strings.HasPrefix(trimmed[1:], ". "):
			return true
		case strings.Contains(trimmed, "|") && (strings.Contains(trimmed, "---") || strings.Count(trimmed, "|") >= 2):
			return true
		case strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "<"):
			return true
		case strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://"):
			return true
		}
	}
	return false
}
