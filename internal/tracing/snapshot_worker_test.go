package tracing

import (
	"testing"
	"time"
)

func TestUsageCatchUpStartHourRefreshesLatestClosedBucket(t *testing.T) {
	target := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)
	latest := target

	got := usageCatchUpStartHour(&latest, target)
	want := target.Add(-recentUsageRefreshWindow)
	if !got.Equal(want) {
		t.Fatalf("start hour = %s, want %s", got, want)
	}
}

func TestUsageCatchUpStartHourContinuesLargeBacklog(t *testing.T) {
	target := time.Date(2026, 7, 3, 6, 0, 0, 0, time.UTC)
	latest := target.Add(-12 * time.Hour)

	got := usageCatchUpStartHour(&latest, target)
	want := latest.Add(time.Hour)
	if !got.Equal(want) {
		t.Fatalf("start hour = %s, want %s", got, want)
	}
}

func TestUsageCatchUpStartHourWithoutSnapshotsComputesPreviousHourOnly(t *testing.T) {
	target := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)

	got := usageCatchUpStartHour(nil, target)
	if !got.Equal(target) {
		t.Fatalf("start hour = %s, want %s", got, target)
	}
}
