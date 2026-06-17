package cmd

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
)

func outputFormatIsJSON() bool {
	return strings.EqualFold(strings.TrimSpace(gatewayOutputFormat), "json")
}

func validateTraceOutputFormat() error {
	switch strings.ToLower(strings.TrimSpace(gatewayOutputFormat)) {
	case "", "table", "json":
		return nil
	default:
		return fmt.Errorf("unsupported output format %q; use table or json", gatewayOutputFormat)
	}
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func printTraceList(resp traceListResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	if len(resp.Traces) == 0 {
		fmt.Println("No traces found.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TRACE\tSTATUS\tAGENT\tSESSION\tRUN\tTOKENS\tSTARTED")
	for _, tr := range resp.Traces {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d/%d\t%s\n",
			shortTraceID(tr.ID.String()),
			tr.Status,
			shortOptionalUUID(tr.AgentID),
			truncateStr(tr.SessionKey, 36),
			truncateStr(tr.RunID, 24),
			tr.TotalInputTokens,
			tr.TotalOutputTokens,
			formatTraceTime(tr.StartTime),
		)
	}
	return tw.Flush()
}

func printTraceDetail(resp traceDetailResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	tr := resp.Trace
	fmt.Printf("Trace:   %s\n", tr.ID)
	fmt.Printf("Status:  %s\n", tr.Status)
	fmt.Printf("Agent:   %s\n", shortOptionalUUID(tr.AgentID))
	fmt.Printf("Session: %s\n", tr.SessionKey)
	fmt.Printf("Run:     %s\n", tr.RunID)
	fmt.Printf("Tokens:  %d input / %d output\n", tr.TotalInputTokens, tr.TotalOutputTokens)
	if tr.Error != "" {
		fmt.Printf("Error:   %s\n", tr.Error)
	}
	if len(resp.Spans) == 0 {
		fmt.Println("\nNo spans found.")
		return nil
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SPAN\tTYPE\tSTATUS\tPROVIDER\tMODEL\tDURATION\tNAME")
	for _, sp := range resp.Spans {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%dms\t%s\n",
			shortTraceID(sp.ID.String()),
			sp.SpanType,
			sp.Status,
			sp.Provider,
			sp.Model,
			sp.DurationMS,
			truncateStr(sp.Name, 48),
		)
	}
	return tw.Flush()
}

func printTraceFollow(resp traceFollowResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	if resp.NextSince != "" {
		fmt.Printf("Next since: %s\n\n", resp.NextSince)
	}
	return printTraceList(traceListResponse{
		Traces: resp.Traces,
		Total:  len(resp.Traces),
		Limit:  resp.Limit,
	})
}

func printTraceTimeline(resp traceTimelineResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("No timeline items found.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEQ\tTYPE\tSTATUS\tTITLE\tPREVIEW\tCREATED")
	for _, item := range resp.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n",
			item.Seq,
			item.ItemType,
			item.Status,
			truncateStr(item.Title, 32),
			truncateStr(firstNonEmpty(item.Preview, item.Content), 60),
			formatTraceTime(item.CreatedAt),
		)
	}
	return tw.Flush()
}

func printGzipJSON(raw []byte) error {
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	defer gr.Close()
	data, err := io.ReadAll(gr)
	if err != nil {
		return err
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return printJSON(value)
}

func defaultTraceExportPath(traceID string) string {
	short := shortTraceID(traceID)
	return fmt.Sprintf("trace-%s-%s.json.gz", short, time.Now().UTC().Format("20060102"))
}

func shortTraceID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func shortOptionalUUID(id *uuid.UUID) string {
	if id == nil {
		return "-"
	}
	return shortTraceID(id.String())
}

func formatTraceTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(time.DateTime)
}
