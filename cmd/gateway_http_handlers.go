package cmd

import (
	"github.com/nextlevelbuilder/goclaw/internal/bus"
	httpapi "github.com/nextlevelbuilder/goclaw/internal/http"
	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

// wireHTTP creates HTTP handlers (agents + skills + traces + MCP + channel instances + providers + builtin tools + pending messages).
func wireHTTP(stores *store.Stores, token, defaultWorkspace, dataDir, bundledSkillsDir string, msgBus *bus.MessageBus, toolsReg *tools.Registry, providerReg *providers.Registry, isOwner func(string) bool, gatewayAddr string, mcpToolLister httpapi.MCPToolLister) (*httpapi.AgentsHandler, *httpapi.SkillsHandler, *httpapi.TracesHandler, *httpapi.MCPHandler, *httpapi.ChannelInstancesHandler, *httpapi.ProvidersHandler, *httpapi.BuiltinToolsHandler, *httpapi.PendingMessagesHandler, *httpapi.TeamEventsHandler, *httpapi.SecureCLIHandler, *httpapi.MCPUserCredentialsHandler) {
	var agentsH *httpapi.AgentsHandler
	var skillsH *httpapi.SkillsHandler
	var tracesH *httpapi.TracesHandler
	var mcpH *httpapi.MCPHandler
	var channelInstancesH *httpapi.ChannelInstancesHandler
	var providersH *httpapi.ProvidersHandler
	var builtinToolsH *httpapi.BuiltinToolsHandler
	var pendingMessagesH *httpapi.PendingMessagesHandler
	var secureCLIH *httpapi.SecureCLIHandler

	if stores != nil && stores.Agents != nil {
		var summoner *httpapi.AgentSummoner
		if providerReg != nil {
			summoner = httpapi.NewAgentSummoner(stores.Agents, providerReg, msgBus)
		}
		agentsH = httpapi.NewAgentsHandler(stores.Agents, token, defaultWorkspace, msgBus, summoner, isOwner)
	}

	if stores != nil && stores.Skills != nil {
		if pgSkills, ok := stores.Skills.(*pg.PGSkillStore); ok {
			dirs := pgSkills.Dirs()
			if len(dirs) > 0 {
				skillsH = httpapi.NewSkillsHandler(pgSkills, dirs[0], dataDir, bundledSkillsDir, token, msgBus)
			}
		}
	}

	if stores != nil && stores.Tracing != nil {
		tracesH = httpapi.NewTracesHandler(stores.Tracing, token)
	}

	if stores != nil && stores.MCP != nil {
		mcpH = httpapi.NewMCPHandler(stores.MCP, token, msgBus, mcpToolLister)
	}
	var mcpUserCredsH *httpapi.MCPUserCredentialsHandler
	if stores != nil && stores.MCP != nil {
		mcpUserCredsH = httpapi.NewMCPUserCredentialsHandler(stores.MCP, token)
	}

	if stores != nil && stores.ChannelInstances != nil {
		channelInstancesH = httpapi.NewChannelInstancesHandler(stores.ChannelInstances, stores.Agents, stores.ConfigPermissions, stores.Contacts, stores.Tenants, token, msgBus)
	}

	if stores != nil && stores.Providers != nil {
		providersH = httpapi.NewProvidersHandler(stores.Providers, stores.ConfigSecrets, token, providerReg, gatewayAddr)
		providersH.SetMessageBus(msgBus)
		if stores.MCP != nil {
			providersH.SetMCPServerLookup(buildMCPServerLookup(stores.MCP))
		}
	}

	var teamEventsH *httpapi.TeamEventsHandler

	if stores != nil && stores.Teams != nil {
		teamEventsH = httpapi.NewTeamEventsHandler(stores.Teams, token)
	}

	if stores != nil && stores.BuiltinTools != nil {
		builtinToolsH = httpapi.NewBuiltinToolsHandler(stores.BuiltinTools, stores.BuiltinToolTenantCfgs, token, msgBus)
	}

	if stores != nil && stores.PendingMessages != nil {
		pendingMessagesH = httpapi.NewPendingMessagesHandler(stores.PendingMessages, stores.Agents, token, providerReg)
	}

	if stores != nil && stores.SecureCLI != nil {
		secureCLIH = httpapi.NewSecureCLIHandler(stores.SecureCLI, token, msgBus)
	}

	return agentsH, skillsH, tracesH, mcpH, channelInstancesH, providersH, builtinToolsH, pendingMessagesH, teamEventsH, secureCLIH, mcpUserCredsH
}
