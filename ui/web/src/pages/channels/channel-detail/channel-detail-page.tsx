import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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

interface ChannelDetailPageProps {
  instanceId: string;
  onBack: () => void;
  onDelete?: (instance: { id: string; name: string }) => void;
}

export function ChannelDetailPage({ instanceId, onBack, onDelete }: ChannelDetailPageProps) {
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
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (loading || !instance) {
    return <DetailPageSkeleton tabs={4} />;
  }

  const status = channels[instance.name] ?? null;
  const agentName = (() => {
    const agent = agents.find((a) => a.id === instance.agent_id);
    return agent?.display_name || agent?.agent_key || instance.agent_id.slice(0, 8);
  })();

  const isTelegram = instance.channel_type === "telegram";

  const handleDelete = () => {
    if (onDelete) {
      setDeleteOpen(true);
    }
  };

  return (
    <div>
      <ChannelHeader
        instance={instance}
        status={status}
        agentName={agentName}
        onBack={onBack}
        onAdvanced={() => setAdvancedOpen(true)}
        onDelete={handleDelete}
      />

      <div className="p-3 sm:p-4">
        <div className="max-w-4xl">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
              <TabsTrigger value="general">{t("detail.tabs.general")}</TabsTrigger>
              <TabsTrigger value="credentials">{t("detail.tabs.credentials")}</TabsTrigger>
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

      <ChannelAdvancedDialog
        open={advancedOpen}
        onOpenChange={setAdvancedOpen}
        instance={instance}
        onUpdate={updateInstance}
      />

      {onDelete && (
        <ConfirmDeleteDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          title={t("delete.title")}
          description={t("delete.description", { name: instance.display_name || instance.name })}
          confirmValue={instance.display_name || instance.name}
          confirmLabel={t("delete.confirmLabel")}
          onConfirm={async () => {
            onDelete({ id: instance.id, name: instance.display_name || instance.name });
            setDeleteOpen(false);
          }}
        />
      )}
    </div>
  );
}
