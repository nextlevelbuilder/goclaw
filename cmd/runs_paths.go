package cmd

import (
	"net/url"
	"strconv"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// CLI type aliases reuse the store shapes so output mirrors the API contract.
type agentRunForCLI = store.AgentRun
type runTimelineItemForCLI = store.RunTimelineItem

type runsListResponse struct {
	Runs   []agentRunForCLI `json:"runs"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type runsGetResponse struct {
	Run agentRunForCLI `json:"run"`
}

type runsEventsResponse struct {
	RunID     string                  `json:"run_id"`
	AfterSeq  int                     `json:"after"`
	Items     []runTimelineItemForCLI `json:"items"`
	Limit     int                     `json:"limit"`
	NextAfter int                     `json:"next_after"`
}

func runsListPath(opts runsListOptions) string {
	values := url.Values{}
	addQuery(values, "session_key", opts.SessionKey)
	addQuery(values, "status", opts.Status)
	addIntQuery(values, "limit", opts.Limit)
	addIntQuery(values, "offset", opts.Offset)
	return pathWithQuery("/v1/runs", values)
}

func runGetPath(runID string) string {
	return "/v1/runs/" + url.PathEscape(runID)
}

func runEventsPath(runID string, after, limit int, sessionKey string) string {
	values := url.Values{}
	if after > 0 {
		values.Set("after", strconv.Itoa(after))
	}
	addIntQuery(values, "limit", limit)
	addQuery(values, "session_key", sessionKey)
	return pathWithQuery("/v1/runs/"+url.PathEscape(runID)+"/events", values)
}
