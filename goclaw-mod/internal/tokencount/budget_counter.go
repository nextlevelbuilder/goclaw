package tokencount

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
)

const (
	budgetEncodingPattern = `(?i:'s|'t|'re|'ve|'m|'ll|'d)|[^\r\n\p{L}\p{N}]?\p{L}+|\p{N}{1,3}| ?[^\s\p{L}\p{N}]+[\r\n]*|\s*[\r\n]+|\s+(?!\S)|\s+`
	budgetMessageOverhead = 4
	budgetInlineMediaUnit = 1600
)

//go:embed cl100k_base.tiktoken.gz
var budgetEncodingData []byte

// BudgetCounter counts the complete input side of a model request with one
// fixed, bundled GoClaw encoding. Its API intentionally has no model or provider
// parameter: agent configuration is the only request-budget authority.
type BudgetCounter interface {
	CountText(text string) (int, error)
	CountMessages(messages []providers.Message) (int, error)
	CountToolSchemas(tools []providers.ToolDefinition) (int, error)
	CountRequest(request providers.ChatRequest) (int, error)
}

type fixedBudgetCounter struct {
	once    sync.Once
	encoder *tiktoken.Tiktoken
	err     error
}

// NewBudgetCounter returns GoClaw's fixed local complete-input counter.
func NewBudgetCounter() BudgetCounter { return &fixedBudgetCounter{} }

func (c *fixedBudgetCounter) CountText(text string) (int, error) {
	encoder, err := c.load()
	if err != nil {
		return 0, err
	}
	return len(encoder.Encode(text, nil, nil)), nil
}

func (c *fixedBudgetCounter) CountMessages(messages []providers.Message) (int, error) {
	encoder, err := c.load()
	if err != nil {
		return 0, err
	}

	total := 0
	for _, message := range messages {
		total += encodeBudgetText(encoder, message.Role)
		total += encodeBudgetText(encoder, message.Content)
		total += encodeBudgetText(encoder, message.Thinking)
		total += encodeBudgetText(encoder, message.ToolCallID)
		total += encodeBudgetText(encoder, message.Phase)
		total += encodeBudgetText(encoder, string(message.RawAssistantContent))
		total += budgetMessageOverhead
		if message.IsError {
			total++
		}
		for _, call := range message.ToolCalls {
			total += encodeBudgetText(encoder, call.ID)
			total += encodeBudgetText(encoder, call.Name)
			total += encodeBudgetJSON(encoder, call.Arguments)
			total += encodeBudgetJSON(encoder, call.Metadata)
			total += encodeBudgetText(encoder, call.ParseError)
		}
		for _, image := range message.Images {
			if image.Data != "" || image.URL != "" {
				total += budgetInlineMediaUnit
			}
		}
		for _, video := range message.Videos {
			if video.Data != "" || video.URL != "" {
				total += budgetInlineMediaUnit
			}
		}
	}
	return total, nil
}

func (c *fixedBudgetCounter) CountToolSchemas(tools []providers.ToolDefinition) (int, error) {
	if len(tools) == 0 {
		return 0, nil
	}
	encoder, err := c.load()
	if err != nil {
		return 0, err
	}
	blob, err := json.Marshal(tools)
	if err != nil {
		return 0, fmt.Errorf("marshal tool schemas for budget: %w", err)
	}
	return len(encoder.Encode(string(blob), nil, nil)), nil
}

func (c *fixedBudgetCounter) CountRequest(request providers.ChatRequest) (int, error) {
	messages, err := c.CountMessages(request.Messages)
	if err != nil {
		return 0, err
	}
	tools, err := c.CountToolSchemas(request.Tools)
	if err != nil {
		return 0, err
	}
	return messages + tools, nil
}

func (c *fixedBudgetCounter) load() (*tiktoken.Tiktoken, error) {
	c.once.Do(func() {
		c.encoder, c.err = buildBudgetEncoder(budgetEncodingData)
	})
	return c.encoder, c.err
}

func buildBudgetEncoder(compressed []byte) (*tiktoken.Tiktoken, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("open bundled budget encoding: %w", err)
	}
	defer reader.Close()

	ranks := make(map[string]int, 100_256)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid bundled budget encoding row")
		}
		token, decodeErr := base64.StdEncoding.DecodeString(parts[0])
		if decodeErr != nil {
			return nil, fmt.Errorf("decode bundled budget token: %w", decodeErr)
		}
		rank, parseErr := strconv.Atoi(parts[1])
		if parseErr != nil {
			return nil, fmt.Errorf("parse bundled budget rank: %w", parseErr)
		}
		ranks[string(token)] = rank
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read bundled budget encoding: %w", err)
	}

	special := map[string]int{
		tiktoken.ENDOFTEXT:   100257,
		tiktoken.FIM_PREFIX:  100258,
		tiktoken.FIM_MIDDLE:  100259,
		tiktoken.FIM_SUFFIX:  100260,
		tiktoken.ENDOFPROMPT: 100276,
	}
	core, err := tiktoken.NewCoreBPE(ranks, special, budgetEncodingPattern)
	if err != nil {
		return nil, fmt.Errorf("build bundled budget encoding: %w", err)
	}
	encoding := &tiktoken.Encoding{
		Name:           "goclaw_budget_cl100k",
		PatStr:         budgetEncodingPattern,
		MergeableRanks: ranks,
		SpecialTokens:  special,
	}
	specialSet := make(map[string]any, len(special))
	for token := range special {
		specialSet[token] = true
	}
	return tiktoken.NewTiktoken(core, encoding, specialSet), nil
}

func encodeBudgetText(encoder *tiktoken.Tiktoken, text string) int {
	if text == "" {
		return 0
	}
	return len(encoder.Encode(text, nil, nil))
}

func encodeBudgetJSON(encoder *tiktoken.Tiktoken, value any) int {
	blob, err := json.Marshal(value)
	if err != nil || string(blob) == "null" || string(blob) == "{}" {
		return 0
	}
	return len(encoder.Encode(string(blob), nil, nil))
}
