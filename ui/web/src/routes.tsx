import { Suspense } from "react";
import { Routes, Route, Navigate } from "react-router";
import { AppLayout } from "@/components/layout/app-layout";
import { RequireAuth } from "@/components/shared/require-auth";
import { RequireSetup } from "@/components/shared/require-setup";
import { RequireFeature } from "@/components/shared/require-feature";
import { ErrorBoundary } from "@/components/shared/error-boundary";
import { ROUTES } from "@/lib/constants";
import { lazyWithRetry } from "@/lib/lazy-with-retry";

// Lazy-loaded pages
const LoginPage = lazyWithRetry(() =>
  import("@/pages/login/login-page").then((m) => ({ default: m.LoginPage })),
);
const OverviewPage = lazyWithRetry(() =>
  import("@/pages/overview/overview-page").then((m) => ({ default: m.OverviewPage })),
);
const ChatPage = lazyWithRetry(() =>
  import("@/pages/chat/chat-page").then((m) => ({ default: m.ChatPage })),
);
const AgentsPage = lazyWithRetry(() =>
  import("@/pages/agents/agents-page").then((m) => ({ default: m.AgentsPage })),
);
const SessionsPage = lazyWithRetry(() =>
  import("@/pages/sessions/sessions-page").then((m) => ({ default: m.SessionsPage })),
);
const SkillsPage = lazyWithRetry(() =>
  import("@/pages/skills/skills-page").then((m) => ({ default: m.SkillsPage })),
);
const CronPage = lazyWithRetry(() =>
  import("@/pages/cron/cron-page").then((m) => ({ default: m.CronPage })),
);
const ConfigPage = lazyWithRetry(() =>
  import("@/pages/config/config-page").then((m) => ({ default: m.ConfigPage })),
);
const TracesPage = lazyWithRetry(() =>
  import("@/pages/traces/traces-page").then((m) => ({ default: m.TracesPage })),
);
const ChannelsPage = lazyWithRetry(() =>
  import("@/pages/channels/channels-page").then((m) => ({ default: m.ChannelsPage })),
);
const ApprovalsPage = lazyWithRetry(() =>
  import("@/pages/approvals/approvals-page").then((m) => ({ default: m.ApprovalsPage })),
);
const NodesPage = lazyWithRetry(() =>
  import("@/pages/nodes/nodes-page").then((m) => ({ default: m.NodesPage })),
);
const LogsPage = lazyWithRetry(() =>
  import("@/pages/logs/logs-page").then((m) => ({ default: m.LogsPage })),
);
const ProvidersPage = lazyWithRetry(() =>
  import("@/pages/providers/providers-page").then((m) => ({ default: m.ProvidersPage })),
);
const CustomToolsPage = lazyWithRetry(() =>
  import("@/pages/custom-tools/custom-tools-page").then((m) => ({ default: m.CustomToolsPage })),
);
const MCPPage = lazyWithRetry(() =>
  import("@/pages/mcp/mcp-page").then((m) => ({ default: m.MCPPage })),
);
const TeamsPage = lazyWithRetry(() =>
  import("@/pages/teams/teams-page").then((m) => ({ default: m.TeamsPage })),
);
const BuiltinToolsPage = lazyWithRetry(() =>
  import("@/pages/builtin-tools/builtin-tools-page").then((m) => ({ default: m.BuiltinToolsPage })),
);
const TtsPage = lazyWithRetry(() =>
  import("@/pages/tts/tts-page").then((m) => ({ default: m.TtsPage })),
);
const EventsPage = lazyWithRetry(() =>
  import("@/pages/events/events-page").then((m) => ({ default: m.EventsPage })),
);
const StoragePage = lazyWithRetry(() =>
  import("@/pages/storage/storage-page").then((m) => ({ default: m.StoragePage })),
);
const SetupPage = lazyWithRetry(() =>
  import("@/pages/setup/setup-page").then((m) => ({ default: m.SetupPage })),
);
const PendingMessagesPage = lazyWithRetry(() =>
  import("@/pages/pending-messages/pending-messages-page").then((m) => ({ default: m.PendingMessagesPage })),
);
const MemoryPage = lazyWithRetry(() =>
  import("@/pages/memory/memory-page").then((m) => ({ default: m.MemoryPage })),
);
const KnowledgeGraphPage = lazyWithRetry(() =>
  import("@/pages/knowledge-graph/knowledge-graph-page").then((m) => ({ default: m.KnowledgeGraphPage })),
);
const ContactsPage = lazyWithRetry(() =>
  import("@/pages/contacts/contacts-page").then((m) => ({ default: m.ContactsPage })),
);
const ActivityPage = lazyWithRetry(() =>
  import("@/pages/activity/activity-page").then((m) => ({ default: m.ActivityPage })),
);
const CliCredentialsPage = lazyWithRetry(() =>
  import("@/pages/cli-credentials/cli-credentials-page").then((m) => ({ default: m.CliCredentialsPage })),
);
const ApiKeysPage = lazyWithRetry(() =>
  import("@/pages/api-keys/api-keys-page").then((m) => ({ default: m.ApiKeysPage })),
);
const GatewayUsersPage = lazyWithRetry(() =>
  import("@/pages/gateway-users/gateway-users-page").then((m) => ({ default: m.GatewayUsersPage })),
);
const PackagesPage = lazyWithRetry(() =>
  import("@/pages/packages/packages-page").then((m) => ({ default: m.PackagesPage })),
);

