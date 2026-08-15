package store

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestAgentAudioFromCtx_EmptyContext(t *testing.T) {
	t.Parallel()
	_, ok := AgentAudioFromCtx(context.Background())
	if ok {
		t.Error("expected ok=false on bare context.Background()")
	}
}

func TestAgentAudioFromCtx_RoundTrip(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	raw := json.RawMessage(`{"tts_voice_id":"V1"}`)
	snap := AgentAudioSnapshot{
		AgentID:     id,
		OtherConfig: append([]byte(nil), raw...),
	}
	ctx := WithAgentAudio(context.Background(), snap)
	got, ok := AgentAudioFromCtx(ctx)
	if !ok {
		t.Fatal("expected ok=true after WithAgentAudio")
	}
	if got.AgentID != id {
		t.Errorf("AgentID: got %v, want %v", got.AgentID, id)
	}
	if !bytes.Equal(got.OtherConfig, raw) {
		t.Errorf("OtherConfig: got %q, want %q", got.OtherConfig, raw)
	}
}

func TestAgentAudioFromCtx_NestedOverride(t *testing.T) {
	t.Parallel()
	id1 := uuid.New()
	id2 := uuid.New()
	snap1 := AgentAudioSnapshot{AgentID: id1, OtherConfig: json.RawMessage(`{"v":"1"}`)}
	snap2 := AgentAudioSnapshot{AgentID: id2, OtherConfig: json.RawMessage(`{"v":"2"}`)}

	ctx := WithAgentAudio(context.Background(), snap1)
	ctx = WithAgentAudio(ctx, snap2)

	got, ok := AgentAudioFromCtx(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if got.AgentID != id2 {
		t.Errorf("expected child snap (id2=%v), got %v", id2, got.AgentID)
	}
}

func TestAgentAudioFromCtx_ImmutabilityAfterInsertion(t *testing.T) {
	t.Parallel()
	srcBytes := []byte(`{"tts_voice_id":"V1"}`)

	// Producer does a defensive copy before calling WithAgentAudio.
	snap := AgentAudioSnapshot{
		AgentID:     uuid.New(),
		OtherConfig: append([]byte(nil), srcBytes...),
	}
	ctx := WithAgentAudio(context.Background(), snap)

	// Mutate the source slice AFTER insertion.
	srcBytes[0] = 'X'

	got, ok := AgentAudioFromCtx(ctx)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// The stored snapshot must NOT reflect the mutation.
	if got.OtherConfig[0] == 'X' {
		t.Error("snapshot OtherConfig was mutated — defensive copy missing in WithAgentAudio or producer")
	}
}

func TestAgentAudioFromCtx_NilUUIDReturnsFalse(t *testing.T) {
	t.Parallel()
	// Inserting a snapshot with uuid.Nil should cause AgentAudioFromCtx to return ok=false.
	snap := AgentAudioSnapshot{AgentID: uuid.Nil}
	ctx := WithAgentAudio(context.Background(), snap)
	_, ok := AgentAudioFromCtx(ctx)
	if ok {
		t.Error("expected ok=false when AgentID is uuid.Nil")
	}
}
