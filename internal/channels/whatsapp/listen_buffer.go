package whatsapp

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/channels/media"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// listenEntry holds a single message for immediate raw storage.
type listenEntry struct {
	Sender    string
	SenderID  string
	Content   string
	Timestamp time.Time
	ChatID    string
	ChatName  string
	AgentID   string // per-message agent UUID override (empty = use buffer default)
	MediaRefs []store.RawMediaRef
}

// mediaSaver is the interface for persisting media files (satisfied by *media.Store).
type mediaSaver interface {
	SaveFile(sessionKey, srcPath, mime string) (id string, dstPath string, err error)
}

// ListenBuffer inserts messages into the listen_raw_messages store in real-time.
// No buffering, no timer — each message is INSERT'd immediately on arrival.
// The background extraction worker polls the table for KG extraction separately.
type ListenBuffer struct {
	mu          sync.Mutex
	rawMsgStore store.ListenRawMessageStore
	mediaStore  mediaSaver
	agentUUID   string
	tenantID    uuid.UUID
	channelName string
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewListenBuffer creates a new ListenBuffer.
func NewListenBuffer(rawMsgStore store.ListenRawMessageStore, agentKey string, _ int) *ListenBuffer {
	ctx, cancel := context.WithCancel(context.Background())
	return &ListenBuffer{
		rawMsgStore: rawMsgStore,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// SetAgentUUID sets the agent UUID for scoping.
func (b *ListenBuffer) SetAgentUUID(agentUUID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agentUUID = agentUUID
}

// SetTenantID sets the tenant ID for scoping.
func (b *ListenBuffer) SetTenantID(tenantID uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tenantID = tenantID
}

// SetChannelName sets the channel name (e.g., "whatsapp").
func (b *ListenBuffer) SetChannelName(name string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.channelName = name
}

// SetRawMsgStore sets the raw message store (called during wiring).
func (b *ListenBuffer) SetRawMsgStore(s store.ListenRawMessageStore) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rawMsgStore = s
}

// SetMediaStore sets the media store for persisting media files.
func (b *ListenBuffer) SetMediaStore(ms mediaSaver) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mediaStore = ms
}

// SetAgentKey is a no-op kept for interface compatibility.
func (b *ListenBuffer) SetAgentKey(_ string) {}

// Add inserts a single message into the listen_raw_messages store immediately.
func (b *ListenBuffer) Add(graphID string, entry listenEntry) {
	b.mu.Lock()
	if b.ctx.Err() != nil {
		b.mu.Unlock()
		return
	}
	rawStore := b.rawMsgStore
	agentUUID := b.agentUUID
	tenantID := b.tenantID
	channelName := b.channelName
	b.mu.Unlock()

	if rawStore == nil {
		slog.Warn("whatsapp listen: insert skipped, missing raw message store",
			"graph_id", graphID, "sender", entry.Sender)
		return
	}
	// Use per-entry agent UUID override if provided (e.g., from group agent_id config).
	effectiveAgentUUID := agentUUID
	if entry.AgentID != "" {
		effectiveAgentUUID = entry.AgentID
	}
	if effectiveAgentUUID == "" {
		slog.Warn("whatsapp listen: insert skipped, agent UUID not set",
			"graph_id", graphID, "sender", entry.Sender)
		return
	}

	msg := store.ListenRawMessage{
		ChannelName:  channelName,
		ChatID:       entry.ChatID,
		ChatName:     entry.ChatName,
		GraphID:      graphID,
		Sender:       entry.Sender,
		SenderID:     entry.SenderID,
		Body:         entry.Content,
		MsgTimestamp: entry.Timestamp,
		AgentID:      effectiveAgentUUID,
		TenantID:     tenantID,
		MediaRefs:    entry.MediaRefs,
	}

	ctx := store.WithTenantID(b.ctx, tenantID)
	if err := rawStore.AppendBatch(ctx, []store.ListenRawMessage{msg}); err != nil {
		slog.Error("whatsapp listen: insert failed",
			"graph_id", graphID, "sender", entry.Sender, "error", err)
		return
	}

	slog.Info("whatsapp listen: raw message stored",
		"graph_id", graphID, "sender", entry.Sender,
		"chat_id", entry.ChatID, "channel", channelName,
		"agent_id", effectiveAgentUUID, "override", entry.AgentID != "",
		"media_refs", len(entry.MediaRefs))
}

// Close cancels the context. No buffered entries to flush — everything is already in the DB.
func (b *ListenBuffer) Close() {
	b.cancel()
}

// PersistMedia moves downloaded media files to durable storage keyed by agentID/graphID.
// Returns persisted refs for DB storage and paths that failed (for temp cleanup).
func (b *ListenBuffer) PersistMedia(agentUUID, graphID string, mediaList []media.MediaInfo) ([]store.RawMediaRef, []string) {
	b.mu.Lock()
	ms := b.mediaStore
	b.mu.Unlock()

	if ms == nil {
		var paths []string
		for _, mi := range mediaList {
			if mi.FilePath != "" {
				paths = append(paths, mi.FilePath)
			}
		}
		return nil, paths
	}

	sessionKey := agentUUID + "/" + graphID
	var persisted []store.RawMediaRef
	var failed []string
	for _, mi := range mediaList {
		if mi.FilePath == "" {
			continue
		}
		mediaID, dstPath, err := ms.SaveFile(sessionKey, mi.FilePath, mi.ContentType)
		if err != nil {
			slog.Warn("whatsapp listen: media persist failed",
				"type", mi.Type, "error", err)
			failed = append(failed, mi.FilePath)
			continue
		}
		persisted = append(persisted, store.RawMediaRef{
			MediaID:     mediaID,
			FilePath:    dstPath,
			MediaType:   mi.Type,
			ContentType: mi.ContentType,
			FileName:    mi.FileName,
			FileSize:    mi.FileSize,
		})
	}
	return persisted, failed
}
