package channelmemory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type fakeExtractionProvider struct {
	responses []providers.ChatResponse
	requests  []providers.ChatRequest
}

func (f *fakeExtractionProvider) Chat(_ context.Context, req providers.ChatRequest) (*providers.ChatResponse, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		return &providers.ChatResponse{Content: "[]", FinishReason: "stop"}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return &resp, nil
}

func (f *fakeExtractionProvider) ChatStream(_ context.Context, req providers.ChatRequest, _ func(providers.StreamChunk)) (*providers.ChatResponse, error) {
	return f.Chat(context.Background(), req)
}

func (f *fakeExtractionProvider) DefaultModel() string { return "fake-model" }
func (f *fakeExtractionProvider) Name() string         { return "fake" }

func TestExtractRetriesAfterLengthFinish(t *testing.T) {
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{
		{Content: `[{"type":"todos","summary":"Finish`, FinishReason: "length"},
		{Content: `[{"type":"todos","summary":"Finish release notes","topics":["release"],"entities":["ExampleGateway"],"confidence":0.9}]`, FinishReason: "stop"},
	}}

	items, err := Extract(context.Background(), provider, "fake-model", nil, extractionTestMessages(12), DefaultAllowedTypes)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Summary != "Finish release notes" {
		t.Fatalf("summary = %q", items[0].Summary)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	if got := provider.requests[0].Options[providers.OptMaxTokens]; got != extractionMaxOutputTokens {
		t.Fatalf("first max_tokens = %v, want %d", got, extractionMaxOutputTokens)
	}
	if got := provider.requests[1].Options[providers.OptMaxTokens]; got != extractionRetryMaxOutputTokens {
		t.Fatalf("retry max_tokens = %v, want %d", got, extractionRetryMaxOutputTokens)
	}
}

func TestExtractRetriesAfterEmptyResponse(t *testing.T) {
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{
		{Content: ``, FinishReason: "stop"},
		{Content: `[]`, FinishReason: "stop"},
	}}

	items, err := Extract(context.Background(), provider, "fake-model", nil, extractionTestMessages(5), DefaultAllowedTypes)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
}

func TestExtractRetriesAfterUnexpectedJSONEnd(t *testing.T) {
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{
		{Content: `[{"type":"projects","summary":"`, FinishReason: "stop"},
		{Content: `[{"type":"projects","summary":"Gateway dashboard rollout","topics":["dashboard"],"entities":["Gateway"],"confidence":0.86}]`, FinishReason: "stop"},
	}}

	items, err := Extract(context.Background(), provider, "fake-model", nil, extractionTestMessages(5), DefaultAllowedTypes)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Type != "projects" {
		t.Fatalf("type = %q, want projects", items[0].Type)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
}

func TestComposeExtractionPromptAppendsCustomPromptsBeforeFinalGuard(t *testing.T) {
	prompt := composeExtractionPrompt(ExtractionOptions{
		AllowedTypes:        []string{"projects", "todos"},
		GlobalCustomPrompt:  "Avoid duplicate facts across candidate items.",
		ChannelCustomPrompt: "Prefer one Project Orion fact over repeated media updates.",
		GroupCustomPrompt:   "This group is the Project Orion launch thread.",
	})

	assertContainsInOrder(t, prompt,
		"Extract only durable, reusable work context",
		"Avoid duplicate facts across candidate items.",
		"Prefer one Project Orion fact over repeated media updates.",
		"This group is the Project Orion launch thread.",
		"Return strict JSON array only",
	)
	if !strings.Contains(prompt, "Never include secrets") {
		t.Fatalf("base privacy guard missing from prompt:\n%s", prompt)
	}
	if got := strings.Count(prompt, "Return strict JSON array only"); got < 2 {
		t.Fatalf("strict JSON guard count = %d, want repeated final guard", got)
	}
}

