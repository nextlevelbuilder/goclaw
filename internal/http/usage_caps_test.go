package http

import (
	"testing"

	"github.com/google/uuid"
)

func TestUsageCapPolicyBodyRejectsInvalidUUID(t *testing.T) {
	invalid := "not-a-uuid"
	if _, err := (policyBody{AgentID: &invalid}).toPolicy(uuid.New()); err == nil {
		t.Fatal("toPolicy accepted invalid agent_id")
	}
	if _, err := (policyBody{ProviderID: &invalid}).toPatch(); err == nil {
		t.Fatal("toPatch accepted invalid provider_id")
	}
}
