package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// DateTimeTool provides precise current date/time with timezone support.
// Models use this before creating cron jobs or any time-sensitive operation
// instead of guessing timestamps from the system prompt's date-only field.
type DateTimeTool struct{}

func NewDateTimeTool() *DateTimeTool { return &DateTimeTool{} }

func (t *DateTimeTool) Name() string { return "datetime" }

func (t *DateTimeTool) Description() string {
	return `Get the current date and time. Use this when you need precise timestamps for scheduling (cron jobs), logging, or any time-sensitive operation.

Returns current time in both UTC and the configured timezone.
If no timezone is provided, uses the system default timezone.`
}

func (t *DateTimeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone name (e.g. 'Asia/Ho_Chi_Minh', 'America/New_York'). If omitted, uses the system default timezone.",
			},
		},
	}
}

func (t *DateTimeTool) Execute(ctx context.Context, args map[string]any) *Result {
	now := time.Now()
	result := map[string]any{
		"utc":     now.UTC().Format(time.RFC3339),
		"unix_ms": now.UnixMilli(),
	}

	tz, _ := args["timezone"].(string)
	if tz == "" {
		if rc := store.RunContextFromCtx(ctx); rc != nil && rc.DefaultTimezone != "" {
			tz = rc.DefaultTimezone
		}
	}

	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid timezone '%s': use IANA names like 'Asia/Ho_Chi_Minh', 'America/New_York'", tz))
		}
		result["local"] = now.In(loc).Format(time.RFC3339)
		result["timezone"] = tz
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return NewResult(string(data))
}
