import { lazy, Suspense } from "react";
import { Routes, Route, Navigate } from "react-router";
import { AppLayout } from "@/components/layout/app-layout";
import { RequireAuth } from "@/components/shared/require-auth";
import { RequireSetup } from "@/components/shared/require-setup";
import { ROUTES } from "@/lib/constants";

// Lazy-loaded pages
const LoginPage = lazy(() =>
  import("@/pages/login/login-page").then((m) => ({ default: m.LoginPage })),
);
const OverviewPage = lazy(() =>
  import("@/pages/overview/overview-page").then((m) => ({ default: m.OverviewPage })),
);
const ChatPage = lazy(() =>
  import("@/pages/chat/chat-page").then((m) => ({ default: m.ChatPage })),
);
const AgentsPage = lazy(() =>
  import("@/pages/agents/agents-page").then((m) => ({ default: m.AgentsPage })),
);
const SessionsPage = lazy(() =>
  import("@/pages/sessions/sessions-page").then((m) => ({ default: m.SessionsPage })),
);
const SkillsPage = lazy(() =>
  import("@/pages/skills/skills-page").then((m) => ({ default: m.SkillsPage })),
);
const CronPage = lazy(() =>
  import("@/pages/cron/cron-page").then((m) => ({ default: m.CronPage })),
);
const ConfigPage = lazy(() =>
  import("@/pages/config/config-page").then((m) => ({ default: m.ConfigPage })),
);
const TracesPage = lazy(() =>
  import("@/pages/traces/traces-page").then((m) => ({ default: m.TracesPage })),
);
const ChannelsPage = lazy(() =>
  import("@/pages/channels/channels-page").then((m) => ({ default: m.ChannelsPage })),
);
const ApprovalsPage = lazy(() =>
  import("@/pages/approvals/approvals-page").then((m) => ({ default: m.ApprovalsPage })),
);
const NodesPage = lazy(() =>
  import("@/pages/nodes/nodes-page").then((m) => ({ default: m.NodesPage })),
);
const LogsPage = lazy(() =>
  import("@/pages/logs/logs-page").then((m) => ({ default: m.LogsPage })),
);
const ProvidersPage = lazy(() =>
  import("@/pages/providers/providers-page").then((m) => ({ default: m.ProvidersPage })),
);
const CustomToolsPage = lazy(() =>
  import("@/pages/custom-tools/custom-tools-page").then((m) => ({ default: m.CustomToolsPage })),
);
const MCPPage = lazy(() =>
  import("@/pages/mcp/mcp-page").then((m) => ({ default: m.MCPPage })),
);
const TeamsPage = lazy(() =>
  import("@/pages/teams/teams-page").then((m) => ({ default: m.TeamsPage })),
);
const BuiltinToolsPage = lazy(() =>
  import("@/pages/builtin-tools/builtin-tools-page").then((m) => ({ default: m.BuiltinToolsPage })),
);
const TtsPage = lazy(() =>
  import("@/pages/tts/tts-page").then((m) => ({ default: m.TtsPage })),
);
const EventsPage = lazy(() =>
  import("@/pages/events/events-page").then((m) => ({ default: m.EventsPage })),
);
const DelegationsPage = lazy(() =>
  import("@/pages/delegations/delegations-page").then((m) => ({ default: m.DelegationsPage })),
);
const StoragePage = lazy(() =>
  import("@/pages/storage/storage-page").then((m) => ({ default: m.StoragePage })),
);
const SetupPage = lazy(() =>
  import("@/pages/setup/setup-page").then((m) => ({ default: m.SetupPage })),
);
const PendingMessagesPage = lazy(() =>
  import("@/pages/pending-messages/pending-messages-page").then((m) => ({ default: m.PendingMessagesPage })),
);
const MemoryPage = lazy(() =>
  import("@/pages/memory/memory-page").then((m) => ({ default: m.MemoryPage })),
);
const KnowledgeGraphPage = lazy(() =>
  import("@/pages/knowledge-graph/knowledge-graph-page").then((m) => ({ default: m.KnowledgeGraphPage })),
);
const ContactsPage = lazy(() =>
  import("@/pages/contacts/contacts-page").then((m) => ({ default: m.ContactsPage })),
);
const ActivityPage = lazy(() =>
  import("@/pages/activity/activity-page").then((m) => ({ default: m.ActivityPage })),
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
          <Route path={ROUTES.TEAMS} element={<TeamsPage key="list" />} />
          <Route path={ROUTES.TEAM_DETAIL} element={<TeamsPage key="detail" />} />
          <Route path={ROUTES.SESSIONS} element={<SessionsPage key="list" />} />
          <Route path={ROUTES.SESSION_DETAIL} element={<SessionsPage key="detail" />} />
          <Route path={ROUTES.SKILLS} element={<SkillsPage key="list" />} />
          <Route path={ROUTES.SKILL_DETAIL} element={<SkillsPage key="detail" />} />
          <Route path={ROUTES.CRON} element={<CronPage />} />
          <Route path={ROUTES.CRON_DETAIL} element={<CronPage />} />
          <Route path={ROUTES.CONFIG} element={<ConfigPage />} />
          <Route path={ROUTES.TRACES} element={<TracesPage key="list" />} />
          <Route path={ROUTES.TRACE_DETAIL} element={<TracesPage key="detail" />} />
          <Route path={ROUTES.EVENTS} element={<EventsPage />} />
          <Route path={ROUTES.DELEGATIONS} element={<DelegationsPage />} />
          <Route path={ROUTES.USAGE} element={<Navigate to={ROUTES.OVERVIEW} replace />} />
          <Route path={ROUTES.ACTIVITY} element={<ActivityPage />} />
          <Route path={ROUTES.CHANNELS} element={<ChannelsPage key="list" />} />
          <Route path={ROUTES.CHANNEL_DETAIL} element={<ChannelsPage key="detail" />} />
          <Route path={ROUTES.CONTACTS} element={<ContactsPage />} />
          <Route path={ROUTES.APPROVALS} element={<ApprovalsPage />} />
          <Route path={ROUTES.NODES} element={<NodesPage />} />
          <Route path={ROUTES.LOGS} element={<LogsPage />} />
          <Route path={ROUTES.PROVIDERS} element={<ProvidersPage />} />
          <Route path={ROUTES.CUSTOM_TOOLS} element={<CustomToolsPage />} />
          <Route path={ROUTES.BUILTIN_TOOLS} element={<BuiltinToolsPage />} />
          <Route path={ROUTES.MCP} element={<MCPPage />} />
          <Route path={ROUTES.TTS} element={<TtsPage />} />
          <Route path={ROUTES.STORAGE} element={<StoragePage />} />
          <Route path={ROUTES.PENDING_MESSAGES} element={<PendingMessagesPage />} />
          <Route path={ROUTES.MEMORY} element={<MemoryPage />} />
          <Route path={ROUTES.KNOWLEDGE_GRAPH} element={<KnowledgeGraphPage />} />
        </Route>

        {/* Catch-all → overview */}
        <Route path="*" element={<Navigate to={ROUTES.OVERVIEW} replace />} />
      </Routes>
    </Suspense>
  );
}
