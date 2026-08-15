package tools

import (
	"testing"

	"github.com/google/uuid"
)

func TestSubagentAnnounceBatchKeyScopesRoutingAndAuthority(t *testing.T) {
	base := SubagentTask{
		OriginTenantID:   uuid.New(),
		RootAgentID:      uuid.New(),
		RootAgentKey:     "root",
		OriginSessionKey: "session",
		OriginChannel:    "telegram",
		OriginChatID:     "chat",
		OriginPeerKind:   "group",
		OriginLocalKey:   "topic",
		OriginUserID:     "user",
		OriginSenderID:   "sender",
		OriginRole:       "operator",
	}
	want := subagentAnnounceBatchKey(&base)

	cases := map[string]func(*SubagentTask){
		"tenant":    func(v *SubagentTask) { v.OriginTenantID = uuid.New() },
		"root id":   func(v *SubagentTask) { v.RootAgentID = uuid.New() },
		"root key":  func(v *SubagentTask) { v.RootAgentKey += "-other" },
		"session":   func(v *SubagentTask) { v.OriginSessionKey += "-other" },
		"channel":   func(v *SubagentTask) { v.OriginChannel += "-other" },
		"chat":      func(v *SubagentTask) { v.OriginChatID += "-other" },
		"peer kind": func(v *SubagentTask) { v.OriginPeerKind += "-other" },
		"local key": func(v *SubagentTask) { v.OriginLocalKey += "-other" },
		"user":      func(v *SubagentTask) { v.OriginUserID += "-other" },
		"sender":    func(v *SubagentTask) { v.OriginSenderID += "-other" },
		"role":      func(v *SubagentTask) { v.OriginRole += "-other" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			got := base
			mutate(&got)
			if key := subagentAnnounceBatchKey(&got); key == want {
				t.Fatalf("batch key did not change when %s changed", name)
			}
		})
	}
}
