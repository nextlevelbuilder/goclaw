package cmd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type scriptedSubagentTaskRecovery struct {
	mu        sync.Mutex
	failures  int
	recovered int64
	calls     int
}

func (s *scriptedSubagentTaskRecovery) RecoverInterrupted(ctx context.Context) (int64, error) {
	if !store.IsMasterScope(ctx) || store.TenantIDFromContext(ctx) != store.MasterTenantID {
		return 0, errors.New("recovery did not receive explicit master scope")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls <= s.failures {
		return 0, errors.New("database temporarily unavailable")
	}
	return s.recovered, nil
}

func (s *scriptedSubagentTaskRecovery) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestRecoverInterruptedSubagentTasksRetriesBeforeTraffic(t *testing.T) {
	recovery := &scriptedSubagentTaskRecovery{failures: 2, recovered: 3}
	stores := &store.Stores{SubagentTaskRecovery: recovery}
	recovered, err := recoverInterruptedSubagentTasks(
		context.Background(), stores, time.Millisecond,
	)
	if err != nil {
		t.Fatalf("recoverInterruptedSubagentTasks: %v", err)
	}
	if recovered != 3 || recovery.callCount() != 3 {
		t.Fatalf("recovery = (%d rows, %d calls), want (3, 3)", recovered, recovery.callCount())
	}
}

func TestRecoverInterruptedSubagentTasksStopsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recovery := &scriptedSubagentTaskRecovery{failures: 1}
	stores := &store.Stores{SubagentTaskRecovery: recovery}
	_, err := recoverInterruptedSubagentTasks(ctx, stores, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("recovery error = %v, want context.Canceled", err)
	}
	if recovery.callCount() != 1 {
		t.Fatalf("recovery calls = %d, want 1", recovery.callCount())
	}
}
