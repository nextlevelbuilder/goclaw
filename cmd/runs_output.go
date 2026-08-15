package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

func printRunsList(resp runsListResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	if len(resp.Runs) == 0 {
		fmt.Println("No runs found.")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tSTATUS\tATTEMPT\tAGENT\tSESSION\tHEARTBEAT\tSTARTED\tCOMPLETED")
	for _, r := range resp.Runs {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n",
			truncateStr(r.RunID, 36),
			r.Status,
			r.Attempt,
			shortOptionalUUID(r.AgentID),
			truncateStr(r.SessionKey, 28),
			formatRunTime(r.HeartbeatAt),
			formatRunTime(r.StartedAt),
			formatRunTimePtr(r.CompletedAt),
		)
	}
	return tw.Flush()
}

func printRunsGet(resp runsGetResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	r := resp.Run
	fmt.Printf("Run:        %s\n", r.RunID)
	fmt.Printf("Status:     %s\n", r.Status)
	fmt.Printf("Attempt:    %d\n", r.Attempt)
	fmt.Printf("Session:    %s\n", r.SessionKey)
	fmt.Printf("Agent:      %s\n", shortOptionalUUID(r.AgentID))
	fmt.Printf("User:       %s\n", r.UserID)
	fmt.Printf("Channel:    %s\n", r.Channel)
	fmt.Printf("Chat:       %s\n", r.ChatID)
	fmt.Printf("Heartbeat:  %s\n", formatRunTime(r.HeartbeatAt))
	fmt.Printf("Started:    %s\n", formatRunTime(r.StartedAt))
	fmt.Printf("Completed:  %s\n", formatRunTimePtr(r.CompletedAt))
	if r.Error != "" {
		fmt.Printf("Error:      %s\n", r.Error)
	}
	return nil
}

func printRunsEvents(resp runsEventsResponse) error {
	if outputFormatIsJSON() {
		return printJSON(resp)
	}
	if len(resp.Items) == 0 {
		fmt.Println("No events found.")
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
			formatRunTime(item.CreatedAt),
		)
	}
	return tw.Flush()
}

func formatRunTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format(time.DateTime)
}

func formatRunTimePtr(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return formatRunTime(*t)
}
