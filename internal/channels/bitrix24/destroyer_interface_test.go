package bitrix24

import (
	"github.com/nextlevelbuilder/goclaw/internal/channels"
)

// Compile-time guard: bitrix24.Channel must satisfy channels.ChannelDestroyer.
// If a future refactor drops Destroy() or changes its signature, this break
// surfaces at build time rather than as a silent zombie-bot regression in
// production (handlers would just skip the destroyer block via type
// assertion miss).
//
// Hat-tip to the existing channels.WebhookChannel guard pattern.
var _ channels.ChannelDestroyer = (*Channel)(nil)
