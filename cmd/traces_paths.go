package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type traceDataForCLI = store.TraceData
type spanDataForCLI = store.SpanData
type timelineItemForCLI = store.RunTimelineItem

type traceListOptions struct {
	Query           string
	AgentID         string
	UserID          string
	SessionKey      string
	Status          string
	Channel         string
	AgentQuery      string
	ChannelQuery    string
	ToolName        string
	From            string
	To              string
	Since           string
	Until           string
	HasToolCalls    string
	MinInputTokens  int
	MaxInputTokens  int
	MinOutputTokens int
	MaxOutputTokens int
	MinToolCalls    int
	MaxToolCalls    int
	Limit           int
	Offset          int
}

type traceFollowOptions struct {
	AgentID      string
	SessionKey   string
	Status       string
	Channel      string
	UserID       string
	Since        string
	Limit        int
	IncludeSpans bool
}

type traceTimelineOptions struct {
	Limit  int
	Offset int
}

type traceListResponse struct {
	Traces []traceDataForCLI `json:"traces"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
}

type traceDetailResponse struct {
	Trace traceDataForCLI  `json:"trace"`
	Spans []spanDataForCLI `json:"spans"`
}

type traceFollowResponse struct {
	Traces         []traceDataForCLI           `json:"traces"`
	SpansByTraceID map[string][]spanDataForCLI `json:"spans_by_trace_id"`
	ServerTime     string                      `json:"server_time"`
	NextSince      string                      `json:"next_since"`
	Limit          int                         `json:"limit"`
}

type traceTimelineResponse struct {
	RunID      string               `json:"run_id"`
	SessionKey string               `json:"session_key"`
	Items      []timelineItemForCLI `json:"items"`
	Limit      int                  `json:"limit"`
	Offset     int                  `json:"offset"`
}

func buildTraceListPath(opts traceListOptions) string {
	values := url.Values{}
	addQuery(values, "q", opts.Query)
	addQuery(values, "agent_id", opts.AgentID)
	addQuery(values, "user_id", opts.UserID)
	addQuery(values, "session_key", opts.SessionKey)
	addQuery(values, "status", opts.Status)
	addQuery(values, "channel", opts.Channel)
	addQuery(values, "agent", opts.AgentQuery)
	addQuery(values, "channel_query", opts.ChannelQuery)
	addQuery(values, "tool_name", opts.ToolName)
	addQuery(values, "from", firstNonEmpty(opts.From, opts.Since))
	addQuery(values, "to", firstNonEmpty(opts.To, opts.Until))
	addQuery(values, "has_tool_calls", opts.HasToolCalls)
	addIntQuery(values, "min_input_tokens", opts.MinInputTokens)
	addIntQuery(values, "max_input_tokens", opts.MaxInputTokens)
	addIntQuery(values, "min_output_tokens", opts.MinOutputTokens)
	addIntQuery(values, "max_output_tokens", opts.MaxOutputTokens)
	addIntQuery(values, "min_tool_calls", opts.MinToolCalls)
	addIntQuery(values, "max_tool_calls", opts.MaxToolCalls)
	addIntQuery(values, "limit", opts.Limit)
	addIntQuery(values, "offset", opts.Offset)
	return pathWithQuery("/v1/traces", values)
}

func buildTraceFollowPath(opts traceFollowOptions) (string, error) {
	if strings.TrimSpace(opts.SessionKey) == "" && strings.TrimSpace(opts.AgentID) == "" {
		return "", fmt.Errorf("traces follow requires --session or --agent-id")
	}
	values := url.Values{}
	addQuery(values, "agent_id", opts.AgentID)
	addQuery(values, "session_key", opts.SessionKey)
	addQuery(values, "status", opts.Status)
	addQuery(values, "channel", opts.Channel)
	addQuery(values, "user_id", opts.UserID)
	addQuery(values, "since", opts.Since)
	addIntQuery(values, "limit", opts.Limit)
	if opts.IncludeSpans {
		values.Set("include_spans", "true")
	}
	return pathWithQuery("/v1/traces/follow", values), nil
}

func buildTraceTimelinePath(runID, sessionKey string, opts traceTimelineOptions) string {
	values := url.Values{}
	addQuery(values, "session_key", sessionKey)
	addIntQuery(values, "limit", opts.Limit)
	addIntQuery(values, "offset", opts.Offset)
	return pathWithQuery("/v1/runs/"+url.PathEscape(runID)+"/timeline", values)
}

func traceDetailPath(traceID string) string {
	return "/v1/traces/" + url.PathEscape(traceID)
}

func traceExportPath(traceID string) string {
	return "/v1/traces/" + url.PathEscape(traceID) + "/export"
}

func traceRunIDFromDetail(detail traceDetailResponse) (string, error) {
	runID := strings.TrimSpace(detail.Trace.RunID)
	if runID == "" {
		return "", fmt.Errorf("trace has no run_id; timeline is unavailable")
	}
	return runID, nil
}

func addQuery(values url.Values, key, value string) {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		values.Set(key, trimmed)
	}
}

func addIntQuery(values url.Values, key string, value int) {
	if value > 0 {
		values.Set(key, strconv.Itoa(value))
	}
}

func pathWithQuery(path string, values url.Values) string {
	if len(values) == 0 {
		return path
	}
	return path + "?" + values.Encode()
}
