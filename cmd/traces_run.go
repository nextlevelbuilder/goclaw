package cmd

import (
	"fmt"
	"net/http"
	"os"
)

func runTracesList(opts traceListOptions) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	resp, err := gatewayHTTPGetTyped[traceListResponse](buildTraceListPath(opts))
	if err != nil {
		return err
	}
	return printTraceList(resp)
}

func runTracesGet(traceID string) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	resp, err := gatewayHTTPGetTyped[traceDetailResponse](traceDetailPath(traceID))
	if err != nil {
		return err
	}
	return printTraceDetail(resp)
}

func runTracesExport(traceID, filePath string) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	raw, status, err := gatewayHTTPDoRawWithLimit(http.MethodGet, traceExportPath(traceID), nil, traceExportResponseLimit)
	if err != nil {
		return err
	}
	if status >= 400 {
		return parseHTTPError(raw, status)
	}
	if outputFormatIsJSON() {
		return printGzipJSON(raw)
	}
	if filePath == "-" {
		_, err = os.Stdout.Write(raw)
		return err
	}
	if filePath == "" {
		filePath = defaultTraceExportPath(traceID)
	}
	if err := os.WriteFile(filePath, raw, 0o600); err != nil {
		return err
	}
	fmt.Printf("Exported trace to %s\n", filePath)
	return nil
}

func runTracesFollow(opts traceFollowOptions) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	path, err := buildTraceFollowPath(opts)
	if err != nil {
		return err
	}
	resp, err := gatewayHTTPGetTyped[traceFollowResponse](path)
	if err != nil {
		return err
	}
	return printTraceFollow(resp)
}

func runTracesTimeline(traceID string, opts traceTimelineOptions) error {
	if err := validateTraceOutputFormat(); err != nil {
		return err
	}
	detail, err := gatewayHTTPGetTyped[traceDetailResponse](traceDetailPath(traceID))
	if err != nil {
		return err
	}
	runID, err := traceRunIDFromDetail(detail)
	if err != nil {
		return err
	}
	resp, err := gatewayHTTPGetTyped[traceTimelineResponse](
		buildTraceTimelinePath(runID, detail.Trace.SessionKey, opts),
	)
	if err != nil {
		return err
	}
	return printTraceTimeline(resp)
}
