package methods

import (
	"context"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// emitAudit broadcasts an audit event via eventBus for async persistence.
func emitAudit(pub bus.EventPublisher, client *gateway.Client, action, entityType, entityID string) {
	emitAuditCtx(pub, client, nil, action, entityType, entityID)
}

// emitAuditCtx is emitAudit with an explicit request context, so callers can
// attach the tenant ID resolved during handshake (WS connect handler injects
// tenant only after auth). Pass nil to fall back to the client's resolved
// tenant.
func emitAuditCtx(pub bus.EventPublisher, client *gateway.Client, reqCtx context.Context, action, entityType, entityID string) {
	if pub == nil {
		return
	}
	tenantID := client.TenantID()
	if reqCtx != nil {
		if tid := store.TenantIDFromContext(reqCtx); tid != uuid.Nil {
			tenantID = tid
		}
	}
	pub.Broadcast(bus.Event{
		Name: protocol.EventAuditLog,
		Payload: bus.AuditEventPayload{
			ActorType:  "user",
			ActorID:    client.UserID(),
			Action:     action,
			EntityType: entityType,
			EntityID:   entityID,
			IPAddress:  client.RemoteAddr(),
			TenantID:   tenantID,
		},
	})
}
