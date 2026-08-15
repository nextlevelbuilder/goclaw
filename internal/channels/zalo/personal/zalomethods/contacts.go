package zalomethods

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/channels/zalo/personal/protocol"
	"github.com/nextlevelbuilder/goclaw/internal/gateway"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	goclawprotocol "github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// ContactsMethods handles fetching Zalo friends/groups for the picker UI.
type ContactsMethods struct {
	instanceStore store.ChannelInstanceStore
	activeFetches sync.Map // instanceID string -> struct{}
}

func NewContactsMethods(s store.ChannelInstanceStore) *ContactsMethods {
	return &ContactsMethods{instanceStore: s}
}

func (m *ContactsMethods) Register(router *gateway.MethodRouter) {
	router.Register(goclawprotocol.MethodZaloPersonalContacts, m.handleContacts)
}

func (m *ContactsMethods) handleContacts(ctx context.Context, client *gateway.Client, req *goclawprotocol.RequestFrame) {
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
	if err != nil || inst.ChannelType != channels.TypeZaloPersonal {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrNotFound, "zalo_personal instance not found"))
		return
	}

	if len(inst.Credentials) == 0 {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInvalidRequest, "instance has no credentials — complete QR login first"))
		return
	}

	// Prevent concurrent fetches for same instance
	if _, loaded := m.activeFetches.LoadOrStore(params.InstanceID, struct{}{}); loaded {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInvalidRequest, "contacts fetch already in progress"))
		return
	}
	defer m.activeFetches.Delete(params.InstanceID)

	// Parse credentials (same struct as factory.go)
	var creds struct {
		IMEI      string               `json:"imei"`
		Cookie    *protocol.CookieUnion `json:"cookie"`
		UserAgent string               `json:"userAgent"`
		Language  *string              `json:"language,omitempty"`
	}
	if err := json.Unmarshal(inst.Credentials, &creds); err != nil {
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInternal, "failed to parse credentials"))
		return
	}

	protoCred := &protocol.Credentials{
		IMEI:      creds.IMEI,
		Cookie:    creds.Cookie,
		UserAgent: creds.UserAgent,
		Language:  creds.Language,
	}

	// Create temporary session and login
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	sess := protocol.NewSession()
	if err := protocol.LoginWithCredentials(fetchCtx, sess, *protoCred); err != nil {
		slog.Warn("Zalo Personal contacts: login failed", "instance", params.InstanceID, "error", err)
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInternal, "Zalo login failed — credentials may be expired, try QR login again"))
		return
	}

	// Fetch friends and groups in parallel
	var friends []protocol.FriendInfo
	var groups []protocol.GroupListInfo

	g, gctx := errgroup.WithContext(fetchCtx)
	g.Go(func() error {
		f, err := protocol.FetchFriends(gctx, sess)
		if err != nil {
			return err
		}
		friends = f
		return nil
	})
	g.Go(func() error {
		gr, err := protocol.FetchGroups(gctx, sess)
		if err != nil {
			return err
		}
		groups = gr
		return nil
	})

	if err := g.Wait(); err != nil {
		slog.Warn("Zalo Personal contacts: fetch failed", "instance", params.InstanceID, "error", err)
		client.SendResponse(goclawprotocol.NewErrorResponse(req.ID, goclawprotocol.ErrInternal, "failed to fetch contacts"))
		return
	}

	client.SendResponse(goclawprotocol.NewOKResponse(req.ID, map[string]any{
		"friends": friends,
		"groups":  groups,
	}))
}
