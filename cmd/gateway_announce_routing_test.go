package cmd

import (
	"testing"

	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// TestAnnounceRouting_PropagatesSenderAndRole guards against the regression
// where team-task completion announces drop SenderID/Role on re-ingress to
// the Lead session — the failure mode reported as
// `permission denied: system context cannot write files in group chats`
// when the Lead tries write_file inside the announce-triggered turn.
//
// team_tool_dispatch.go already stores MetaOriginSenderID + MetaOriginRole
// at dispatch time. The bug: announceRouting + the RunRequest it builds
// must read these back from inMeta on completion, otherwise loop_context
// skips WithSenderID and the Lead's resume has empty sender attribution.
func TestAnnounceRouting_PropagatesSenderAndRole(t *testing.T) {
	const (
		realSender = "5218954741"   // Telegram numeric user id
		realRole   = "admin"
		realUserID = "group:telegram:-1003812294018"
	)

	// Simulate what consumer_handlers.go reads from a teammate-message inMeta.
	inMeta := map[string]string{
		tools.MetaOriginSenderID: realSender,
		tools.MetaOriginRole:     realRole,
		tools.MetaOriginUserID:   realUserID,
		tools.MetaTeamID:         "019d8a59-6e40-730f-89b2-8a41b7e1fad2",
	}

	r := announceRouting{
		OriginUserID:   inMeta[tools.MetaOriginUserID],
		OriginSenderID: inMeta[tools.MetaOriginSenderID],
		OriginRole:     inMeta[tools.MetaOriginRole],
		TeamID:         inMeta[tools.MetaTeamID],
	}

	if r.OriginSenderID != realSender {
		t.Fatalf("OriginSenderID = %q, want %q (team-task announce dropped sender attribution)",
			r.OriginSenderID, realSender)
	}
	if r.OriginRole != realRole {
		t.Fatalf("OriginRole = %q, want %q (team-task announce dropped RBAC role)",
			r.OriginRole, realRole)
	}
	if r.OriginUserID != realUserID {
		t.Fatalf("OriginUserID = %q, want %q", r.OriginUserID, realUserID)
	}
}

// TestAnnounceRouting_EmptyMetaPropagatesEmpty asserts the wire-through is
// faithful when upstream legitimately has no sender (e.g. a system-initiated
// dispatch). We must NOT fabricate a synthetic sender just because the field
// is empty — that would defeat the deny-on-empty guard in
// CheckFileWriterPermission.
func TestAnnounceRouting_EmptyMetaPropagatesEmpty(t *testing.T) {
	inMeta := map[string]string{
		tools.MetaTeamID: "team-uuid",
	}
	r := announceRouting{
		OriginUserID:   inMeta[tools.MetaOriginUserID],
		OriginSenderID: inMeta[tools.MetaOriginSenderID],
		OriginRole:     inMeta[tools.MetaOriginRole],
	}
	if r.OriginSenderID != "" {
		t.Errorf("OriginSenderID = %q, want empty (no upstream sender to propagate)", r.OriginSenderID)
	}
	if r.OriginRole != "" {
		t.Errorf("OriginRole = %q, want empty", r.OriginRole)
	}
}
