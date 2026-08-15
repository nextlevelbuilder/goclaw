//go:build !windows

package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func commandCronConfig(enabled bool) *config.Config {
	c := &config.Config{}
	c.Cron.CommandEnabled = enabled
	return c
}

func commandCronJob(spec *store.CronCommandSpec, deliver bool) *store.CronJob {
	job := &store.CronJob{
		ID:       uuid.NewString(),
		TenantID: uuid.New(),
		Name:     "probe",
		AgentID:  "ops",
		UserID:   "user-1",
		Payload:  store.CronPayload{Kind: store.CronPayloadKindCommand, Command: spec},
	}
	if deliver {
		job.Deliver = true
		job.DeliverChannel = "telegram"
		job.DeliverTo = "chat-1"
	}
	return job
}

// A command payload must be refused unless cron.command_enabled is set.
func TestCronJobHandler_CommandDisabled(t *testing.T) {
	handler := makeCronJobHandler(nil, nil, commandCronConfig(false), nil, nil, nil, nil, nil, nil)
	if _, err := handler(commandCronJob(&store.CronCommandSpec{Argv: []string{"sh", "-c", "echo hi"}}, false)); err == nil {
		t.Fatal("expected error when cron.command_enabled is false")
	}
}

// A successful command runs with zero model tokens and its stdout is delivered.
func TestCronJobHandler_CommandSuccessDelivers(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	handler := makeCronJobHandler(nil, mb, commandCronConfig(true), nil, nil, nil, nil, nil, nil)
	result, err := handler(commandCronJob(&store.CronCommandSpec{Argv: []string{"sh", "-c", "printf hello"}}, true))
	if err != nil {
		t.Fatalf("command cron returned error: %v", err)
	}
	if result == nil || result.Content != "hello" {
		t.Fatalf("result = %#v, want content hello", result)
	}
	if result.InputTokens != 0 || result.OutputTokens != 0 {
		t.Errorf("command cron must report zero tokens, got in=%d out=%d", result.InputTokens, result.OutputTokens)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound delivery of command output")
	}
	if got.Content != "hello" || got.Channel != "telegram" || got.ChatID != "chat-1" {
		t.Fatalf("outbound = %#v, want telegram/chat-1/hello", got)
	}
}

// A non-zero exit returns an error (recorded as a failed run) and is NOT
// delivered — only successful output is announced.
func TestCronJobHandler_CommandFailureNotDelivered(t *testing.T) {
	mb := bus.New()
	defer mb.Close()

	handler := makeCronJobHandler(nil, mb, commandCronConfig(true), nil, nil, nil, nil, nil, nil)
	result, err := handler(commandCronJob(&store.CronCommandSpec{Argv: []string{"sh", "-c", "echo boom 1>&2; exit 3"}}, true))
	if err == nil {
		t.Fatal("expected error for non-zero command exit")
	}
	if result != nil {
		t.Fatalf("failed command should return nil result, got %#v", result)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if got, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatalf("failed command must not deliver, got %#v", got)
	}
}

// An empty argv is rejected before execution.
func TestCronJobHandler_CommandInvalidSpec(t *testing.T) {
	handler := makeCronJobHandler(nil, nil, commandCronConfig(true), nil, nil, nil, nil, nil, nil)
	if _, err := handler(commandCronJob(&store.CronCommandSpec{}, false)); err == nil {
		t.Fatal("expected error for empty argv")
	}
}
