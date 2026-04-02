import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import { AlertTriangle, CheckCircle2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useChannelDetail } from "../hooks/use-channel-detail";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { ChannelHeader } from "./channel-header";
import { ChannelGeneralTab } from "./channel-general-tab";
import { ChannelCredentialsTab } from "./channel-credentials-tab";
import { ChannelGroupsTab } from "./channel-groups-tab";
import { ChannelManagersTab } from "./channel-managers-tab";
import { ChannelAdvancedDialog } from "./channel-advanced-dialog";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { useChannels } from "../hooks/use-channels";
import { channelsWithAuth, reauthDialogs } from "../channel-wizard-registry";
import {
  formatRelativeTime,
  getChannelCheckedLabel,
  getChannelRemediationMeta,
  getRenderableChannelStatus,
  getChannelStatusMeta,
} from "../channels-status-view";

interface ChannelDetailPageProps {
  instanceId: string;
  onBack: () => void;
  onDelete?: (instance: { id: string; name: string }) => void;
}

const DEFAULT_CHANNEL_DETAIL_TAB = "general";
const baseChannelDetailTabs = new Set(["general", "credentials", "managers"]);

export function resolveChannelDetailTab(
  requestedTab: string | null,
  isTelegram: boolean,
) {
  if (!requestedTab) return DEFAULT_CHANNEL_DETAIL_TAB;
  if (requestedTab === "groups") {
    return isTelegram ? "groups" : DEFAULT_CHANNEL_DETAIL_TAB;
  }
  return baseChannelDetailTabs.has(requestedTab)
    ? requestedTab
    : DEFAULT_CHANNEL_DETAIL_TAB;
}

