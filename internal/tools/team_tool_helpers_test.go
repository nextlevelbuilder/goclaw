package tools

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestReviewOutboundMessage_UsesLocalKey(t *testing.T) {
	task := &store.TeamTaskData{
		Channel: "telegram",
		ChatID:  "-100123456",
		Metadata: map[string]any{
			TaskMetaLocalKey: "-100123456:topic:47",
		},
	}

	got := reviewOutboundMessage(task, "review needed")
	if got.Metadata == nil {
		t.Fatal("expected metadata to be populated")
	}
	if got.Metadata["local_key"] != "-100123456:topic:47" {
		t.Fatalf("local_key = %q, want %q", got.Metadata["local_key"], "-100123456:topic:47")
	}
}

func TestReviewOutboundMessage_OmitsLocalKeyWhenMissing(t *testing.T) {
	task := &store.TeamTaskData{
		Channel: "telegram",
		ChatID:  "-100123456",
	}

	got := reviewOutboundMessage(task, "review needed")
	if got.Metadata != nil {
		t.Fatalf("expected metadata to be nil, got %#v", got.Metadata)
	}
}

func TestTaskLocalKeyMetadata(t *testing.T) {
	t.Run("uses local key", func(t *testing.T) {
		task := &store.TeamTaskData{
			Metadata: map[string]any{
				TaskMetaLocalKey: "-100123456:topic:47",
			},
		}

		got := TaskLocalKeyMetadata(task)
		if got == nil {
			t.Fatal("expected metadata to be populated")
		}
		if got[TaskMetaLocalKey] != "-100123456:topic:47" {
			t.Fatalf("local_key = %q, want %q", got[TaskMetaLocalKey], "-100123456:topic:47")
		}
	})

	t.Run("omits local key when missing", func(t *testing.T) {
		if got := TaskLocalKeyMetadata(&store.TeamTaskData{}); got != nil {
			t.Fatalf("expected metadata to be nil, got %#v", got)
		}
	})
}
