package pg

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// The sort helpers only exist to make concurrent writers take row locks in the
// same order (ON CONFLICT deadlocks otherwise). The observable invariant is that
// the resulting order does not depend on the input order.

func snapshotRows() []store.UsageSnapshot {
	hour := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	agentA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	agentB := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	mk := func(h time.Time, agent *uuid.UUID, provider, model, channel string) store.UsageSnapshot {
		return store.UsageSnapshot{
			BucketHour: h,
			AgentID:    agent,
			Provider:   provider,
			Model:      model,
			Channel:    channel,
		}
	}
	return []store.UsageSnapshot{
		mk(hour.Add(time.Hour), nil, "", "", "telegram"),
		mk(hour, &agentB, "openai", "gpt-4o", "telegram"),
		mk(hour, nil, "", "", "zalo"),
		mk(hour, &agentA, "openai", "gpt-4o", "zalo"),
		mk(hour, &agentA, "openai", "gpt-4o", "telegram"),
		mk(hour, nil, "", "", "telegram"),
		mk(hour, &agentA, "anthropic", "claude", "telegram"),
	}
}

func snapshotKey(s store.UsageSnapshot) string {
	agent := uuid.Nil
	if s.AgentID != nil {
		agent = *s.AgentID
	}
	return s.BucketHour.UTC().Format(time.RFC3339) + "|" + agent.String() + "|" +
		s.Provider + "|" + s.Model + "|" + s.Channel
}

func TestSortSnapshotsByConflictKeyIsOrderIndependent(t *testing.T) {
	rows := snapshotRows()
	ascending := append([]store.UsageSnapshot(nil), rows...)
	sortSnapshotsByConflictKey(ascending)

	reversed := make([]store.UsageSnapshot, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		reversed = append(reversed, rows[i])
	}
	sortSnapshotsByConflictKey(reversed)

	for i := range ascending {
		if got, want := snapshotKey(reversed[i]), snapshotKey(ascending[i]); got != want {
			t.Fatalf("row %d order differs: reversed=%s ascending=%s", i, got, want)
		}
	}
}

func TestSortSnapshotsByConflictKeyOrdersBucketsThenKeys(t *testing.T) {
	hour := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	nilAgent := func(provider, model, channel string) store.UsageSnapshot {
		return store.UsageSnapshot{BucketHour: hour, Provider: provider, Model: model, Channel: channel}
	}
	agent := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	rows := []store.UsageSnapshot{
		{BucketHour: hour.Add(time.Hour), Channel: "telegram"},
		nilAgent("openai", "gpt-4o", "zalo"),
		{BucketHour: hour, AgentID: &agent, Channel: "telegram"},
		nilAgent("openai", "gpt-4o", "telegram"),
		nilAgent("", "", "telegram"),
		nilAgent("", "", "zalo"),
	}
	sortSnapshotsByConflictKey(rows)

	want := []string{
		snapshotKey(nilAgent("", "", "telegram")), // nil agent = zero UUID sorts first
		snapshotKey(nilAgent("", "", "zalo")),
		snapshotKey(nilAgent("openai", "gpt-4o", "telegram")),
		snapshotKey(nilAgent("openai", "gpt-4o", "zalo")),
		snapshotKey(store.UsageSnapshot{BucketHour: hour, AgentID: &agent, Channel: "telegram"}),
		snapshotKey(store.UsageSnapshot{BucketHour: hour.Add(time.Hour), Channel: "telegram"}),
	}
	for i, w := range want {
		if got := snapshotKey(rows[i]); got != w {
			t.Fatalf("row %d = %s, want %s", i, got, w)
		}
	}
}

func rollupRows() []store.UsageEventRollup {
	hour := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	tenant := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	agent := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	mk := func(bucket time.Time, agentID *uuid.UUID, eventType, status string) store.UsageEventRollup {
		return store.UsageEventRollup{
			TenantID:     tenant,
			BucketHour:   bucket,
			EventType:    eventType,
			ResourceType: "llm",
			ResourceName: "run",
			Source:       "agent",
			AgentID:      agentID,
			Channel:      "telegram",
			Provider:     "openai",
			Model:        "gpt-4o",
			Status:       status,
		}
	}
	return []store.UsageEventRollup{
		mk(hour, &agent, "llm_call", "ok"),
		mk(hour, nil, "llm_call", "ok"),
		mk(hour.Add(time.Hour), nil, "llm_call", "ok"),
		mk(hour, nil, "llm_call", "error"),
	}
}

func rollupKey(r store.UsageEventRollup) string {
	agent := uuid.Nil
	if r.AgentID != nil {
		agent = *r.AgentID
	}
	return r.TenantID.String() + "|" + r.BucketHour.UTC().Format(time.RFC3339) + "|" +
		r.EventType + "|" + r.ResourceType + "|" + r.ResourceName + "|" + r.Source + "|" +
		agent.String() + "|" + r.Channel + "|" + r.Provider + "|" + r.Model + "|" + r.Status
}

func TestSortRollupsByConflictKeyIsOrderIndependent(t *testing.T) {
	rows := rollupRows()
	ascending := append([]store.UsageEventRollup(nil), rows...)
	sortRollupsByConflictKey(ascending)

	rotated := append(append([]store.UsageEventRollup(nil), rows[2:]...), rows[:2]...)
	sortRollupsByConflictKey(rotated)

	for i := range ascending {
		if got, want := rollupKey(rotated[i]), rollupKey(ascending[i]); got != want {
			t.Fatalf("row %d order differs: rotated=%s ascending=%s", i, got, want)
		}
	}
	if got := rollupKey(ascending[0]); got != rollupKey(rollupRows()[3]) {
		t.Fatalf("first row = %s, want the nil-agent error row of the earliest bucket", got)
	}
}