function PageLoader() {
  return (
    <div className="flex h-full items-center justify-center">
      <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
    </div>
  );
}

export function AppRoutes() {
  return (
    <ErrorBoundary>
    <Suspense fallback={<PageLoader />}>
      <Routes>
        <Route path={ROUTES.LOGIN} element={<LoginPage />} />

        {/* Setup wizard — standalone layout, requires auth but no sidebar */}
        <Route
          path={ROUTES.SETUP}
          element={
            <RequireAuth>
              <SetupPage />
            </RequireAuth>
          }
        />

        {/* Main app — requires auth + setup complete */}
        <Route
          element={
            <RequireAuth>
              <RequireSetup>
                <AppLayout />
              </RequireSetup>
            </RequireAuth>
          }
        >
          <Route index element={<Navigate to={ROUTES.OVERVIEW} replace />} />
          <Route path={ROUTES.OVERVIEW} element={<OverviewPage />} />
          <Route path={ROUTES.CHAT} element={<ChatPage />} />
          <Route path={ROUTES.CHAT_SESSION} element={<ChatPage />} />
          <Route path={ROUTES.AGENTS} element={<AgentsPage key="list" />} />
          <Route path={ROUTES.AGENT_DETAIL} element={<AgentsPage key="detail" />} />
          <Route path={ROUTES.TEAMS} element={<RequireFeature route={ROUTES.TEAMS}><TeamsPage key="list" /></RequireFeature>} />
          <Route path={ROUTES.TEAM_DETAIL} element={<RequireFeature route={ROUTES.TEAMS}><TeamsPage key="detail" /></RequireFeature>} />
          <Route path={ROUTES.SESSIONS} element={<RequireFeature route={ROUTES.SESSIONS}><SessionsPage key="list" /></RequireFeature>} />
          <Route path={ROUTES.SESSION_DETAIL} element={<RequireFeature route={ROUTES.SESSIONS}><SessionsPage key="detail" /></RequireFeature>} />
          <Route path={ROUTES.SKILLS} element={<RequireFeature route={ROUTES.SKILLS}><SkillsPage key="list" /></RequireFeature>} />
          <Route path={ROUTES.SKILL_DETAIL} element={<RequireFeature route={ROUTES.SKILLS}><SkillsPage key="detail" /></RequireFeature>} />
          <Route path={ROUTES.CRON} element={<RequireFeature route={ROUTES.CRON}><CronPage /></RequireFeature>} />
          <Route path={ROUTES.CRON_DETAIL} element={<RequireFeature route={ROUTES.CRON}><CronPage /></RequireFeature>} />
          <Route path={ROUTES.CONFIG} element={<ConfigPage />} />
          <Route path={ROUTES.TRACES} element={<RequireFeature route={ROUTES.TRACES}><TracesPage key="list" /></RequireFeature>} />
          <Route path={ROUTES.TRACE_DETAIL} element={<RequireFeature route={ROUTES.TRACES}><TracesPage key="detail" /></RequireFeature>} />
          <Route path={ROUTES.EVENTS} element={<RequireFeature route={ROUTES.EVENTS}><EventsPage /></RequireFeature>} />
          <Route path={ROUTES.USAGE} element={<Navigate to={ROUTES.OVERVIEW} replace />} />
          <Route path={ROUTES.ACTIVITY} element={<RequireFeature route={ROUTES.ACTIVITY}><ActivityPage /></RequireFeature>} />
          <Route path={ROUTES.CHANNELS} element={<RequireFeature route={ROUTES.CHANNELS}><ChannelsPage key="list" /></RequireFeature>} />
          <Route path={ROUTES.CHANNEL_DETAIL} element={<RequireFeature route={ROUTES.CHANNELS}><ChannelsPage key="detail" /></RequireFeature>} />
          <Route path={ROUTES.CONTACTS} element={<RequireFeature route={ROUTES.CONTACTS}><ContactsPage /></RequireFeature>} />
          <Route path={ROUTES.APPROVALS} element={<RequireFeature route={ROUTES.APPROVALS}><ApprovalsPage /></RequireFeature>} />
          <Route path={ROUTES.NODES} element={<RequireFeature route={ROUTES.NODES}><NodesPage /></RequireFeature>} />
          <Route path={ROUTES.LOGS} element={<RequireFeature route={ROUTES.LOGS}><LogsPage /></RequireFeature>} />
          <Route path={ROUTES.PROVIDERS} element={<ProvidersPage />} />
          <Route path={ROUTES.CUSTOM_TOOLS} element={<CustomToolsPage />} />
          <Route path={ROUTES.BUILTIN_TOOLS} element={<RequireFeature route={ROUTES.BUILTIN_TOOLS}><BuiltinToolsPage /></RequireFeature>} />
          <Route path={ROUTES.MCP} element={<RequireFeature route={ROUTES.MCP}><MCPPage /></RequireFeature>} />
          <Route path={ROUTES.TTS} element={<RequireFeature route={ROUTES.TTS}><TtsPage /></RequireFeature>} />
          <Route path={ROUTES.STORAGE} element={<RequireFeature route={ROUTES.STORAGE}><StoragePage /></RequireFeature>} />
          <Route path={ROUTES.PENDING_MESSAGES} element={<RequireFeature route={ROUTES.PENDING_MESSAGES}><PendingMessagesPage /></RequireFeature>} />
          <Route path={ROUTES.MEMORY} element={<RequireFeature route={ROUTES.MEMORY}><MemoryPage /></RequireFeature>} />
          <Route path={ROUTES.KNOWLEDGE_GRAPH} element={<RequireFeature route={ROUTES.KNOWLEDGE_GRAPH}><KnowledgeGraphPage /></RequireFeature>} />
          <Route path={ROUTES.CLI_CREDENTIALS} element={<CliCredentialsPage />} />
          <Route path={ROUTES.API_KEYS} element={<ApiKeysPage />} />
          <Route path={ROUTES.GATEWAY_USERS} element={<GatewayUsersPage />} />
          <Route path={ROUTES.PACKAGES} element={<PackagesPage />} />
        </Route>

        {/* Catch-all → overview */}
        <Route path="*" element={<Navigate to={ROUTES.OVERVIEW} replace />} />
      </Routes>
    </Suspense>
    </ErrorBoundary>
  );
}
