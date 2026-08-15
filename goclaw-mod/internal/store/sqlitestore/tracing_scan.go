//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func scanTraceRow(row *sql.Row) (*store.TraceData, error) {
	var d store.TraceData
	var parentTraceID, agentID, teamID *uuid.UUID
	var userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string
	var endTime nullSqliteTime
	var durationMS *int
	var metadata *[]byte
	var tags []byte
	var startTime, createdAt sqliteTime

	err := row.Scan(&d.ID, &parentTraceID, &agentID, &userID, &sessionKey, &runID, &startTime, &endTime,
		&durationMS, &name, &channel, &inputPreview, &outputPreview,
		&d.TotalInputTokens, &d.TotalOutputTokens, &d.TotalCost, &d.SpanCount, &d.LLMCallCount, &d.ToolCallCount,
		&d.Status, &errStr, &metadata, &tags, &teamID, &createdAt)
	if err != nil {
		return nil, err
	}
	d.StartTime = startTime.Time
	d.CreatedAt = createdAt.Time
	var endTimePtr *time.Time
	if endTime.Valid {
		endTimePtr = &endTime.Time
	}
	applyTraceNullables(&d, parentTraceID, agentID, teamID, userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr, endTimePtr, durationMS, metadata, tags)
	return &d, nil
}

func scanTraceRows(rows *sql.Rows) ([]store.TraceData, error) {
	var result []store.TraceData
	for rows.Next() {
		var d store.TraceData
		var parentTraceID, agentID, teamID *uuid.UUID
		var userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string
		var endTime nullSqliteTime
		var durationMS *int
		var metadata *[]byte
		var tags []byte
		var startTime, createdAt sqliteTime

		if err := rows.Scan(&d.ID, &parentTraceID, &agentID, &userID, &sessionKey, &runID, &startTime, &endTime,
			&durationMS, &name, &channel, &inputPreview, &outputPreview,
			&d.TotalInputTokens, &d.TotalOutputTokens, &d.TotalCost, &d.SpanCount, &d.LLMCallCount, &d.ToolCallCount,
			&d.Status, &errStr, &metadata, &tags, &teamID, &createdAt); err != nil {
			slog.Warn("tracing: trace scan failed", "error", err)
			continue
		}
		d.StartTime = startTime.Time
		d.CreatedAt = createdAt.Time
		var endTimePtr *time.Time
	if endTime.Valid {
		endTimePtr = &endTime.Time
	}
	applyTraceNullables(&d, parentTraceID, agentID, teamID, userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr, endTimePtr, durationMS, metadata, tags)
		result = append(result, d)
	}
	return result, rows.Err()
}

func applyTraceNullables(d *store.TraceData,
	parentTraceID, agentID, teamID *uuid.UUID,
	userID, sessionKey, runID, name, channel, inputPreview, outputPreview, errStr *string,
	endTime *time.Time, durationMS *int, metadata *[]byte, tags []byte,
) {
	d.ParentTraceID = parentTraceID
	d.AgentID = agentID
	d.TeamID = teamID
	d.UserID = derefStr(userID)
	d.SessionKey = derefStr(sessionKey)
	d.RunID = derefStr(runID)
	d.EndTime = endTime
	if durationMS != nil {
		d.DurationMS = *durationMS
	}
	d.Name = derefStr(name)
	d.Channel = derefStr(channel)
	d.InputPreview = derefStr(inputPreview)
	d.OutputPreview = derefStr(outputPreview)
	d.Error = derefStr(errStr)
	if metadata != nil {
		d.Metadata = *metadata
	}
	scanJSONStringArray(tags, &d.Tags)
}
