package tools

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
)

func TestSubagentDirectAnnouncementDoesNotBlockOnFullInboundBus(t *testing.T) {
	messageBus := bus.New()
	for range 1000 {
		messageBus.PublishInbound(bus.InboundMessage{Content: "fill"})
	}
	manager := NewSubagentManager(nil, nil, "", messageBus, nil, SubagentConfig{})
	taskStore := newRecordingSubagentTaskStore()
	manager.SetTaskStore(taskStore)
	task := &SubagentTask{
		ID:               "task-1",
		Label:            "probe",
		Status:           TaskStatusCompleted,
		Result:           "done",
		CreatedAt:        time.Now().Add(-time.Second).UnixMilli(),
		OriginChannel:    "test",
		OriginChatID:     "chat-1",
		OriginTenantID:   uuid.New(),
		RootAgentID:      uuid.New(),
		RootAgentKey:     "root",
		OriginSessionKey: "session-1",
		dbID:             uuid.New(),
	}

	done := make(chan struct{})
	go func() {
		manager.announceTask(context.Background(), task, nil, 1, true)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subagent announcement blocked on a full inbound bus")
	}
	select {
	case metadata := <-taskStore.metadata:
		if metadata[asyncCompletionDeliveryKey] != asyncCompletionDeliveryMissed {
			t.Fatalf("announcement metadata = %#v, want undelivered", metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("subagent missed announcement was not recorded after bus saturation")
	}
}
