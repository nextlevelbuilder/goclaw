package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGCronStore) UpdateJob(jobID string, patch store.CronJobPatch) (*store.CronJob, error) {
	id, err := uuid.Parse(jobID)
	if err != nil {
		return nil, fmt.Errorf("invalid job ID: %s", jobID)
	}

	updates := make(map[string]any)
	if patch.Name != "" {
		updates["name"] = patch.Name
	}
	if patch.Enabled != nil {
		updates["enabled"] = *patch.Enabled
	}
	if patch.Schedule != nil {
		// Fetch current schedule to merge with patch (partial updates)
		var curKind string
		var curExpr, curTZ *string
		var curIntervalMS *int64
		var curRunAt *time.Time
		s.db.QueryRow(
			"SELECT schedule_kind, cron_expression, timezone, interval_ms, run_at FROM cron_jobs WHERE id = $1", id,
		).Scan(&curKind, &curExpr, &curTZ, &curIntervalMS, &curRunAt)

		// Resolve the effective schedule kind
		newKind := patch.Schedule.Kind
		if newKind == "" {
			newKind = curKind // keep current kind if not specified
		}
		updates["schedule_kind"] = newKind

		// Set type-specific fields and clear others
		switch newKind {
		case "cron":
			if patch.Schedule.Expr != "" {
				updates["cron_expression"] = patch.Schedule.Expr
			}
			if patch.Schedule.TZ != "" {
				if _, err := time.LoadLocation(patch.Schedule.TZ); err != nil {
					return nil, fmt.Errorf("invalid timezone: %s", patch.Schedule.TZ)
				}
				updates["timezone"] = patch.Schedule.TZ
			}
			// Clear other type fields when switching to cron
			if curKind != "cron" {
				updates["interval_ms"] = nil
				updates["run_at"] = nil
			}
		case "every":
			if patch.Schedule.EveryMS != nil {
				updates["interval_ms"] = *patch.Schedule.EveryMS
			}
			// Clear other type fields when switching to every
			if curKind != "every" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["run_at"] = nil
			}
		case "at":
			if patch.Schedule.AtMS != nil {
				t := time.UnixMilli(*patch.Schedule.AtMS)
				updates["run_at"] = t
			}
			// Clear other type fields when switching to at
			if curKind != "at" {
				updates["cron_expression"] = nil
				updates["timezone"] = nil
				updates["interval_ms"] = nil
			}
		}

		// Build merged schedule for recomputing next_run_at
		merged := store.CronSchedule{Kind: newKind}
		switch newKind {
		case "cron":
			if patch.Schedule.Expr != "" {
				merged.Expr = patch.Schedule.Expr
			} else if curExpr != nil {
				merged.Expr = *curExpr
			}
			if patch.Schedule.TZ != "" {
				merged.TZ = patch.Schedule.TZ
			} else if curTZ != nil && newKind == curKind {
				merged.TZ = *curTZ
			}
		case "every":
			if patch.Schedule.EveryMS != nil {
				merged.EveryMS = patch.Schedule.EveryMS
			} else if curIntervalMS != nil {
				merged.EveryMS = curIntervalMS
			}
		case "at":
			if patch.Schedule.AtMS != nil {
				merged.AtMS = patch.Schedule.AtMS
			} else if curRunAt != nil {
				ms := curRunAt.UnixMilli()
				merged.AtMS = &ms
			}
		}

		// Validate the merged schedule before applying
		switch merged.Kind {
		case "cron":
			if merged.Expr == "" {
				return nil, fmt.Errorf("cron schedule requires expr")
			}
			gx := gronx.New()
			if !gx.IsValid(merged.Expr) {
				return nil, fmt.Errorf("invalid cron expression: %s", merged.Expr)
			}
		case "every":
			if merged.EveryMS == nil || *merged.EveryMS <= 0 {
				return nil, fmt.Errorf("every schedule requires positive everyMs")
			}
		case "at":
			if merged.AtMS == nil {
				return nil, fmt.Errorf("at schedule requires atMs")
			}
		}

		next := computeNextRun(&merged, time.Now(), s.defaultTZ)
		updates["next_run_at"] = next
	}
	if patch.DeleteAfterRun != nil {
		updates["delete_after_run"] = *patch.DeleteAfterRun
	}

	// Update agent_id column
	if patch.AgentID != nil {
		if *patch.AgentID == "" {
			updates["agent_id"] = nil
		} else if aid, parseErr := uuid.Parse(*patch.AgentID); parseErr == nil {
			updates["agent_id"] = aid
		}
	}

	// Update payload JSONB — fetch current, merge patch fields, re-serialize
	needsPayloadUpdate := patch.Message != "" || patch.Deliver != nil || patch.Channel != nil || patch.To != nil || patch.WakeHeartbeat != nil
	if needsPayloadUpdate {
		var payloadJSON []byte
		if scanErr := s.db.QueryRow("SELECT payload FROM cron_jobs WHERE id = $1", id).Scan(&payloadJSON); scanErr == nil {
			var payload store.CronPayload
			json.Unmarshal(payloadJSON, &payload)

			if patch.Message != "" {
				payload.Message = patch.Message
			}
			if patch.Deliver != nil {
				payload.Deliver = *patch.Deliver
			}
			if patch.Channel != nil {
				payload.Channel = *patch.Channel
			}
			if patch.To != nil {
				payload.To = *patch.To
			}
			if patch.WakeHeartbeat != nil {
				payload.WakeHeartbeat = *patch.WakeHeartbeat
			}

			merged, _ := json.Marshal(payload)
			updates["payload"] = merged
		}
	}

	updates["updated_at"] = time.Now()

	if err := execMapUpdate(context.Background(), s.db, "cron_jobs", id, updates); err != nil {
		return nil, err
	}

	s.cacheLoaded = false
	job, _ := s.scanJob(id)
	return job, nil
}
