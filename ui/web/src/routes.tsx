import { Suspense } from "react";
import { Routes, Route, Navigate } from "react-router";
import { AppLayout } from "@/components/layout/app-layout";
import { RequireAuth } from "@/components/shared/require-auth";
import { RequireAdmin, RequireCrossTenant } from "@/components/shared/require-role";
import { RequireSetup } from "@/components/shared/require-setup";
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
const PackagesPage = lazyWithRetry(() =>
  import("@/pages/packages/packages-page").then((m) => ({ default: m.PackagesPage })),
);
const TenantsAdminPage = lazyWithRetry(() =>
  import("@/pages/tenants-admin/tenants-admin-page").then((m) => ({ default: m.TenantsAdminPage })),
);
const TenantDetailPage = lazyWithRetry(() =>
  import("@/pages/tenants-admin/tenant-detail-page").then((m) => ({ default: m.TenantDetailPage })),
);
const TenantSelectorPage = lazyWithRetry(() =>
  import("@/pages/login/tenant-selector").then((m) => ({ default: m.TenantSelectorPage })),
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

        {/* Tenant selector — accessible when authenticated but tenant not yet selected */}
        <Route path={ROUTES.SELECT_TENANT} element={<TenantSelectorPage />} />

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
          <Route path={ROUTES.CHAT_PATTERN} element={<ChatPage />} />
          <Route path={ROUTES.AGENTS} element={<AgentsPage key="list" />} />
          <Route path={ROUTES.AGENT_DETAIL} element={<AgentsPage key="detail" />} />
          <Route path={ROUTES.TEAMS} element={<TeamsPage key="list" />} />
          <Route path={ROUTES.TEAM_DETAIL} element={<TeamsPage key="detail" />} />
          <Route path={ROUTES.SESSIONS} element={<SessionsPage key="list" />} />
          <Route path={ROUTES.SESSION_DETAIL} element={<SessionsPage key="detail" />} />
          <Route path={ROUTES.SKILLS} element={<SkillsPage key="list" />} />
          <Route path={ROUTES.SKILL_DETAIL} element={<SkillsPage key="detail" />} />
          <Route path={ROUTES.CRON} element={<CronPage key="list" />} />
          <Route path={ROUTES.CRON_DETAIL} element={<CronPage key="detail" />} />
          {/* Admin-only pages */}
          <Route path={ROUTES.CONFIG} element={<RequireCrossTenant><ConfigPage /></RequireCrossTenant>} />
          <Route path={ROUTES.PROVIDERS} element={<RequireAdmin><ProvidersPage key="list" /></RequireAdmin>} />
          <Route path={ROUTES.PROVIDER_DETAIL} element={<RequireAdmin><ProvidersPage key="detail" /></RequireAdmin>} />
          <Route path={ROUTES.CLI_CREDENTIALS} element={<RequireAdmin><CliCredentialsPage /></RequireAdmin>} />
          <Route path={ROUTES.API_KEYS} element={<RequireAdmin><ApiKeysPage /></RequireAdmin>} />
          <Route path={ROUTES.CHANNELS} element={<RequireAdmin><ChannelsPage key="list" /></RequireAdmin>} />
          <Route path={ROUTES.CHANNEL_DETAIL} element={<RequireAdmin><ChannelsPage key="detail" /></RequireAdmin>} />
          <Route path={ROUTES.NODES} element={<RequireAdmin><NodesPage /></RequireAdmin>} />
          <Route path={ROUTES.LOGS} element={<RequireAdmin><LogsPage /></RequireAdmin>} />
          <Route path={ROUTES.BUILTIN_TOOLS} element={<RequireAdmin><BuiltinToolsPage /></RequireAdmin>} />
          <Route path={ROUTES.MCP} element={<RequireAdmin><MCPPage /></RequireAdmin>} />
          <Route path={ROUTES.TTS} element={<RequireAdmin><TtsPage /></RequireAdmin>} />
          <Route path={ROUTES.STORAGE} element={<RequireAdmin><StoragePage /></RequireAdmin>} />
          <Route path={ROUTES.PACKAGES} element={<RequireAdmin><PackagesPage /></RequireAdmin>} />
          <Route path={ROUTES.TENANTS} element={<RequireCrossTenant><TenantsAdminPage /></RequireCrossTenant>} />
          <Route path={ROUTES.TENANT_DETAIL} element={<RequireCrossTenant><TenantDetailPage /></RequireCrossTenant>} />

          {/* Operator+ pages */}
          <Route path={ROUTES.TRACES} element={<TracesPage key="list" />} />
          <Route path={ROUTES.TRACE_DETAIL} element={<TracesPage key="detail" />} />
          <Route path={ROUTES.EVENTS} element={<EventsPage />} />
          <Route path={ROUTES.USAGE} element={<Navigate to={ROUTES.OVERVIEW} replace />} />
          <Route path={ROUTES.ACTIVITY} element={<ActivityPage />} />
          <Route path={ROUTES.CONTACTS} element={<ContactsPage />} />
          <Route path={ROUTES.APPROVALS} element={<ApprovalsPage />} />
          <Route path={ROUTES.PENDING_MESSAGES} element={<PendingMessagesPage />} />
          <Route path={ROUTES.MEMORY} element={<MemoryPage />} />
          <Route path={ROUTES.KNOWLEDGE_GRAPH} element={<KnowledgeGraphPage />} />
        </Route>

        {/* Catch-all → overview */}
        <Route path="*" element={<Navigate to={ROUTES.OVERVIEW} replace />} />
      </Routes>
    </Suspense>
    </ErrorBoundary>
  );
}
