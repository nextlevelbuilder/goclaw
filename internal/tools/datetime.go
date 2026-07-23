package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DateTimeTool provides precise current date/time with timezone support.
// Models use this before creating cron jobs or any time-sensitive operation
// instead of guessing timestamps from the system prompt's date-only field.
type DateTimeTool struct {
	now func() time.Time
}

func NewDateTimeTool() *DateTimeTool { return &DateTimeTool{now: time.Now} }

func (t *DateTimeTool) Name() string { return "datetime" }

func (t *DateTimeTool) Description() string {
	return `Get the current date and time. Use this when you need precise timestamps for scheduling (cron jobs), logging, or any time-sensitive operation.

Returns current time in both UTC and the requested timezone.
If no timezone is provided, returns UTC only.
Calendar fields such as weekday and iso_weekday are computed by the server and are authoritative. Use them verbatim; never infer or recalculate the weekday from a date.`
}

func (t *DateTimeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"timezone": map[string]any{
				"type":        "string",
				"description": "IANA timezone name (e.g. 'Asia/Ho_Chi_Minh', 'America/New_York'). If omitted, returns UTC only.",
			},
		},
	}
}

func (t *DateTimeTool) Execute(_ context.Context, args map[string]any) *Result {
	now := time.Now()
	if t != nil && t.now != nil {
		now = t.now()
	}
	utc := now.UTC()
	result := map[string]any{
		"utc":             utc.Format(time.RFC3339),
		"utc_date":        utc.Format(time.DateOnly),
		"utc_time":        utc.Format(time.TimeOnly),
		"utc_weekday":     utc.Weekday().String(),
		"utc_iso_weekday": isoWeekday(utc),
		"unix_ms":         now.UnixMilli(),
	}

	if tz, ok := args["timezone"].(string); ok && tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return ErrorResult(fmt.Sprintf("invalid timezone '%s': use IANA names like 'Asia/Ho_Chi_Minh', 'America/New_York'", tz))
		}
		local := now.In(loc)
		result["local"] = local.Format(time.RFC3339)
		result["local_date"] = local.Format(time.DateOnly)
		result["local_time"] = local.Format(time.TimeOnly)
		result["weekday"] = local.Weekday().String()
		result["iso_weekday"] = isoWeekday(local)
		result["utc_offset"] = local.Format("Z07:00")
		result["timezone"] = tz
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to encode datetime result: %v", err))
	}
	return NewResult(string(data))
}

func isoWeekday(value time.Time) int {
	weekday := int(value.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}
