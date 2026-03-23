package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DefaultMemoryFlushSystemPrompt is the system prompt for structured extraction.
const DefaultMemoryFlushSystemPrompt = "Ты извлекаешь ключевую информацию из разговора для сохранения в памяти. Этот документ используется в двух случаях: во время текущей сессии — когда контекст вытесняется и агент ищет что обсуждалось ранее; в будущих сессиях — для восстановления контекста прошлой работы. Пиши на русском языке. Будь исчерпывающим и конкретным — записывай точные значения, не общие фразы."

// structuredExtractionPrompt is the user prompt for Go-controlled compact extraction.
const structuredExtractionPrompt = `Извлеки всю важную информацию из этого разговора в следующие категории.
ВАЖНО: пиши конкретные значения, не общие описания.
Неправильно: «порт был изменён». Правильно: «порт изменён с 3000 на 3456».
Неправильно: «добавлен новый агент». Правильно: «добавлен агент Euler, модель claude-sonnet-4, роль архитектор».

КОНТЕКСТ
Кто пользователь, над каким проектом/задачей работали, какова была основная цель сессии.

РЕШЕНИЯ
Каждое принятое решение и его обоснование. Включай отвергнутые альтернативы если они обсуждались.

КОНФИГУРАЦИЯ
Все точные значения: пути к файлам, порты, ID, URL, переменные окружения, настройки, команды.

ЗАДАЧИ
Что выполнено, что в процессе, что заблокировано и почему.

ОШИБКИ И РЕШЕНИЯ
Проблемы с которыми столкнулись и как их решили. Точные сообщения об ошибках.

ОТКРЫТЫЕ ВОПРОСЫ
Нерешённые вопросы, вещи которые нужно проверить или обсудить позже.

Если категория пуста — пиши «нет».
Отвечай только структурированным документом, без вступлений.`


// MemoryFlushSettings holds resolved flush config with defaults applied.
type MemoryFlushSettings struct {
	Enabled      bool
	SystemPrompt string
}

// ResolveMemoryFlushSettings resolves flush settings from config, applying defaults.
// Returns nil if disabled.
func ResolveMemoryFlushSettings(compaction *config.CompactionConfig) *MemoryFlushSettings {
	if compaction == nil || compaction.MemoryFlush == nil {
		// Default: enabled
		return &MemoryFlushSettings{
			Enabled:      true,
			SystemPrompt: DefaultMemoryFlushSystemPrompt,
		}
	}

	mf := compaction.MemoryFlush
	if mf.Enabled != nil && !*mf.Enabled {
		return nil
	}

	settings := &MemoryFlushSettings{
		Enabled:      true,
		SystemPrompt: DefaultMemoryFlushSystemPrompt,
	}

	if mf.SystemPrompt != "" {
		settings.SystemPrompt = mf.SystemPrompt
	}

	return settings
}

// shouldRunMemoryFlush checks whether a memory flush should run before compaction.
// Flush always runs when compaction triggers (called inside maybeSummarize),
// gated only by enabled/memory checks and a dedup guard per compaction cycle.
func (l *Loop) shouldRunMemoryFlush(ctx context.Context, sessionKey string, totalTokens int, settings *MemoryFlushSettings) bool {
	if settings == nil || !settings.Enabled || !l.hasMemory {
		return false
	}

	if totalTokens <= 0 {
		return false
	}

	// Deduplication: skip if already flushed in this compaction cycle.
	compactionCount := l.sessions.GetCompactionCount(ctx, sessionKey)
	lastFlushAt := l.sessions.GetMemoryFlushCompactionCount(ctx, sessionKey)
	if lastFlushAt >= 0 && lastFlushAt == compactionCount {
		return false
	}

	return true
}

