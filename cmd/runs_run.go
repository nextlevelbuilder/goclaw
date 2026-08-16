package cmd

import (
	"fmt"
)

func runRunsList(opts runsListOptions) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	if !runsStatusValid(opts.Status) {
		return fmt.Errorf("invalid status %q; use pending|running|compacting|completed|failed|cancelled", opts.Status)
	}
	opts.Limit, opts.Offset = runsPagination(opts.Limit, opts.Offset)
	resp, err := gatewayHTTPGetTyped[runsListResponse](runsListPath(opts))
	if err != nil {
		return err
	}
	return printRunsList(resp)
}

func runRunsGet(runID string) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	resp, err := gatewayHTTPGetTyped[runsGetResponse](runGetPath(runID))
	if err != nil {
		return err
	}
	return printRunsGet(resp)
}

func runRunsEvents(runID string, after, limit int, sessionKey string) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	if after < 0 {
		return fmt.Errorf("--after must be non-negative")
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	resp, err := gatewayHTTPGetTyped[runsEventsResponse](runEventsPath(runID, after, limit, sessionKey))
	if err != nil {
		return err
	}
	return printRunsEvents(resp)
}
