package http

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

type effectiveAccessIndex struct {
	actorID          string
	accessibleBySlug map[string]bool
	agentGrantByID   map[string]store.SkillWithGrantStatus
	agentGrantBySlug map[string]store.SkillWithGrantStatus
}

func (h *SkillsHandler) buildEffectiveAccessIndex(r interface{ Context() context.Context }, agentID uuid.UUID, userID string) (effectiveAccessIndex, error) {
	ctx := r.Context()
	accessStore, ok := h.skills.(store.SkillAccessStore)
	if !ok {
		return effectiveAccessIndex{}, fmt.Errorf("skill access store not available")
	}
	accessible, err := accessStore.ListAccessible(ctx, agentID, userID)
	if err != nil {
		return effectiveAccessIndex{}, err
	}
	grantStatus, err := h.skills.ListWithGrantStatus(ctx, agentID)
	if err != nil {
		return effectiveAccessIndex{}, err
	}
	idx := effectiveAccessIndex{
		actorID:          store.ActorIDFromContext(ctx),
		accessibleBySlug: make(map[string]bool, len(accessible)),
		agentGrantByID:   make(map[string]store.SkillWithGrantStatus, len(grantStatus)),
		agentGrantBySlug: make(map[string]store.SkillWithGrantStatus, len(grantStatus)),
	}
	if idx.actorID == "" {
		idx.actorID = userID
	}
	for _, sk := range accessible {
		idx.accessibleBySlug[sk.Slug] = true
	}
	for _, grant := range grantStatus {
		idx.agentGrantByID[grant.ID.String()] = grant
		idx.agentGrantBySlug[grant.Slug] = grant
	}
	return idx, nil
}

func effectiveAccessForSkill(ctx context.Context, sk store.SkillInfo, idx effectiveAccessIndex, userID string) skillEffectiveAccessResponse {
	resp := skillEffectiveAccessResponse{Skill: refForSkill(sk), Reason: "none"}
	if sk.Status != "active" || !sk.Enabled {
		resp.Reason = "inactive"
		return resp
	}
	if !idx.accessible(sk) {
		return resp
	}
	if sk.IsSystem {
		resp.Accessible = true
		resp.Reason = "system"
		return resp
	}
	if sk.Visibility == skills.VisibilityPublic {
		resp.Accessible = true
		resp.Reason = "public"
		return resp
	}
	actorID := idx.actorID
	if actorID == "" {
		actorID = store.ActorIDFromContext(ctx)
	}
	if actorID == "" {
		actorID = userID
	}
	if sk.Visibility == skills.VisibilityPrivate && (sk.OwnerID == userID || sk.OwnerID == actorID) {
		resp.Accessible = true
		resp.Reason = "owner"
		return resp
	}
	if sk.Visibility != skills.VisibilityInternal {
		return resp
	}
	if grant, ok := idx.agentGrant(sk); ok && grant.Granted {
		resp.Accessible = true
		resp.Reason = "agent_grant"
		resp.CanManage = grant.CanManage
		resp.PinnedVersion = grant.PinnedVer
		return resp
	}
	resp.Accessible = true
	resp.Reason = "user_grant"
	return resp
}

func (idx effectiveAccessIndex) accessible(sk store.SkillInfo) bool {
	return idx.accessibleBySlug[sk.Slug]
}

func (idx effectiveAccessIndex) agentGrant(sk store.SkillInfo) (store.SkillWithGrantStatus, bool) {
	if sk.ID != "" {
		if grant, ok := idx.agentGrantByID[sk.ID]; ok {
			return grant, true
		}
	}
	grant, ok := idx.agentGrantBySlug[sk.Slug]
	return grant, ok
}
