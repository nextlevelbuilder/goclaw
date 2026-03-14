import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { ArrowLeft, Radio } from "lucide-react";
import { useChannelDetail } from "../hooks/use-channel-detail";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { ChannelGeneralTab } from "./channel-general-tab";
import { ChannelCredentialsTab } from "./channel-credentials-tab";
import { ChannelConfigTab } from "./channel-config-tab";
import { ChannelGroupsTab } from "./channel-groups-tab";
import { ChannelManagersTab } from "./channel-managers-tab";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { channelTypeLabels } from "../channels-status-view";
import { useChannels } from "../hooks/use-channels";

interface ChannelDetailPageProps {
  instanceId: string;
  onBack: () => void;
}

export function ChannelDetailPage({ instanceId, onBack }: ChannelDetailPageProps) {
  const { t } = useTranslation("channels");
  const {
    instance,
    loading,
    updateInstance,
    listManagerGroups,
    listManagers,
    addManager,
    removeManager,
    listContacts,
  } = useChannelDetail(instanceId);
  const { agents } = useAgents();
  const { channels } = useChannels();
  const [activeTab, setActiveTab] = useState("general");

  if (loading || !instance) {
    return <DetailPageSkeleton tabs={4} />;
  }

  const status = channels[instance.name] ?? null;
  const agentName = (() => {
    const agent = agents.find((a) => a.id === instance.agent_id);
    return agent?.display_name || agent?.agent_key || instance.agent_id.slice(0, 8);
  })();

  const isTelegram = instance.channel_type === "telegram";

  return (
    <div className="p-4 sm:p-6">
      {/* Header */}
      <div className="mb-6 flex items-start gap-4">
        <Button variant="ghost" size="icon" onClick={onBack} className="mt-0.5 shrink-0">
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
          <Radio className="h-6 w-6" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h2 className="truncate text-xl font-semibold">
              {instance.display_name || instance.name}
            </h2>
            <Badge variant={instance.enabled ? "success" : "secondary"}>
              {instance.enabled ? t("enabled") : t("disabled")}
            </Badge>
            {status && (
              <Badge variant={status.running ? "success" : "secondary"}>
                {status.running ? t("status.running") : t("status.stopped")}
              </Badge>
            )}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted-foreground">
            {instance.display_name && (
              <>
                <span className="font-mono text-xs">{instance.name}</span>
                <span className="text-border">|</span>
              </>
            )}
            <Badge variant="outline" className="text-[11px]">
              {channelTypeLabels[instance.channel_type] || instance.channel_type}
            </Badge>
            <span className="text-border">|</span>
            <span>{t("detail.agent", { name: agentName })}</span>
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="max-w-4xl rounded-xl border bg-card p-3 shadow-sm sm:p-4">
        <Tabs value={activeTab} onValueChange={setActiveTab}>
          <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
            <TabsTrigger value="general">{t("detail.tabs.general")}</TabsTrigger>
            <TabsTrigger value="credentials">{t("detail.tabs.credentials")}</TabsTrigger>
            <TabsTrigger value="config">{t("detail.tabs.config")}</TabsTrigger>
            {isTelegram && <TabsTrigger value="groups">{t("detail.tabs.groups")}</TabsTrigger>}
            <TabsTrigger value="managers">{t("detail.tabs.managers")}</TabsTrigger>
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

          <TabsContent value="config" className="mt-4">
            <ChannelConfigTab
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
              listContacts={listContacts}
            />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  );
}
