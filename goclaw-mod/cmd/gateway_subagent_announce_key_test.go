package cmd

import (
	"testing"

	"github.com/google/uuid"
)

func TestSubagentAnnounceRoutingKeyScopesRoutingAndAuthority(t *testing.T) {
	base := subagentAnnounceRouting{
		TenantID:     uuid.New(),
		RootAgentID:  uuid.New(),
		ParentAgent:  "root",
		SessionKey:   "session",
		OrigChannel:  "telegram",
		OrigChatID:   "chat",
		OrigPeerKind: "group",
		OrigLocalKey: "topic",
		UserID:       "user",
		SenderID:     "sender",
		Role:         "operator",
	}
	want := subagentAnnounceRoutingKey(base)

	cases := map[string]func(*subagentAnnounceRouting){
		"tenant":       func(v *subagentAnnounceRouting) { v.TenantID = uuid.New() },
		"root id":      func(v *subagentAnnounceRouting) { v.RootAgentID = uuid.New() },
		"parent agent": func(v *subagentAnnounceRouting) { v.ParentAgent += "-other" },
		"session":      func(v *subagentAnnounceRouting) { v.SessionKey += "-other" },
		"channel":      func(v *subagentAnnounceRouting) { v.OrigChannel += "-other" },
		"chat":         func(v *subagentAnnounceRouting) { v.OrigChatID += "-other" },
		"peer kind":    func(v *subagentAnnounceRouting) { v.OrigPeerKind += "-other" },
		"local key":    func(v *subagentAnnounceRouting) { v.OrigLocalKey += "-other" },
		"user":         func(v *subagentAnnounceRouting) { v.UserID += "-other" },
		"sender":       func(v *subagentAnnounceRouting) { v.SenderID += "-other" },
		"role":         func(v *subagentAnnounceRouting) { v.Role += "-other" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if key := subagentAnnounceRoutingKey(got); key == want {
				t.Fatalf("routing key did not change when %s changed", name)
			}
		})
	}
}
