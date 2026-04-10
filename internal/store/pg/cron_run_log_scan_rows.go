package pg

import (
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// cronRunLogRow is an sqlx scan struct for cron_run_logs SELECT queries.
// All store.CronRunLogEntry fields are db:"-" so a dedicated row struct is required.
type cronRunLogRow struct {
	JobID        uuid.UUID  `db:"job_id"`
	Status       string     `db:"status"`
	Error        *string    `db:"error"`
	Summary      *string    `db:"summary"`
	RanAt        time.Time  `db:"ran_at"`
	DurationMS   int64      `db:"duration_ms"`
	InputTokens  int        `db:"input_tokens"`
	OutputTokens int        `db:"output_tokens"`
}

// toCronRunLogEntry converts a cronRunLogRow to store.CronRunLogEntry.
func (r *cronRunLogRow) toCronRunLogEntry() store.CronRunLogEntry {
	return store.CronRunLogEntry{
		Ts:           r.RanAt.UnixMilli(),
		JobID:        r.JobID.String(),
		Status:       r.Status,
		Error:        derefStr(r.Error),
		Summary:      derefStr(r.Summary),
		DurationMS:   r.DurationMS,
		InputTokens:  r.InputTokens,
		OutputTokens: r.OutputTokens,
	}
}
