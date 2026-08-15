package cmd

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

type recordingGatewayTaskStore struct {
	metadata chan map[string]any
}

func (*recordingGatewayTaskStore) Create(context.Context, *store.SubagentTaskData) error {
	return nil
}
func (*recordingGatewayTaskStore) Get(context.Context, uuid.UUID, uuid.UUID) (*store.SubagentTaskData, error) {
	return nil, nil
}
func (*recordingGatewayTaskStore) UpdateStatus(context.Context, uuid.UUID, uuid.UUID, string, *string, int, int64, int64) error {
	return nil
}
func (*recordingGatewayTaskStore) ListByParent(context.Context, uuid.UUID, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (*recordingGatewayTaskStore) ListBySession(context.Context, uuid.UUID, string) ([]store.SubagentTaskData, error) {
	return nil, nil
}
func (*recordingGatewayTaskStore) Archive(context.Context, uuid.UUID, time.Duration, int) (int64, error) {
	return 0, nil
}
func (s *recordingGatewayTaskStore) UpdateMetadata(_ context.Context, _ uuid.UUID, _ uuid.UUID, metadata map[string]any) error {
	s.metadata <- metadata
	return nil
}

func TestSubagentBatchAnnouncementDoesNotBlockOnFullInboundBus(t *testing.T) {
	messageBus := bus.New()
	for range 1000 {
		messageBus.PublishInbound(bus.InboundMessage{Content: "fill"})
	}
	manager := tools.NewSubagentManager(nil, nil, "", messageBus, nil, tools.SubagentConfig{})
	taskStore := &recordingGatewayTaskStore{metadata: make(chan map[string]any, 1)}
	manager.SetTaskStore(taskStore)
	callback := makeDelegateAnnounceCallback(manager, messageBus)
	completionID := uuid.New()

	done := make(chan struct{})
	go func() {
		callback(
			"session-1",
			[]tools.AnnounceQueueItem{{
				SubagentID:       "task-1",
				CompletionID:     completionID,
				DurablyPersisted: true,
				Label:            "probe",
				Status:           tools.TaskStatusCompleted,
				Result:           "done",
			}},
			tools.AnnounceMetadata{
				OriginChatID:     "chat-1",
				OriginSessionKey: "session-1",
				OriginTenantID:   uuid.New(),
				RootAgentID:      uuid.New(),
				ParentAgent:      "root",
			},
		)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("batched subagent announcement blocked on a full inbound bus")
	}
	select {
	case metadata := <-taskStore.metadata:
		if metadata["announcement_status"] != "undelivered" {
			t.Fatalf("announcement metadata = %#v, want undelivered", metadata)
		}
	case <-time.After(time.Second):
		t.Fatal("batched missed announcement was not recorded after bus saturation")
	}
}
