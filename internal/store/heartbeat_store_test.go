package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNextHeartbeatRunAt_InitialScheduleUsesDeterministicStagger(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	agentID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	intervalSec := 1800

	want := now.Add(time.Duration(intervalSec)*time.Second + StaggerOffset(agentID, intervalSec))
	got := NextHeartbeatRunAt(now, agentID, intervalSec, nil)

	if !got.Equal(want) {
		t.Fatalf("NextHeartbeatRunAt initial schedule = %v, want %v", got, want)
	}
}

func TestNextHeartbeatRunAt_AdvancesFromAnchorWithoutDrift(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	agentID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	intervalSec := 300
	anchor := now.Add(-7 * time.Minute)

	want := now.Add(3 * time.Minute)
	got := NextHeartbeatRunAt(now, agentID, intervalSec, &anchor)

	if !got.Equal(want) {
		t.Fatalf("NextHeartbeatRunAt anchored schedule = %v, want %v", got, want)
	}
}

func TestNextHeartbeatRunAt_CapsCatchUpAfterLongDowntime(t *testing.T) {
	now := time.Date(2026, time.March, 28, 12, 0, 0, 0, time.UTC)
	agentID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	intervalSec := 300
	anchor := now.Add(-31 * time.Minute)

	want := now.Add(5 * time.Minute)
	got := NextHeartbeatRunAt(now, agentID, intervalSec, &anchor)

	if !got.Equal(want) {
		t.Fatalf("NextHeartbeatRunAt catch-up schedule = %v, want %v", got, want)
	}
}
