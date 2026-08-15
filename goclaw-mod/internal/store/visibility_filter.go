package store

import (
	"context"
	"strings"
)

// IsSkillVisibleTo returns true if the caller identified by ctx can discover
// the given skill. Rules:
//   - System skills are visible to everyone.
//   - Empty or "public" visibility is treated as public (legacy rows default
//     to "public" for safety since older stores did not enforce the field).
//   - "private" skills are only visible to the owner. Three identity strings
//     are considered (actor, user, sender) to match the same identities
//     isOwnerOfSkill checks for backward compatibility (#915).
//   - "internal" skills require explicit grants, which are evaluated in
//     ListAccessible; this baseline helper hides them.
//
// Admin/master-scope bypass is the caller's responsibility — this helper
// reflects the non-privileged baseline.
func IsSkillVisibleTo(ctx context.Context, ownerID, visibility string, isSystem bool) bool {
	if isSystem {
		return true
	}
	// Normalize to defend against historical rows with mixed case / whitespace
	// that bypassed the write-path normalizer.
	switch strings.ToLower(strings.TrimSpace(visibility)) {
	case "", "public":
		return true
	case "private":
		if ownerID == "" {
			// No owner recorded — treat as public (historical data).
			return true
		}
		actorID := ActorIDFromContext(ctx)
		userID := UserIDFromContext(ctx)
		senderID := SenderIDFromContext(ctx)
		return ownerID == actorID || ownerID == userID || ownerID == senderID
	case "internal":
		return false
	default:
		// Unknown enum value: fail closed (hide).
		return false
	}
}

// FilterVisibleSkills returns skills the caller can discover. Uses
// IsSkillVisibleTo for each entry.
func FilterVisibleSkills(ctx context.Context, skills []SkillInfo) []SkillInfo {
	out := make([]SkillInfo, 0, len(skills))
	for _, s := range skills {
		if IsSkillVisibleTo(ctx, s.OwnerID, s.Visibility, s.IsSystem) {
			out = append(out, s)
		}
	}
	return out
}
