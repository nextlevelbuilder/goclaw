import { useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { CheckCircle2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useChannelDetail } from "../hooks/use-channel-detail";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { ChannelHeader } from "./channel-header";
import { ChannelGeneralTab } from "./channel-general-tab";
import { ChannelCredentialsTab } from "./channel-credentials-tab";
import { ChannelGroupsTab } from "./channel-groups-tab";
import { ChannelManagersTab } from "./channel-managers-tab";
import { ChannelDiagnosticsCard } from "./channel-diagnostics-card";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { useChannels } from "../hooks/use-channels";
import { channelsWithAuth } from "../channel-wizard-registry";
import {
  getChannelCheckedLabel,
  getChannelRemediationMeta,
  getRenderableChannelStatus,
  getChannelStatusMeta,
} from "../channels-status-view";
import { useChannelTimeline } from "./channel-detail-timeline-hook";
import { ChannelDetailDialogs } from "./channel-detail-dialogs";

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

  const timelineItems = useChannelTimeline(status, t);

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
            <ChannelDiagnosticsCard
              status={status}
              statusMeta={statusMeta}
              remediation={remediation}
              checkedLabel={checkedLabel}
              diagnosticsHint={diagnosticsHint}
              timelineItems={timelineItems}
              onRemediationAction={handleRemediationAction}
            />
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

      <ChannelDetailDialogs
        instance={instance}
        advancedOpen={advancedOpen}
        setAdvancedOpen={setAdvancedOpen}
        reauthOpen={reauthOpen}
        setReauthOpen={setReauthOpen}
        deleteOpen={deleteOpen}
        setDeleteOpen={setDeleteOpen}
        supportsReauth={supportsReauth}
        onDelete={onDelete}
        onUpdate={updateInstance}
      />
    </div>
  );
}
