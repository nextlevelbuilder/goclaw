package cmd

import (
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// wireHTTP creates HTTP handlers (agents + skills + traces + MCP + channel instances + providers + builtin tools + pending messages).
func wireHTTP(stores *store.Stores, defaultWorkspace, dataDir, bundledSkillsDir string, msgBus *bus.MessageBus, toolsReg *tools.Registry, providerReg *providers.Registry, modelReg providers.ModelRegistry, isOwner func(string) bool, gatewayAddr string, mcpToolLister httpapi.MCPToolLister) (*httpapi.AgentsHandler, *httpapi.SkillsHandler, *httpapi.TracesHandler, *httpapi.MCPHandler, *httpapi.ChannelInstancesHandler, *httpapi.ProvidersHandler, *httpapi.BuiltinToolsHandler, *httpapi.PendingMessagesHandler, *httpapi.TeamEventsHandler, *httpapi.SecureCLIHandler, *httpapi.SecureCLIGrantHandler, *httpapi.MCPUserCredentialsHandler) {
	var agentsH *httpapi.AgentsHandler
	var skillsH *httpapi.SkillsHandler
	var tracesH *httpapi.TracesHandler
	var mcpH *httpapi.MCPHandler
	var channelInstancesH *httpapi.ChannelInstancesHandler
	var providersH *httpapi.ProvidersHandler
	var builtinToolsH *httpapi.BuiltinToolsHandler
	var pendingMessagesH *httpapi.PendingMessagesHandler
	var secureCLIH *httpapi.SecureCLIHandler
	var secureCLIGrantH *httpapi.SecureCLIGrantHandler

	if stores != nil && stores.Agents != nil {
		var summoner *httpapi.AgentSummoner
		if providerReg != nil {
			summoner = httpapi.NewAgentSummoner(stores.Agents, providerReg, msgBus)
		}
		agentsH = httpapi.NewAgentsHandler(stores.Agents, stores.Providers, providerReg, stores.DB, stores.Tracing, defaultWorkspace, msgBus, summoner, isOwner)
		agentsH.SetImportStores(stores.Memory, stores.KnowledgeGraph)
		agentsH.SetDataDir(dataDir)
	}

	if stores != nil && stores.Skills != nil {
		if manageStore, ok := stores.Skills.(store.SkillManageStore); ok {
			dirs := manageStore.Dirs()
			if len(dirs) > 0 {
				skillsH = httpapi.NewSkillsHandler(manageStore, dirs[0], dataDir, bundledSkillsDir, msgBus, stores.SkillTenantCfgs, stores.Tenants)
				skillsH.SetDB(stores.DB)
			}
		}
	}

	if stores != nil && stores.Tracing != nil {
		tracesH = httpapi.NewTracesHandler(stores.Tracing)
	}

	if stores != nil && stores.MCP != nil {
		mcpH = httpapi.NewMCPHandler(stores.MCP, msgBus, mcpToolLister)
		mcpH.SetDB(stores.DB)
	}
	var mcpUserCredsH *httpapi.MCPUserCredentialsHandler
	if stores != nil && stores.MCP != nil {
		mcpUserCredsH = httpapi.NewMCPUserCredentialsHandler(stores.MCP, stores.Tenants)
	}

	if stores != nil && stores.ChannelInstances != nil {
		channelInstancesH = httpapi.NewChannelInstancesHandler(stores.ChannelInstances, stores.Agents, stores.ConfigPermissions, stores.Contacts, stores.Tenants, msgBus)
	}

	if stores != nil && stores.Providers != nil {
		providersH = httpapi.NewProvidersHandler(stores.Providers, stores.ConfigSecrets, providerReg, gatewayAddr)
		providersH.SetMessageBus(msgBus)
		if modelReg != nil {
			providersH.SetModelRegistry(modelReg)
		}
		if stores.SystemConfigs != nil {
			providersH.SetSystemConfigStore(stores.SystemConfigs)
		}
		if stores.MCP != nil {
			providersH.SetMCPServerLookup(buildMCPServerLookup(stores.MCP))
		}
		if stores.Tracing != nil {
			providersH.SetTracingStore(stores.Tracing)
		}
		if stores.Agents != nil {
			providersH.SetAgentStore(stores.Agents)
		}
	}

	var teamEventsH *httpapi.TeamEventsHandler

	if stores != nil && stores.Teams != nil {
		teamEventsH = httpapi.NewTeamEventsHandler(stores.Teams)
	}

	if stores != nil && stores.BuiltinTools != nil {
		builtinToolsH = httpapi.NewBuiltinToolsHandler(stores.BuiltinTools, stores.BuiltinToolTenantCfgs, stores.Tenants, stores.ConfigSecrets, msgBus)
	}

	if stores != nil && stores.PendingMessages != nil {
		pendingMessagesH = httpapi.NewPendingMessagesHandler(stores.PendingMessages, stores.Agents, providerReg)
	}

	if stores != nil && stores.SecureCLI != nil {
		secureCLIH = httpapi.NewSecureCLIHandler(stores.SecureCLI, msgBus)
	}
	if stores != nil && stores.SecureCLIGrants != nil {
		secureCLIGrantH = httpapi.NewSecureCLIGrantHandler(stores.SecureCLIGrants, msgBus)
	}

	return agentsH, skillsH, tracesH, mcpH, channelInstancesH, providersH, builtinToolsH, pendingMessagesH, teamEventsH, secureCLIH, secureCLIGrantH, mcpUserCredsH
}
