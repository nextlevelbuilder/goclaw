package teamworkclassify

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

var canonicalCapabilityKeys = map[string]struct{}{
	string(CapabilityLeadCoordinator): {},
	string(CapabilityResearch):        {},
	string(CapabilityStrategy):        {},
	string(CapabilityAnalyticsCritic): {},
	string(CapabilityContentLead):     {},
	string(CapabilityVisualPrompt):    {},
	string(CapabilityTechnical):       {},
	string(CapabilityQA):              {},
}

func ParseStructuredCapabilities(raw json.RawMessage) ([]StructuredCapability, DataStatus) {
	if len(raw) == 0 {
		return nil, DataStatusUnknown
	}
	var bag map[string]json.RawMessage
	if json.Unmarshal(raw, &bag) != nil {
		return nil, DataStatusUnknown
	}
	value, exists := bag["capabilities"]
	if !exists {
		return nil, DataStatusUnknown
	}
	var entries []json.RawMessage
	if json.Unmarshal(value, &entries) != nil {
		return nil, DataStatusUnknown
	}
	capabilities := make([]StructuredCapability, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		var key string
		var label string
		if json.Unmarshal(entry, &key) != nil {
			var object struct {
				Key   string `json:"key"`
				Label string `json:"label"`
			}
			if json.Unmarshal(entry, &object) != nil {
				return nil, DataStatusUnknown
			}
			key, label = object.Key, object.Label
		}
		key = strings.ToLower(strings.TrimSpace(key))
		label = strings.TrimSpace(label)
		if !validCapabilityKey(key) {
			return nil, DataStatusUnknown
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		capabilities = append(capabilities, StructuredCapability{Key: key, Label: label})
	}
	slices.SortFunc(capabilities, func(a, b StructuredCapability) int { return strings.Compare(a.Key, b.Key) })
	return capabilities, DataStatusKnown
}

func validCapabilityKey(key string) bool {
	if _, ok := canonicalCapabilityKeys[key]; ok {
		return true
	}
	slug, ok := strings.CutPrefix(key, "custom:")
	if !ok || slug == "" {
		return false
	}
	for _, r := range slug {
		if !(unicode.IsLower(r) || unicode.IsDigit(r) || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func profileFromAgent(agent store.AgentData, kind, teamRole, fallbackText string) Profile {
	expertise := strings.TrimSpace(strings.Join([]string{agent.Frontmatter, agent.AgentDescription}, "\n"))
	if expertise == "" {
		expertise = strings.TrimSpace(fallbackText)
	}
	capabilities, status := ParseStructuredCapabilities(agent.OtherConfig)
	return Profile{
		Kind:               kind,
		Name:               firstNonEmpty(agent.DisplayName, agent.AgentKey),
		Text:               expertise,
		AgentID:            agent.ID,
		AgentKey:           agent.AgentKey,
		DisplayName:        agent.DisplayName,
		TeamRole:           teamRole,
		Capabilities:       capabilities,
		CapabilitiesStatus: status,
		ExpertiseSummary:   expertise,
	}
}

func enrichRosterProfiles(ctx context.Context, stores ProfileStores, input *Input, tenantID uuid.UUID) {
	if input == nil || stores.Agents == nil {
		return
	}
	ids := uniqueProfileAgentIDs(input.CurrentAgent, input.Members, input.Delegates)
	agents, err := stores.Agents.GetByIDs(ctx, ids)
	if err != nil {
		markRosterUnknown(input)
		return
	}
	agentByID := make(map[uuid.UUID]store.AgentData, len(agents))
	for _, agent := range agents {
		agentByID[agent.ID] = agent
	}
	if agent, ok := agentByID[input.CurrentAgent.AgentID]; ok {
		input.CurrentAgent = profileFromAgent(agent, "agent", input.TeamRole, input.CurrentAgent.Text)
	}
	for i := range input.Members {
		if agent, ok := agentByID[input.Members[i].AgentID]; ok {
			input.Members[i] = profileFromAgent(agent, "team_member", input.Members[i].TeamRole, input.Members[i].Text)
		} else {
			input.Members[i].CapabilitiesStatus = DataStatusUnknown
			input.Members[i].AvailableToolsStatus = DataStatusUnknown
		}
	}
	for i := range input.Delegates {
		if agent, ok := agentByID[input.Delegates[i].AgentID]; ok {
			input.Delegates[i] = profileFromAgent(agent, "delegate", input.Delegates[i].TeamRole, input.Delegates[i].Text)
		} else {
			input.Delegates[i].CapabilitiesStatus = DataStatusUnknown
			input.Delegates[i].AvailableToolsStatus = DataStatusUnknown
		}
	}
	applyAvailableToolSnapshots(ctx, stores, input, tenantID, agentByID)
}

func uniqueProfileAgentIDs(current Profile, groups ...[]Profile) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{})
	if current.AgentID != uuid.Nil {
		seen[current.AgentID] = struct{}{}
	}
	for _, group := range groups {
		for _, profile := range group {
			if profile.AgentID != uuid.Nil {
				seen[profile.AgentID] = struct{}{}
			}
		}
	}
	ids := make([]uuid.UUID, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return ids
}

func markRosterUnknown(input *Input) {
	input.CurrentAgent.CapabilitiesStatus = DataStatusUnknown
	input.CurrentAgent.AvailableToolsStatus = DataStatusUnknown
	for i := range input.Members {
		input.Members[i].CapabilitiesStatus = DataStatusUnknown
		input.Members[i].AvailableToolsStatus = DataStatusUnknown
	}
	for i := range input.Delegates {
		input.Delegates[i].CapabilitiesStatus = DataStatusUnknown
		input.Delegates[i].AvailableToolsStatus = DataStatusUnknown
	}
}

func applyAvailableToolSnapshots(ctx context.Context, stores ProfileStores, input *Input, tenantID uuid.UUID, agents map[uuid.UUID]store.AgentData) {
	profiles := []*Profile{&input.CurrentAgent}
	for i := range input.Members {
		profiles = append(profiles, &input.Members[i])
	}
	for i := range input.Delegates {
		profiles = append(profiles, &input.Delegates[i])
	}
	markUnknown := func() {
		for _, profile := range profiles {
			profile.AvailableTools = nil
			profile.AvailableToolsStatus = DataStatusUnknown
		}
	}
	if tenantID == uuid.Nil || stores.BuiltinTools == nil || stores.TenantToolConfigs == nil || stores.MCP == nil || stores.ToolPolicy == nil || stores.ToolRegistry == nil {
		markUnknown()
		return
	}
	definitions, err := stores.BuiltinTools.List(ctx)
	if err != nil {
		markUnknown()
		return
	}
	tenantOverrides, err := stores.TenantToolConfigs.ListAll(ctx, tenantID)
	if err != nil {
		markUnknown()
		return
	}
	servers, err := stores.MCP.ListServers(ctx)
	if err != nil {
		markUnknown()
		return
	}
	ids := make([]uuid.UUID, 0, len(profiles))
	for _, profile := range profiles {
		if profile.AgentID != uuid.Nil {
			ids = append(ids, profile.AgentID)
		}
	}
	grants, err := stores.MCP.ListAgentGrantsByAgentIDs(ctx, ids)
	if err != nil {
		markUnknown()
		return
	}
	serverByID := make(map[uuid.UUID]store.MCPServerData, len(servers))
	for _, server := range servers {
		serverByID[server.ID] = server
	}
	grantsByAgent := make(map[uuid.UUID][]store.MCPAgentGrant)
	for _, grant := range grants {
		grantsByAgent[grant.AgentID] = append(grantsByAgent[grant.AgentID], grant)
	}
	for _, profile := range profiles {
		agent, ok := agents[profile.AgentID]
		if !ok {
			profile.AvailableToolsStatus = DataStatusUnknown
			continue
		}
		available := make([]string, 0, len(definitions))
		for _, definition := range definitions {
			enabled := definition.Enabled
			if override, exists := tenantOverrides[definition.Name]; exists {
				enabled = override
			}
			if enabled && stores.ToolPolicy.WouldAllow(stores.ToolRegistry, definition.Name, agent.Provider, agent.ParseToolsConfig(), input.ToolAllow) {
				available = append(available, definition.Name)
			}
		}
		mcpKnown := true
		for _, grant := range grantsByAgent[profile.AgentID] {
			if !grant.Enabled {
				continue
			}
			server, ok := serverByID[grant.ServerID]
			if !ok || !server.Enabled {
				continue
			}
			toolNames, known := staticMCPToolNames(server, grant)
			if !known {
				mcpKnown = false
				continue
			}
			for _, name := range toolNames {
				if stores.ToolPolicy.WouldAllow(stores.ToolRegistry, name, agent.Provider, agent.ParseToolsConfig(), input.ToolAllow) {
					available = append(available, name)
				}
			}
		}
		profile.AvailableTools = compactSortedStrings(available)
		if mcpKnown {
			profile.AvailableToolsStatus = DataStatusKnown
		} else {
			profile.AvailableToolsStatus = DataStatusUnknown
		}
	}
}

func staticMCPToolNames(server store.MCPServerData, grant store.MCPAgentGrant) ([]string, bool) {
	allow, allowOK := parseStringList(grant.ToolAllow)
	deny, denyOK := parseStringList(grant.ToolDeny)
	if !allowOK || !denyOK {
		return nil, false
	}
	cache := make(map[string]json.RawMessage)
	if len(server.Settings) > 0 {
		var settings map[string]json.RawMessage
		if json.Unmarshal(server.Settings, &settings) == nil {
			if raw, ok := settings["tool_cache"]; ok {
				_ = json.Unmarshal(raw, &cache)
			}
		}
	}
	bareNames := allow
	if len(bareNames) == 0 {
		if len(cache) == 0 {
			return nil, false
		}
		bareNames = make([]string, 0, len(cache))
		for name := range cache {
			bareNames = append(bareNames, name)
		}
	}
	denied := make(map[string]struct{}, len(deny))
	for _, name := range deny {
		denied[stripMCPPrefix(name)] = struct{}{}
	}
	prefix := effectiveMCPPrefix(server.ToolPrefix, server.Name)
	result := make([]string, 0, len(bareNames))
	for _, name := range bareNames {
		name = stripMCPPrefix(name)
		if _, blocked := denied[name]; blocked || name == "" {
			continue
		}
		result = append(result, prefix+"__"+name)
	}
	return compactSortedStrings(result), true
}

func parseStringList(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil, false
	}
	return values, true
}

func effectiveMCPPrefix(prefix, serverName string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = strings.ReplaceAll(strings.TrimSpace(serverName), "-", "_")
	}
	if !strings.HasPrefix(prefix, "mcp_") {
		prefix = "mcp_" + prefix
	}
	return prefix
}

func stripMCPPrefix(name string) string {
	name = strings.TrimSpace(name)
	if index := strings.Index(name, "__"); index >= 0 {
		return name[index+2:]
	}
	return name
}

func renderStructuredRosterProfile(profile Profile) string {
	capabilityParts := make([]string, 0, len(profile.Capabilities))
	for _, capability := range profile.Capabilities {
		part := capability.Key
		if capability.Label != "" {
			part += " (" + capability.Label + ")"
		}
		capabilityParts = append(capabilityParts, part)
	}
	return fmt.Sprintf("agent_id: %s\nagent_key: %s\ndisplay_name: %s\nteam_role: %s\ncapabilities_status: %s\ncapabilities: %s\nexpertise_summary: %s\navailable_tools_status: %s\navailable_tools: %s",
		profile.AgentID, profileAgentKey(profile), firstNonEmpty(profile.DisplayName, profile.Name), firstNonEmpty(profile.TeamRole, "unknown"),
		firstNonEmpty(string(profile.CapabilitiesStatus), string(DataStatusUnknown)), strings.Join(capabilityParts, ", "),
		firstNonEmpty(profile.ExpertiseSummary, profile.Text), firstNonEmpty(string(profile.AvailableToolsStatus), string(DataStatusUnknown)), strings.Join(profile.AvailableTools, ", "))
}