func TestBuildExtractionInputPrependsContextBlock(t *testing.T) {
	input := buildExtractionInput(extractionTestMessages(1), ExtractionContext{
		Platform:          "discord",
		ChannelInstance:   "discord-main",
		HistoryKey:        "thread-1",
		ChannelID:         "thread-1",
		ChannelName:       "project-thread",
		ParentChannelID:   "parent-1",
		ParentChannelName: "design",
		CategoryID:        "category-1",
		CategoryName:      "Launch",
	})

	assertContainsInOrder(t, input,
		"[Channel context]",
		"platform: discord",
		"channel_name: project-thread",
		"parent_channel_name: design",
		"category_name: Launch",
		"[/Channel context]",
		"tester: Durable project context",
	)
}

func TestExtractWithOptionsTruncatesLongContextAndKeepsMessages(t *testing.T) {
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{
		{Content: `[]`, FinishReason: "stop"},
	}}
	opts := ExtractionOptions{
		Context: ExtractionContext{
			Platform:    "discord",
			ChannelName: strings.Repeat("project-context-", 1000),
		},
	}

	_, err := ExtractWithOptions(context.Background(), provider, "fake-model", nil, extractionTestMessages(1), opts)
	if err != nil {
		t.Fatalf("ExtractWithOptions() error = %v", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.requests))
	}
	userInput := provider.requests[0].Messages[1].Content
	if !strings.Contains(userInput, "tester: Durable project context") {
		t.Fatalf("message content missing after long context:\n%s", userInput)
	}
	if len(userInput) > extractionMaxInputChars {
		t.Fatalf("user input length = %d, want <= %d", len(userInput), extractionMaxInputChars)
	}
}

func TestExtractWithOptionsUsesSamePromptOnRetry(t *testing.T) {
	provider := &fakeExtractionProvider{responses: []providers.ChatResponse{
		{Content: `[{"type":"projects","summary":"`, FinishReason: "stop"},
		{Content: `[]`, FinishReason: "stop"},
	}}
	opts := ExtractionOptions{
		AllowedTypes:        DefaultAllowedTypes,
		GlobalCustomPrompt:  "Avoid duplicate facts across candidate items.",
		ChannelCustomPrompt: "Prefer one Project Orion fact.",
		GroupCustomPrompt:   "This group is the Project Orion launch thread.",
	}

	_, err := ExtractWithOptions(context.Background(), provider, "fake-model", nil, extractionTestMessages(5), opts)
	if err != nil {
		t.Fatalf("ExtractWithOptions() error = %v", err)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider calls = %d, want 2", len(provider.requests))
	}
	first := provider.requests[0].Messages[0].Content
	second := provider.requests[1].Messages[0].Content
	if first != second {
		t.Fatalf("retry prompt changed\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if !strings.Contains(first, opts.GlobalCustomPrompt) || !strings.Contains(first, opts.ChannelCustomPrompt) || !strings.Contains(first, opts.GroupCustomPrompt) {
		t.Fatalf("custom prompts missing from system prompt:\n%s", first)
	}
}

func TestParseExtractionResponseStripsCodeFence(t *testing.T) {
	items, err := parseExtractionResponse("```json\n[]\n```")
	if err != nil {
		t.Fatalf("parseExtractionResponse() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func assertContainsInOrder(t *testing.T, text string, parts ...string) {
	t.Helper()
	pos := 0
	for _, part := range parts {
		idx := strings.Index(text[pos:], part)
		if idx < 0 {
			t.Fatalf("%q not found after offset %d in:\n%s", part, pos, text)
		}
		pos += idx + len(part)
	}
}

func extractionTestMessages(n int) []store.PendingMessage {
	messages := make([]store.PendingMessage, 0, n)
	base := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)
	for i := range n {
		messages = append(messages, store.PendingMessage{
			Sender:    "tester",
			Body:      "Durable project context that may be extracted into channel memory.",
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
	}
	return messages
}
