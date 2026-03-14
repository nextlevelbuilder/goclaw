package gateway

import "github.com/nextlevelbuilder/goclaw/pkg/protocol"

// methodFeatureMap maps WS method names to the feature flag that controls them.
// Methods not listed here are always allowed regardless of feature flags.
var methodFeatureMap = map[string]string{
	// Chat
	protocol.MethodChatSend:    "chat",
	protocol.MethodChatHistory: "chat",
	protocol.MethodChatAbort:   "chat",
	protocol.MethodChatInject:  "chat",

	// Agents
	protocol.MethodAgentsList:     "agents",
	protocol.MethodAgentsCreate:   "agents",
	protocol.MethodAgentsUpdate:   "agents",
	protocol.MethodAgentsDelete:   "agents",
	protocol.MethodAgentsFileList: "agents",
	protocol.MethodAgentsFileGet:  "agents",
	protocol.MethodAgentsFileSet:  "agents",

	// Agent links
	protocol.MethodAgentsLinksList:   "agents",
	protocol.MethodAgentsLinksCreate: "agents",
	protocol.MethodAgentsLinksUpdate: "agents",
	protocol.MethodAgentsLinksDelete: "agents",

	// Teams
	protocol.MethodTeamsList:            "agent_teams",
	protocol.MethodTeamsCreate:          "agent_teams",
	protocol.MethodTeamsGet:             "agent_teams",
	protocol.MethodTeamsDelete:          "agent_teams",
	protocol.MethodTeamsUpdate:          "agent_teams",
	protocol.MethodTeamsTaskList:        "agent_teams",
	protocol.MethodTeamsTaskGet:         "agent_teams",
	protocol.MethodTeamsTaskApprove:     "agent_teams",
	protocol.MethodTeamsTaskReject:      "agent_teams",
	protocol.MethodTeamsTaskComment:     "agent_teams",
	protocol.MethodTeamsTaskComments:    "agent_teams",
	protocol.MethodTeamsTaskEvents:      "agent_teams",
	protocol.MethodTeamsTaskCreate:      "agent_teams",
	protocol.MethodTeamsTaskAssign:      "agent_teams",
	protocol.MethodTeamsMembersAdd:      "agent_teams",
	protocol.MethodTeamsMembersRemove:   "agent_teams",
	protocol.MethodTeamsKnownUsers:      "agent_teams",
	protocol.MethodTeamsScopes:          "agent_teams",
	protocol.MethodTeamsWorkspaceList:   "agent_teams",
	protocol.MethodTeamsWorkspaceRead:   "agent_teams",
	protocol.MethodTeamsWorkspaceDelete: "agent_teams",

	// Sessions
	protocol.MethodSessionsList:    "sessions",
	protocol.MethodSessionsPreview: "sessions",
	protocol.MethodSessionsPatch:   "sessions",
	protocol.MethodSessionsDelete:  "sessions",
	protocol.MethodSessionsReset:   "sessions",

	// Channels
	protocol.MethodChannelsList:           "channels",
	protocol.MethodChannelsStatus:         "channels",
	protocol.MethodChannelsToggle:         "channels",
	protocol.MethodChannelInstancesList:   "channels",
	protocol.MethodChannelInstancesGet:    "channels",
	protocol.MethodChannelInstancesCreate: "channels",
	protocol.MethodChannelInstancesUpdate: "channels",
	protocol.MethodChannelInstancesDelete: "channels",

	// Skills
	protocol.MethodSkillsList:   "skills",
	protocol.MethodSkillsGet:    "skills",
	protocol.MethodSkillsUpdate: "skills",

	// Cron
	protocol.MethodCronList:   "cron",
	protocol.MethodCronCreate: "cron",
	protocol.MethodCronUpdate: "cron",
	protocol.MethodCronDelete: "cron",
	protocol.MethodCronToggle: "cron",
	protocol.MethodCronStatus: "cron",
	protocol.MethodCronRun:    "cron",
	protocol.MethodCronRuns:   "cron",

	// TTS
	protocol.MethodTTSStatus:      "tts",
	protocol.MethodTTSEnable:      "tts",
	protocol.MethodTTSDisable:     "tts",
	protocol.MethodTTSConvert:     "tts",
	protocol.MethodTTSSetProvider: "tts",
	protocol.MethodTTSProviders:   "tts",

	// Delegations
	protocol.MethodDelegationsList: "delegations",
	protocol.MethodDelegationsGet:  "delegations",

	// Approvals
	protocol.MethodApprovalsList:    "approvals",
	protocol.MethodApprovalsApprove: "approvals",
	protocol.MethodApprovalsDeny:    "approvals",

	// Logs
	protocol.MethodLogsTail: "logs",
}

// MethodFeature returns the feature name that gates a WS method.
// Returns "" if the method is not gated by any feature.
func MethodFeature(method string) string {
	return methodFeatureMap[method]
}
