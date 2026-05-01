package channels

import (
	"context"
	"errors"
)

// MemberInfo carries the minimal chat-member profile used by the gateway
// to enrich file_writer permission metadata on grant.
type MemberInfo struct {
	Username    string
	DisplayName string
}

// ErrMemberResolveNotSupported indicates the channel does not implement
// ChannelMemberResolver (e.g., Discord/Feishu in Phase 01). Callers should
// treat this as a soft skip — proceed without enriched metadata.
var ErrMemberResolveNotSupported = errors.New("member resolve not supported by this channel")

// MemberResolver is the facade consumed by gateway/HTTP handlers. Manager
// implements it; handlers depend only on this interface (no channel-specific
// imports) to avoid cycles.
type MemberResolver interface {
	ResolveMember(ctx context.Context, channelName, chatID, userID string) (MemberInfo, error)
}

// ChannelMemberResolver is the optional per-channel contract. Channels that
// can look up a member by chat+user ID implement this; Manager dispatches
// through it.
type ChannelMemberResolver interface {
	ResolveMember(ctx context.Context, chatID, userID string) (MemberInfo, error)
}

// ResolveMember looks up a channel by name and delegates to its
// ChannelMemberResolver. Returns ErrMemberResolveNotSupported when the
// channel is missing or does not implement the optional interface.
func (m *Manager) ResolveMember(ctx context.Context, channelName, chatID, userID string) (MemberInfo, error) {
	m.mu.RLock()
	ch, ok := m.channels[channelName]
	m.mu.RUnlock()
	if !ok {
		return MemberInfo{}, ErrMemberResolveNotSupported
	}
	resolver, ok := ch.(ChannelMemberResolver)
	if !ok {
		return MemberInfo{}, ErrMemberResolveNotSupported
	}
	return resolver.ResolveMember(ctx, chatID, userID)
}
