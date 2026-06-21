package bitrix24

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// bitrixCreds maps the credentials JSON from channel_instances.credentials.
//
// Bitrix24 keeps portal-level OAuth (client_id / client_secret / tokens) on the
// `bitrix_portals` row — not here. A channel_instance is a thin pointer into
// that portal plus per-bot config, so creds is currently empty. Kept as an
// explicit struct to reserve the shape for future bot-local secrets (e.g. a
// per-bot HMAC) without breaking stored rows.
type bitrixCreds struct{}

// bitrixInstanceConfig maps the non-secret config JSONB from channel_instances.config.
//
// Portal + BotCode + BotName are required. Everything else is optional with
// sensible defaults applied in the factory. Fields are grouped to match the
// Phase 03 plan — resource link first, then policies, rendering, stream,
// misc, and a per-instance PublicURL override (used for the webhook URLs
// sent to imbot.register).
type bitrixInstanceConfig struct {
	// Resource link (required)
	Portal    string `json:"portal"`               // bitrix_portals.name scoped by tenant_id
	BotCode   string `json:"bot_code"`             // stable key passed to imbot.register / LookupRegisteredBot
	BotName   string `json:"bot_name"`             // display name
	BotAvatar string `json:"bot_avatar,omitempty"` // optional URL; factory resolves and base64-encodes at Start()

	// BotType — forwarded verbatim to imbot.register TYPE param.
	//
	//   "B" = standard chatbot (default; matches Bitrix24 docs default).
	//         Nhân viên nội bộ; sees DMs always, sees group messages only
	//         when @mentioned. Pairs with tenant_users via ContactCollector
	//         and receives per-user MCP credentials (Phase C provisioner).
	//         Recommended: dm_policy=pairing, group_policy=open.
	//
	//   "O" = Open Channel bot. Khách hàng từ widget external chat (imol|…).
	//         Admin phải gắn bot vào Open Channel queue qua UI Bitrix sau
	//         register.
	//         Recommended: dm_policy=open, group_policy=open (khách không
	//         pair được). MCP credentials bị skip cho bot này — nếu cần
	//         MCP tools, admin phải setup shared credential (Phase E tương
	//         lai). Factory does NOT auto-relax dm_policy — admin phải
	//         explicit set open, hoặc bot sẽ im lặng với khách (logs chỉ
	//         rõ "pairing needed").
	//
	// Anything else rejected at factory load.
	BotType string `json:"bot_type,omitempty"`

	// Policies
	AllowFrom      []string `json:"allow_from,omitempty"`
	GroupAllowFrom []string `json:"group_allow_from,omitempty"`
	DMPolicy       string   `json:"dm_policy,omitempty"`
	GroupPolicy    string   `json:"group_policy,omitempty"`
	DeptAllowFrom  []int    `json:"dept_allow_from,omitempty"` // Phase 04
	RequireMention *bool    `json:"require_mention,omitempty"`

	// Rendering
	TextChunkLimit int `json:"text_chunk_limit,omitempty"` // default 4000
	MediaMaxMB     int `json:"media_max_mb,omitempty"`     // default 20

	// Stream / reactions (Phase 05, 07)
	Streaming     *bool  `json:"streaming,omitempty"`
	ReactionLevel string `json:"reaction_level,omitempty"` // off|minimal|full

	// Misc
	HistoryLimit int                        `json:"history_limit,omitempty"`
	BlockReply   *bool                      `json:"block_reply,omitempty"`
	ChatBehavior *config.ChatBehaviorConfig `json:"chat_behavior,omitempty"`

	// Webhook endpoint override. Bitrix24 imbot.register requires absolute
	// URLs for EVENT_MESSAGE_ADD etc. GoClaw has no global GOCLAW_PUBLIC_URL
	// setting — we let operators configure it per-instance so multiple
	// gateways fronting different ingresses can co-exist.
	//
	// When empty Start() warns and still registers with /bitrix24/events as
	// a relative path; the admin has to fix the config before webhooks flow.
	PublicURL string `json:"public_url,omitempty"`

	// Optional MCP lazy-provisioning binding (Phase C).
	//
	// When MCPServerName + MCPBaseURL are set AND the factory variant that
	// accepts a MCPServerStore is used (FactoryWithPortalStoreAndMCP), the
	// channel tries to mint per-user MCP credentials on first message:
	//
	//   1. Channel receives message from user U with OAuth tokens in event.
	//   2. Channel looks up MCPUserCredentials(serverID, senderID). Present
	//      → skip. Absent → POST /api/auto-onboard on MCPBaseURL forwarding
	//      U's OAuth tokens. MCP server authenticates the call via Bitrix
	//      `profile` against the supplied access_token (Path B — no shared
	//      admin secret required) and responds with a per-user api_key,
	//      which channel stores via SetUserCredentials.
	//   3. Agent pipeline downstream reads those creds naturally.
	//
	// Best-effort: if any step fails, channel logs a warning and forwards
	// the message anyway — agent loop will just see no creds and skip
	// that MCP server's tools. User gets a response, albeit without MCP.
	//
	// Half-config fails at factory load: both fields set or both empty.
	//
	// Skipped entirely for Open Channel bots (bot_type=O) — transient
	// customers don't map to tenant_users.
	MCPServerName string `json:"mcp_server_name,omitempty"` // mcp_servers.name
	MCPBaseURL    string `json:"mcp_base_url,omitempty"`    // HTTPS root
}

