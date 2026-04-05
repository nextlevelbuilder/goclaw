import { useState, useCallback } from "react";
import { useParams } from "react-router";
import {
  Monitor,
  Play,
  Square,
  RefreshCw,
  ExternalLink,
  Globe,
  Loader2,
  Bot,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import {
  useBrowserStatus,
  type BrowserTab,
} from "./hooks/use-browser-status";
import { BrowserViewer } from "./browser-viewer";
import { useAgents } from "@/pages/agents/hooks/use-agents";

export function BrowserPage() {
  const { targetId } = useParams<{ targetId: string }>();

  if (targetId) {
    return <BrowserDetailView targetId={targetId} />;
  }
  return <BrowserListView />;
}

function BrowserListView() {
  const { t } = useTranslation("browser");
  const { t: tCommon } = useTranslation("common");
  const [agentFilter, setAgentFilter] = useState<string | undefined>();
  const {
    status,
    tabs,
    loading,
    actionLoading,
    startBrowser,
    stopBrowser,
    createLiveSession,
    refresh,
  } = useBrowserStatus();
  const { agents } = useAgents();
  const [viewingTab, setViewingTab] = useState<{
    tab: BrowserTab;
    token: string;
  } | null>(null);
  const [sessionLoading, setSessionLoading] = useState<string | null>(null);

  const handleViewTab = useCallback(
    async (tab: BrowserTab) => {
      setSessionLoading(tab.targetId);
      try {
        const session = await createLiveSession(tab.targetId, "takeover");
        setViewingTab({ tab, token: session.token });
      } catch {
        // ignore
      } finally {
        setSessionLoading(null);
      }
    },
    [createLiveSession],
  );

  // Filter tabs by agent on the client side.
  // Tabs with empty agentKey (opened before tracking) are shown under every filter.
  const filteredTabs = agentFilter
    ? tabs.filter((t) => t.agentKey === agentFilter || !t.agentKey)
    : tabs;

  // Full-screen viewer mode
  if (viewingTab) {
    return (
      <div className="flex h-full flex-col">
        <BrowserViewer
          token={viewingTab.token}
          initialMode="takeover"
          onClose={() => setViewingTab(null)}
          tabTitle={viewingTab.tab.title}
          tabUrl={viewingTab.tab.url}
          className="h-full"
        />
      </div>
    );
  }

  // Agents that have browser tool enabled (or just show all for simplicity)
  const agentOptions = agents.map((a) => ({
    key: a.agent_key || a.id,
    label: a.agent_key || a.id,
  }));

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            {status && (
              <Badge variant={status.running ? "default" : "secondary"}>
                {status.running ? t("running") : t("stopped")}
              </Badge>
            )}
            {status?.headless === false && status.running && (
              <Badge variant="outline">{t("visible")}</Badge>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={loading}
              className="gap-1"
            >
              <RefreshCw
                className={
                  "h-3.5 w-3.5" + (loading ? " animate-spin" : "")
                }
              />
            </Button>
            {status?.running ? (
              <Button
                variant="destructive"
                size="sm"
                onClick={stopBrowser}
                disabled={actionLoading}
                className="gap-1"
              >
                <Square className="h-3.5 w-3.5" /> {t("stop")}
              </Button>
            ) : (
              <Button
                size="sm"
                onClick={startBrowser}
                disabled={actionLoading}
                className="gap-1"
              >
                <Play className="h-3.5 w-3.5" /> {t("start")}
              </Button>
            )}
          </div>
        }
      />

      {/* Agent filter — always visible when browser is running */}
      {status?.running && agentOptions.length > 0 && (
        <div className="mt-4 flex items-center gap-2">
          <Bot className="h-4 w-4 text-muted-foreground shrink-0" />
          <span className="text-sm text-muted-foreground shrink-0">{tCommon("agent")}:</span>
          <div className="flex flex-wrap gap-1">
            <button
              type="button"
              onClick={() => setAgentFilter(undefined)}
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                !agentFilter
                  ? "bg-primary text-primary-foreground"
                  : "bg-muted text-muted-foreground hover:bg-accent"
              }`}
            >
              {t("allAgents")}
            </button>
            {agentOptions.map((agent) => (
              <button
                type="button"
                key={agent.key}
                onClick={() => setAgentFilter(agent.key)}
                className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                  agentFilter === agent.key
                    ? "bg-primary text-primary-foreground"
                    : "bg-muted text-muted-foreground hover:bg-accent"
                }`}
              >
                {agent.label}
              </button>
            ))}
          </div>
        </div>
      )}

      <div className="mt-4">
        {!status?.running ? (
          <EmptyState
            icon={Monitor}
            title={t("notRunning")}
            description={t("notRunningDescription")}
            action={
              <Button size="sm" onClick={startBrowser} disabled={actionLoading} className="gap-1">
                <Play className="h-3.5 w-3.5" /> {t("start")}
              </Button>
            }
          />
        ) : filteredTabs.length === 0 ? (
          <EmptyState
            icon={Globe}
            title={t("noTabs")}
            description={agentFilter ? t("noTabsForAgent") : t("noTabsDescription")}
          />
        ) : (
          <div className="space-y-2">
            <h3 className="text-sm font-medium text-muted-foreground">
              {t("openTabs")} ({filteredTabs.length})
            </h3>
            <div className="grid gap-2">
              {filteredTabs.map((tab) => (
                <div
                  key={tab.targetId}
                  className="flex items-center gap-3 rounded-lg border bg-card p-3 shadow-sm hover:bg-accent/50 transition-colors cursor-pointer"
                >
                  <Globe className="h-5 w-5 shrink-0 text-muted-foreground" />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium">
                      {tab.title || "Untitled"}
                    </span>
                    <div className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-xs text-muted-foreground font-mono">
                        {tab.url}
                      </span>
                      {tab.agentKey && !agentFilter && (
                        <Badge variant="outline" className="shrink-0 text-[10px] px-1.5 py-0 h-4 gap-0.5">
                          <Bot className="h-2.5 w-2.5" />
                          {tab.agentKey}
                        </Badge>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-1.5 shrink-0">
                    <Button
                      variant="outline"
                      size="sm"
                      className="gap-1 h-7 text-xs"
                      onClick={() => handleViewTab(tab)}
                      disabled={sessionLoading === tab.targetId}
                    >
                      {sessionLoading === tab.targetId ? (
                        <Loader2 className="h-3 w-3 animate-spin" />
                      ) : (
                        <Monitor className="h-3 w-3" />
                      )}
                      {t("view")}
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0"
                      onClick={() =>
                        window.open(`/browser/${tab.targetId}`, "_blank")
                      }
                      title={t("openInNewTab")}
                    >
                      <ExternalLink className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

function BrowserDetailView({ targetId }: { targetId: string }) {
  const { createLiveSession } = useBrowserStatus();
  const [token, setToken] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  // Create session on mount
  useState(() => {
    createLiveSession(targetId, "takeover")
      .then((res) => {
        setToken(res.token);
        setLoading(false);
      })
      .catch((err) => {
        setError(err.message ?? "Failed to create session");
        setLoading(false);
      });
  });

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="flex items-center gap-2 text-muted-foreground">
          <Loader2 className="h-5 w-5 animate-spin" />
          <span className="text-sm">Creating live session...</span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <p className="text-sm text-destructive">{error}</p>
          <Button
            variant="outline"
            size="sm"
            className="mt-2"
            onClick={() => window.history.back()}
          >
            Go back
          </Button>
        </div>
      </div>
    );
  }

  if (!token) return null;

  return (
    <BrowserViewer
      token={token}
      initialMode="takeover"
      onClose={() => window.history.back()}
      className="h-full"
    />
  );
}