export function ChannelDetailPage({
  instanceId,
  onBack,
  onDelete,
}: ChannelDetailPageProps) {
  const { t } = useTranslation("channels");
  const [searchParams] = useSearchParams();
  const {
    instance,
    loading,
    updateInstance,
    listManagerGroups,
    listManagers,
    addManager,
    removeManager,
  } = useChannelDetail(instanceId);
  const { agents } = useAgents();
  const { channels } = useChannels();
  const [activeTab, setActiveTab] = useState("general");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [reauthOpen, setReauthOpen] = useState(false);

  const status = instance
    ? getRenderableChannelStatus(channels[instance.name] ?? null, instance)
    : null;
  const agentName = (() => {
    if (!instance) return "";
    const agent = agents.find((a) => a.id === instance.agent_id);
    return (
      agent?.display_name || agent?.agent_key || instance.agent_id.slice(0, 8)
    );
  })();

  const isTelegram = instance?.channel_type === "telegram";
  const supportsReauth = instance
    ? channelsWithAuth.has(instance.channel_type)
    : false;
  const statusMeta = getChannelStatusMeta(status, instance?.enabled ?? false, t);
  const remediation = getChannelRemediationMeta(status, supportsReauth, t);
  const checkedLabel = getChannelCheckedLabel(status, t);

  useEffect(() => {
    if (!instance) return;
    setActiveTab(resolveChannelDetailTab(searchParams.get("tab"), isTelegram));
  }, [instance, isTelegram, searchParams]);

  useEffect(() => {
    if (!instance) return;
    if (searchParams.get("advanced") === "1") {
      setAdvancedOpen(true);
    }
  }, [instance, searchParams]);

  const handleDelete = () => {
    if (onDelete) {
      setDeleteOpen(true);
    }
  };

  const handleRemediationAction = () => {
    switch (remediation?.target) {
      case "credentials":
        setActiveTab("credentials");
        break;
      case "advanced":
        setAdvancedOpen(true);
        break;
      case "reauth":
        if (supportsReauth) {
          setReauthOpen(true);
        }
        break;
      default:
        break;
    }
  };

  const headerAction =
    remediation && remediation.target !== "details"
      ? { label: remediation.label, onClick: handleRemediationAction }
      : null;

  const timelineItems = useMemo(() => {
    const items: Array<{ label: string; value: string }> = [];
    const firstFailed = formatRelativeTime(status?.first_failed_at);
    const lastChecked = formatRelativeTime(status?.checked_at);
    const lastHealthy = formatRelativeTime(status?.last_healthy_at);

    if (firstFailed) {
      items.push({
        label: t("detail.timeline.firstFailed", { defaultValue: "First failed" }),
        value: firstFailed,
      });
    }
    if (lastChecked) {
      items.push({
        label: t("detail.timeline.lastChecked", { defaultValue: "Last checked" }),
        value: lastChecked,
      });
    }
    if (status?.consecutive_failures) {
      items.push({
        label: t("detail.timeline.failures", { defaultValue: "Failures" }),
        value: t("detail.timeline.failureStreak", {
          defaultValue: "{{count}} in a row",
          count: status.consecutive_failures,
        }),
      });
    } else if (status?.failure_count) {
      items.push({
        label: t("detail.timeline.failures", { defaultValue: "Failures" }),
        value: t("detail.timeline.failureTotal", {
          defaultValue: "{{count}} total",
          count: status.failure_count,
        }),
      });
    }
    if (lastHealthy) {
      items.push({
        label: t("detail.timeline.lastHealthy", { defaultValue: "Last healthy" }),
        value: lastHealthy,
      });
    }

    return items;
  }, [
    status?.checked_at,
    status?.consecutive_failures,
    status?.failure_count,
    status?.first_failed_at,
    status?.last_healthy_at,
    t,
  ]);

  const showDiagnosticsCard =
    status?.state === "failed" ||
    status?.state === "degraded" ||
    !!status?.remediation ||
    !!status?.consecutive_failures ||
    !!status?.first_failed_at;

  const neutralHealthNote =
    !showDiagnosticsCard &&
    (status?.state === "healthy" || status?.state === "starting") &&
    checkedLabel;

  const diagnosticsHint =
    remediation?.hint ||
    t("detail.reviewDiagnostics", {
      defaultValue: "Review the latest diagnosis in this channel before changing settings.",
    });

  const ReauthDialog = supportsReauth
    ? reauthDialogs[instance?.channel_type ?? ""]
    : null;

  if (loading || !instance) {
    return <DetailPageSkeleton tabs={4} />;
  }

  return (
    <div>
      <ChannelHeader
        instance={instance}
        status={status}
        agentName={agentName}
        onBack={onBack}
        onAdvanced={() => setAdvancedOpen(true)}
        onDelete={handleDelete}
        primaryAction={headerAction}
      />

      <div className="p-3 sm:p-4">
        <div className="max-w-4xl space-y-4">
          {showDiagnosticsCard && status && (
            <div
              className={cn(
                "rounded-xl border p-4",
                statusMeta.surfaceClass,
              )}
            >
              <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_220px]">
                <div>
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <AlertTriangle className="h-4 w-4" />
                    <span>
                      {t("detail.whatHappened", {
                        defaultValue: "What happened",
                      })}
                    </span>
                  </div>
                  <p className="mt-2 text-base font-semibold">
                    {status.summary || statusMeta.label}
                  </p>
                  <p className="mt-1 text-sm text-muted-foreground">
                    {status.remediation?.headline || diagnosticsHint}
                  </p>

                  <div className="mt-4">
                    <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                      {t("detail.recommendedAction", {
                        defaultValue: "Recommended action",
                      })}
                    </p>
                    <p className="mt-2 text-sm font-medium">
                      {remediation?.label ||
                        t("actions.inspect", { defaultValue: "Inspect issue" })}
                    </p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {diagnosticsHint}
                    </p>
                    {remediation && remediation.target !== "details" && (
                      <Button
                        size="sm"
                        onClick={handleRemediationAction}
                        className="mt-3 sm:hidden"
                      >
                        {remediation.label}
                      </Button>
                    )}
                  </div>

                  {status.detail && (
                    <details className="mt-4 rounded-lg border border-border/80 bg-background/60 p-3">
                      <summary className="cursor-pointer text-sm font-medium">
                        {t("detail.technicalDetail", {
                          defaultValue: "Technical detail",
                        })}
                      </summary>
                      <p className="mt-2 break-words text-xs text-muted-foreground">
                        {status.detail}
                      </p>
                    </details>
                  )}
                </div>

                <div>
                  <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-muted-foreground">
                    {t("detail.timeline.title", { defaultValue: "Timeline" })}
                  </p>
                  <div className="mt-3 space-y-2">
                    {timelineItems.length > 0 ? (
                      timelineItems.map((item) => (
                        <div
                          key={item.label}
                          className="flex items-start justify-between gap-4 rounded-lg bg-background/60 px-3 py-2"
                        >
                          <span className="text-xs text-muted-foreground">
                            {item.label}
                          </span>
                          <span className="text-right text-xs font-medium tabular-nums">
                            {item.value}
                          </span>
                        </div>
                      ))
                    ) : (
                      <div className="rounded-lg bg-background/60 px-3 py-2 text-xs text-muted-foreground">
                        {checkedLabel ||
                          t("detail.timeline.noData", {
                            defaultValue: "No recent channel checks recorded yet.",
                          })}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
          )}

          {neutralHealthNote && (
            <div className="flex items-center gap-2 rounded-lg border border-emerald-200/70 bg-emerald-500/[0.04] px-3 py-2 text-sm dark:border-emerald-500/20 dark:bg-emerald-500/10">
              <CheckCircle2 className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
              <span className="text-muted-foreground">{neutralHealthNote}</span>
            </div>
          )}

          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
              <TabsTrigger value="general">
                {t("detail.tabs.general")}
              </TabsTrigger>
              <TabsTrigger value="credentials">
                {t("detail.tabs.credentials")}
              </TabsTrigger>
              {isTelegram && (
                <TabsTrigger value="groups">
                  {t("detail.tabs.groups")}
                </TabsTrigger>
              )}
              <TabsTrigger value="managers">
                {t("detail.tabs.managers")}
              </TabsTrigger>
            </TabsList>

            <TabsContent value="general" className="mt-4">
              <ChannelGeneralTab
                instance={instance}
                agents={agents}
                onUpdate={updateInstance}
              />
            </TabsContent>

            <TabsContent value="credentials" className="mt-4">
              <ChannelCredentialsTab
                instance={instance}
                onUpdate={updateInstance}
              />
            </TabsContent>

            {isTelegram && (
              <TabsContent value="groups" className="mt-4">
                <ChannelGroupsTab
                  instance={instance}
                  onUpdate={updateInstance}
                  listManagerGroups={listManagerGroups}
                />
              </TabsContent>
            )}

            <TabsContent value="managers" className="mt-4">
              <ChannelManagersTab
                listManagerGroups={listManagerGroups}
                listManagers={listManagers}
                addManager={addManager}
                removeManager={removeManager}
              />
            </TabsContent>
          </Tabs>
        </div>
      </div>

      <ChannelAdvancedDialog
        open={advancedOpen}
        onOpenChange={setAdvancedOpen}
        instance={instance}
        onUpdate={updateInstance}
      />

      {ReauthDialog && (
        <ReauthDialog
          open={reauthOpen}
          onOpenChange={setReauthOpen}
          instanceId={instance.id}
          instanceName={instance.display_name || instance.name}
          onSuccess={() => {
            setReauthOpen(false);
          }}
        />
      )}

      {onDelete && (
        <ConfirmDeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title={t("delete.title")}
          description={t("delete.description", {
            name: instance.display_name || instance.name,
          })}
          confirmValue={instance.display_name || instance.name}
          confirmLabel={t("delete.confirmLabel")}
          onConfirm={async () => {
            onDelete({
              id: instance.id,
              name: instance.display_name || instance.name,
            });
            setDeleteOpen(false);
          }}
        />
      )}
    </div>
  );
}
