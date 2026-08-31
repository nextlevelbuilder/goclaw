package cmd

import (
	"sync"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/channels"
	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/skills"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkconfig"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

// ConsumerDeps bundles shared dependencies for consumer message handlers.
// Replaces 11+ positional params with a single injectable struct.
type ConsumerDeps struct {
	Cfg              *config.Config
	Agents           *agent.Router
	Sched            *scheduler.Scheduler
	ChannelMgr       *channels.Manager
	MsgBus           *bus.MessageBus
	TeamStore        store.TeamStore
	AgentLinkStore   store.AgentLinkStore
	AgentStore       store.AgentStore
	SessStore        store.SessionStore
	PostTurn         tools.PostTurnProcessor
	QuotaChecker     *channels.QuotaChecker
	ContactCollector *store.ContactCollector
	TaskRunSessions  sync.Map
	SubagentMgr      *tools.SubagentManager
	UsageCaps        *usagecaps.Service
	ProviderReg      *providers.Registry
	SkillsLoader     *skills.Loader
	MCPStore         store.MCPAgentGrantBatchStore
	BuiltinToolStore store.BuiltinToolStore
	TenantToolStore  store.BuiltinToolTenantConfigStore
	ToolPolicy       *tools.PolicyEngine
	ToolRegistry     *tools.Registry
	BgWg             sync.WaitGroup
	GetAnnounceMu    func(string) *sync.Mutex
	// TeamWorkCfg resolves per-tenant Team Work classifier settings for the
	// inbound ingress. Shared with the WS surface (same *teamworkconfig.Resolver
	// instance) so cache invalidation is coherent. When nil, the inbound gate
	// falls back to Cfg's file-config values (pre-isolation behavior).
	TeamWorkCfg *teamworkconfig.Resolver
}
