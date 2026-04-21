package whatsapp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	goclawprotocol "github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// GroupMethods handles whatsapp.groups.refresh.
type GroupMethods struct {
	instanceStore store.ChannelInstanceStore
	manager       *channels.Manager
}

// NewGroupMethods creates a new GroupMethods instance.
func NewGroupMethods(instanceStore store.ChannelInstanceStore, manager *channels.Manager) *GroupMethods {
	return &GroupMethods{instanceStore: instanceStore, manager: manager}
}

// Register registers the group-related WS methods on the router.
func (m *GroupMethods) Register(router *gateway.MethodRouter) {
	router.Register(goclawprotocol.MethodWhatsAppGroupsRefresh, m.handleRefreshGroups)
}

func (m *GroupMethods) handleRefreshGroups(ctx context.Context, client *gateway.Client, req *goclawprotocol.RequestFrame) {
	var params struct {
		InstanceID string `json:"instance_id"`
	}
	if req.Params != nil {
		_ = json.Unmarshal(req.Params, &params)
	}

	instID, err := uuid.Parse(params.InstanceID)
	if err != nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInvalidRequest, "invalid instance_id"))
		return
	}

	inst, err := m.instanceStore.Get(ctx, instID)
	if err != nil || inst.ChannelType != channels.TypeWhatsApp {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "whatsapp instance not found"))
		return
	}

	// Resolve the channel from the manager (may need a brief wait for async reload).
	var wa *Channel
	for range 10 {
		if ch, ok := m.manager.GetChannel(inst.Name); ok {
			if w, ok := ch.(*Channel); ok {
				wa = w
				break
			}
		}
		select {
		case <-ctx.Done():
			client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInternal, "context cancelled"))
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
	if wa == nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "whatsapp channel not running"))
		return
	}

	groups, err := wa.RefreshGroups(ctx)
	if err != nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInternal, err.Error()))
		return
	}

	client.SendResponse(goclawprotocol.NewOKResponse(req.ID, map[string]any{"groups": groups}))
}
