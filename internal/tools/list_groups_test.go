package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

func TestListGroupsTool_Execute(t *testing.T) {
	tool := NewListGroupsTool()
	var gotChannel string
	tool.SetGroupLister(func(_ context.Context, channel string) ([]channels.GroupInfo, error) {
		gotChannel = channel
		return []channels.GroupInfo{
			{GroupID: "747300108647389888", Name: "Ban Điều Hành", TotalMember: 8},
			{GroupID: "1208490753003346779", Name: "Team Content", TotalMember: 12},
		}, nil
	})

	ctx := WithToolChannel(context.Background(), "bunny-zalo-personal")
	res := tool.Execute(ctx, map[string]any{})
	if res == nil || res.IsError {
		t.Fatalf("unexpected error result: %+v", res)
	}
	if gotChannel != "bunny-zalo-personal" {
		t.Fatalf("lister called with channel %q, want bunny-zalo-personal", gotChannel)
	}

	var out struct {
		Channel string `json:"channel"`
		Count   int    `json:"count"`
		Groups  []struct {
			GroupID string `json:"group_id"`
			Name    string `json:"name"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(res.ForLLM), &out); err != nil {
		t.Fatalf("failed to parse tool output: %v (raw: %s)", err, res.ForLLM)
	}
	if out.Count != 2 {
		t.Fatalf("count = %d, want 2", out.Count)
	}
	if out.Groups[0].Name != "Ban Điều Hành" || out.Groups[0].GroupID != "747300108647389888" {
		t.Fatalf("unexpected first group: %+v", out.Groups[0])
	}
}

func TestListGroupsTool_NoChannelInContext(t *testing.T) {
	tool := NewListGroupsTool()
	tool.SetGroupLister(func(_ context.Context, _ string) ([]channels.GroupInfo, error) {
		t.Fatal("lister should not be called without a channel in context")
		return nil, nil
	})

	res := tool.Execute(context.Background(), map[string]any{})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result when channel is missing from context, got: %+v", res)
	}
}

func TestListGroupsTool_ListerUnset(t *testing.T) {
	tool := NewListGroupsTool()
	ctx := WithToolChannel(context.Background(), "bunny-zalo-personal")
	res := tool.Execute(ctx, map[string]any{})
	if res == nil || !res.IsError {
		t.Fatalf("expected error result when lister is unset, got: %+v", res)
	}
}
