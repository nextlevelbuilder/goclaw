package oa

import (
	"context"
	"log/slog"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// handleUserFollow records the follower as a contact. Team-reply capture
// (eventbus contact-lifecycle publishing) is out of scope for this port.
func (c *Channel) handleUserFollow(e *oaInboundEvent) {
	slog.Info("zalo_oa.webhook.follow_event",
		"event", "user_follow", "user_id", e.Sender.ID, "display_name", e.Sender.DisplayName)
	if e.Sender.ID == "" {
		return
	}
	if cc := c.ContactCollector(); cc != nil {
		// Tenant-scoped ctx: EnsureContact/UpsertContact resolve
		// TenantIDFromContext, and a nil tenant would fall back to
		// MasterTenantID — a cross-tenant leak in multi-tenant deploys.
		cc.EnsureContact(store.WithTenantID(context.Background(), c.TenantID()), c.Type(), c.Name(), e.Sender.ID, e.Sender.ID,
			e.Sender.DisplayName, "", "direct", "follower", "", "")
	}
}

func (c *Channel) handleUserUnfollow(e *oaInboundEvent) {
	slog.Info("zalo_oa.webhook.follow_event",
		"event", "user_unfollow", "user_id", e.Sender.ID)
	// Unfollow: no contact mutation. Future work can emit a lifecycle event
	// once a consumer exists for it.
}