// Factory is the base channels.ChannelFactory signature. Bitrix24 requires a
// BitrixPortalStore, so this returns an explanatory error — the gateway must
// register FactoryWithPortalStore() instead. Kept to satisfy anyone who
// looks for Factory by convention across channel packages.
func Factory(name string, creds json.RawMessage, cfg json.RawMessage,
	msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {
	return nil, errors.New("bitrix24: use FactoryWithPortalStore in gateway wiring — portal store is required")
}

// FactoryWithPortalStore returns a ChannelFactory closed over the portal
// store + AES encryption key. Gateway wiring calls this once at startup and
// hands the returned closure to the InstanceLoader.
//
// This variant leaves MCP lazy-provisioning disabled. Use
// FactoryWithPortalStoreAndMCP when the gateway is configured with an
// MCPServerStore and you want channels to auto-onboard per-user MCP
// credentials on first message.
//
// Responsibilities of the closure:
//   - Unmarshal and validate cfg (required fields + defaults).
//   - Resolve the shared Router singleton (creates it on first invocation).
//   - Build the Channel with pairing + allow lists + mention policy wired up.
//
// The closure does NOT resolve/load the Portal or talk to Bitrix24 — that
// work is deferred to Channel.Start() so a bad row doesn't crash boot.
func FactoryWithPortalStore(portalStore store.BitrixPortalStore, encKey string) channels.ChannelFactory {
	return FactoryWithPortalStoreAndMCP(portalStore, nil, encKey)
}

// FactoryWithPortalStoreAndMCP is the MCP-aware variant of
// FactoryWithPortalStore. When mcpStore is non-nil AND the instance config
// has both mcp_server_name + mcp_base_url set, the channel enables lazy
// provisioning: on first message from each user, it POSTs to
// {mcp_base_url}/api/auto-onboard to mint per-user MCP credentials,
// which downstream agent pipeline reads naturally. The MCP server
// authenticates each call via the caller-supplied Bitrix access_token
// (Path B) — no shared admin secret is required.
//
// Pass nil mcpStore to disable provisioning even if config has the fields.
// Half-config (only one of mcp_server_name / mcp_base_url set) fails fast.
func FactoryWithPortalStoreAndMCP(portalStore store.BitrixPortalStore, mcpStore store.MCPServerStore, encKey string) channels.ChannelFactory {
	return func(name string, creds json.RawMessage, cfg json.RawMessage,
		msgBus *bus.MessageBus, pairingSvc store.PairingStore) (channels.Channel, error) {

		if portalStore == nil {
			return nil, errors.New("bitrix24 factory: nil BitrixPortalStore (gateway wiring bug)")
		}

		// creds is optional for Bitrix24 — the bot has no private secrets of
		// its own. Decode anyway so a malformed blob surfaces as a boot error.
		if len(creds) > 0 {
			var c bitrixCreds
			if err := json.Unmarshal(creds, &c); err != nil {
				return nil, fmt.Errorf("decode bitrix24 credentials: %w", err)
			}
		}

		var ic bitrixInstanceConfig
		if len(cfg) > 0 {
			if err := json.Unmarshal(cfg, &ic); err != nil {
				return nil, fmt.Errorf("decode bitrix24 config: %w", err)
			}
		}
		if ic.Portal == "" || ic.BotCode == "" || ic.BotName == "" {
			return nil, errors.New("bitrix24 channel requires portal, bot_code, and bot_name")
		}

		applyConfigDefaults(&ic)

		// Validate bot_type AFTER defaults so empty → "B" passes the check.
		// Keep the set small and explicit — other values (e.g. "H" hidden
		// helper) may appear in Bitrix docs but we haven't verified the
		// event semantics, so refusing unknown types avoids shipping a bot
		// that silently receives no events.
		switch ic.BotType {
		case "B", "O":
			// ok
		default:
			return nil, fmt.Errorf("bitrix24: invalid bot_type %q (must be \"B\" or \"O\")", ic.BotType)
		}

		// MCP provisioning config is all-or-nothing. Catching half-config here
		// prevents a silent "provisioning disabled but you meant to enable it"
		// surprise — admin either sets both or neither.
		hasServerName := strings.TrimSpace(ic.MCPServerName) != ""
		hasBaseURL := strings.TrimSpace(ic.MCPBaseURL) != ""
		if hasServerName != hasBaseURL {
			return nil, errors.New("bitrix24: mcp_server_name and mcp_base_url must both be set, or both empty")
		}

		// Shared process-wide router. InitWebhookRouter uses sync.Once so the
		// first caller wins; later callers get the same pointer. Any nil-store
		// mistake would have panicked on the first call anyway — returning
		// the error keeps boot diagnostics clean.
		router, err := InitWebhookRouter(portalStore, encKey, RouterConfig{})
		if err != nil {
			return nil, fmt.Errorf("bitrix24 router init: %w", err)
		}

		ch := &Channel{
			BaseChannel: channels.NewBaseChannel(name, msgBus, mergeAllowLists(ic.AllowFrom, ic.GroupAllowFrom)),
			cfg:         ic,
			portalStore: portalStore,
			encKey:      encKey,
			router:      router,
			stopCh:      make(chan struct{}),
			mcpStore:    mcpStore, // may be nil; provisionIfMissing treats nil as disabled
		}
		ch.SetType(channels.TypeBitrix24)
		ch.SetName(name)
		ch.SetPairingService(pairingSvc)
		// applyConfigDefaults guarantees RequireMention is non-nil, but guard
		// the deref anyway so a future refactor of the defaults can't turn this
		// into a boot-time nil-pointer panic.
		requireMention := true
		if ic.RequireMention != nil {
			requireMention = *ic.RequireMention
		}
		ch.SetRequireMention(requireMention)
		ch.SetHistoryLimit(ic.HistoryLimit)
		ch.ValidatePolicy(ic.DMPolicy, ic.GroupPolicy)
		return ch, nil
	}
}

// applyConfigDefaults fills in the per-instance knobs a well-behaved portal
// would have been given at onboard time. Pulled into its own function so
// tests can exercise the default surface directly.
func applyConfigDefaults(ic *bitrixInstanceConfig) {
	// bot_type default matches Bitrix24 imbot.register TYPE default.
	// Keep this BEFORE the policy defaults so future logic can branch on
	// bot_type if needed — currently it does not (see type docstring).
	if ic.BotType == "" {
		ic.BotType = "B"
	}
	if ic.DMPolicy == "" {
		ic.DMPolicy = string(channels.DMPolicyPairing)
	}
	if ic.GroupPolicy == "" {
		ic.GroupPolicy = string(channels.GroupPolicyOpen)
	}
	if ic.TextChunkLimit <= 0 {
		ic.TextChunkLimit = 4000
	}
	if ic.MediaMaxMB <= 0 {
		ic.MediaMaxMB = 20
	}
	if ic.ReactionLevel == "" {
		ic.ReactionLevel = "minimal"
	}
	if ic.RequireMention == nil {
		t := true
		ic.RequireMention = &t
	}
	if ic.Streaming == nil {
		t := true
		ic.Streaming = &t
	}
}

// mergeAllowLists concatenates DM and group allow-lists into a single slice
// that BaseChannel.IsAllowed can check against. Order preserved; empty input
// slices skipped so the resulting slice stays nil when nothing is configured
// (BaseChannel treats a nil allow-list as "open").
func mergeAllowLists(dm, group []string) []string {
	if len(dm) == 0 && len(group) == 0 {
		return nil
	}
	out := make([]string, 0, len(dm)+len(group))
	out = append(out, dm...)
	out = append(out, group...)
	return out
}
