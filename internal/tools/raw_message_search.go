package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// RawMessageSearchTool searches raw WhatsApp message history using hybrid
// keyword + semantic search with Reciprocal Rank Fusion.
type RawMessageSearchTool struct {
	chunkStore store.RawMessageChunkStore
}

func NewRawMessageSearchTool() *RawMessageSearchTool {
	return &RawMessageSearchTool{}
}

func (t *RawMessageSearchTool) SetChunkStore(s store.RawMessageChunkStore) {
	t.chunkStore = s
}

func (t *RawMessageSearchTool) Name() string { return "raw_message_search" }

func (t *RawMessageSearchTool) Description() string {
	return "Search raw WhatsApp message history using hybrid keyword + semantic search. " +
		"Returns message chunks with sender, chat, and timestamp metadata. " +
		"Use this to find specific conversations, decisions, or information from monitored chats. " +
		"Supports filtering by chat_id or graph scope. " +
		"Search in the SAME language as the stored messages for best results."
}

func (t *RawMessageSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language search query. Use the same language as the stored messages.",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Filter to a specific chat/group ID.",
			},
			"graph_id": map[string]any{
				"type":        "string",
				"description": "Filter to a specific graph scope.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results (default 10, max 50).",
			},
			"from_time": map[string]any{
				"type":        "string",
				"description": "Only include chunks from messages on or after this time (ISO 8601).",
			},
			"to_time": map[string]any{
				"type":        "string",
				"description": "Only include chunks from messages on or before this time (ISO 8601).",
			},
		},
		"required": []string{"query"},
	}
}

func (t *RawMessageSearchTool) Execute(ctx context.Context, args map[string]any) *Result {
	if t.chunkStore == nil {
		return &Result{ForLLM: "Raw message search is not available (no chunk store configured)."}
	}

	query, _ := args["query"].(string)
	if query == "" {
		return &Result{ForLLM: "Error: query is required."}
	}

	agentID := store.AgentIDFromContext(ctx)
	if agentID.String() == "" || agentID.String() == "00000000-0000-0000-0000-000000000000" {
		return &Result{ForLLM: "Error: agent context not available."}
	}

	opts := store.RawMessageChunkSearchOptions{MaxResults: 10}

	if chatID, ok := args["chat_id"].(string); ok && chatID != "" {
		opts.ChatID = chatID
	}
	if graphID, ok := args["graph_id"].(string); ok && graphID != "" {
		opts.GraphID = graphID
	}
	if maxResults, ok := toInt(args["max_results"]); ok && maxResults > 0 {
		opts.MaxResults = min(maxResults, 50)
	}
	if fromStr, ok := args["from_time"].(string); ok && fromStr != "" {
		if t, err := time.Parse(time.RFC3339, fromStr); err == nil {
			opts.FromTime = &t
		}
	}
	if toStr, ok := args["to_time"].(string); ok && toStr != "" {
		if t, err := time.Parse(time.RFC3339, toStr); err == nil {
			opts.ToTime = &t
		}
	}

	results, err := t.chunkStore.Search(ctx, query, agentID.String(), opts)
	if err != nil {
		return &Result{ForLLM: fmt.Sprintf("Search error: %v", err)}
	}

	if len(results) == 0 {
		return &Result{ForLLM: "No matching message chunks found."}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Found %d message chunks (hybrid RRF search):\n\n", len(results))

	for i, r := range results {
		c := r.Chunk
		fmt.Fprintf(&b, "%d. [%s] Chat: \"%s\" (%s)\n",
			i+1, c.MsgTimeFrom.Format("2006-01-02 15:04"), c.ChatName, c.ChatID)
		fmt.Fprintf(&b, "   From: %s\n", c.Sender)
		if r.FTSRank > 0 || r.VectorRank > 0 {
			fmt.Fprintf(&b, "   Score: %.4f", r.Score)
			if r.FTSRank > 0 {
				fmt.Fprintf(&b, " (keyword rank: %d", r.FTSRank)
				if r.VectorRank > 0 {
					fmt.Fprintf(&b, ", semantic rank: %d", r.VectorRank)
				}
				fmt.Fprintf(&b, ")")
			} else if r.VectorRank > 0 {
				fmt.Fprintf(&b, " (semantic rank: %d)", r.VectorRank)
			}
			fmt.Fprintln(&b)
		}
		text := c.Text
		if len(text) > 500 {
			text = text[:497] + "..."
		}
		fmt.Fprintf(&b, "   \"%s\"\n\n", text)
	}

	return &Result{ForLLM: b.String()}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