// runMemoryFlush performs structured extraction of the conversation into a compact document.
// Instead of asking the agent to decide what to write (tool-call loop), this is fully
// Go-controlled: LLM extracts structured facts → Go writes to memory/compact_N.md via
// the same write_file → interceptor → embedding pipeline. Agent cannot skip the write.
func (l *Loop) runMemoryFlush(ctx context.Context, sessionKey string, settings *MemoryFlushSettings) {
	slog.Info("memory flush: starting structured extraction", "session", sessionKey)

	flushCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Build messages: system prompt + history summary + flush prompt
	history := l.sessions.GetHistory(flushCtx, sessionKey)
	summary := l.sessions.GetSummary(flushCtx, sessionKey)

	// Build messages for extraction LLM call
	var messages []providers.Message
	messages = append(messages, providers.Message{
		Role:    "system",
		Content: settings.SystemPrompt,
	})

	if summary != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: "[Previous conversation summary]\n" + summary,
		})
		messages = append(messages, providers.Message{
			Role:    "assistant",
			Content: "Understood.",
		})
	}

	// Include full history for extraction
	sanitized, _ := sanitizeHistory(history)
	messages = append(messages, sanitized...)

	messages = append(messages, providers.Message{
		Role:    "user",
		Content: structuredExtractionPrompt,
	})

	resp, err := l.provider.Chat(flushCtx, providers.ChatRequest{
		Messages: messages,
		Model:    l.model,
		Options: map[string]any{
			"max_tokens":  5000,
			"temperature": 0.2,
		},
	})
	if err != nil {
		slog.Warn("memory flush: extraction LLM call failed", "error", err)
		l.sessions.SetMemoryFlushDone(ctx, sessionKey)
		l.sessions.Save(ctx, sessionKey)
		return
	}

	extracted := SanitizeAssistantContent(resp.Content)
	if extracted == "" || IsSilentReply(extracted) {
		slog.Info("memory flush: nothing to extract")
		l.sessions.SetMemoryFlushDone(ctx, sessionKey)
		l.sessions.Save(ctx, sessionKey)
		return
	}

	// Compact number = current count + 1 (IncrementCompaction runs after TruncateHistory)
	compactNum := l.sessions.GetCompactionCount(ctx, sessionKey) + 1
	numericID := l.sessions.GetNumericID(ctx, sessionKey)
	var path string
	if numericID > 0 {
		path = fmt.Sprintf("memory/sessions/%d/compact_%d.md", numericID, compactNum)
	} else {
		path = fmt.Sprintf("memory/compact_%d.md", compactNum)
	}

	// Enrich context with compact metadata so PutDocument can tag memory_documents
	writeCtx := store.WithCompactSessionKey(flushCtx, sessionKey)
	writeCtx = store.WithCompactNumber(writeCtx, compactNum)

	// Write via the same write_file → memory interceptor → embedding pipeline
	result := l.tools.ExecuteWithContext(writeCtx, "write_file",
		map[string]any{"path": path, "content": extracted},
		"", "", "", sessionKey, nil,
	)


	if result != nil && result.IsError {
		slog.Warn("memory flush: write_file failed", "path", path, "error", result.ForLLM)
	} else {
		slog.Info("memory flush: compact written", "session", sessionKey, "compact", compactNum, "path", path, "chars", len(extracted))
	}

	// Mark flush as done
	l.sessions.SetMemoryFlushDone(ctx, sessionKey)
	l.sessions.Save(ctx, sessionKey)

	slog.Info("memory flush: completed", "session", sessionKey)
}

// extractiveMemoryFallback runs the regex-based extraction on conversation history
// and writes the result directly to the memory store when the LLM flush produced no output.
func (l *Loop) extractiveMemoryFallback(ctx context.Context, sessionKey string, history []providers.Message, reason string) {
	if l.memStore == nil {
		return
	}

	// Limit input to last 20 messages to avoid regex over unbounded text
	if len(history) > 20 {
		history = history[len(history)-20:]
	}

	extracted := ExtractiveMemoryFallback(history)
	if extracted == "" {
		slog.Info("memory flush: extractive fallback produced no content", "session", sessionKey, "reason", reason)
		return
	}

	agentID := l.agentUUID.String()
	userID := store.MemoryUserID(ctx)
	docPath := fmt.Sprintf("memory/%s-auto-extract.md", time.Now().Format("2006-01-02"))

	// Append to existing document if it exists
	existing, err := l.memStore.GetDocument(ctx, agentID, userID, docPath)
	if err == nil && existing != "" {
		extracted = existing + "\n\n---\n\n" + extracted
	}

	if err := l.memStore.PutDocument(ctx, agentID, userID, docPath, extracted); err != nil {
		slog.Warn("memory flush: extractive fallback write failed", "session", sessionKey, "error", err)
		return
	}

	if err := l.memStore.IndexDocument(ctx, agentID, userID, docPath); err != nil {
		slog.Warn("memory flush: extractive fallback index failed", "session", sessionKey, "error", err)
		// Non-fatal: document was saved
	}

	slog.Info("memory flush: extractive fallback saved", "session", sessionKey, "reason", reason, "path", docPath, "content_len", len(extracted))
}
