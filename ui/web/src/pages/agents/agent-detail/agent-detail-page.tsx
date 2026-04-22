import { useState, lazy, Suspense } from "react";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { useAgentDetail } from "../hooks/use-agent-detail";
import { useAgents } from "../hooks/use-agents";
import { useAgentHeartbeat } from "../hooks/use-agent-heartbeat";
import { AgentHeader } from "./agent-header";
import { AgentOverviewTab } from "./agent-overview-tab";
import { AgentFilesTab } from "./agent-files-tab";
import { AgentInstancesTab } from "./agent-instances-tab";
import { AgentPermissionsTab } from "./agent-permissions-tab";
import { AgentEvolutionTab } from "./evolution-tab/agent-evolution-tab";
import { AgentHooksTab } from "./agent-hooks-tab";
import { SummoningModal } from "../summoning-modal";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { DetailPageSkeleton } from "@/components/shared/loading-skeleton";
import { agentDisplayName } from "./agent-display-utils";
import { SystemPromptDialog } from "./system-prompt-dialog";

const AgentAdvancedDialog = lazy(() =>
  import("./agent-advanced-dialog").then((m) => ({ default: m.AgentAdvancedDialog }))
);
const HeartbeatConfigDialog = lazy(() =>
  import("./heartbeat-config-dialog").then((m) => ({ default: m.HeartbeatConfigDialog }))
);

interface AgentDetailPageProps {
  agentId: string;
  onBack: () => void;
}

export function AgentDetailPage({ agentId, onBack }: AgentDetailPageProps) {
  const { t } = useTranslation("agents");
  const navigate = useNavigate();
  const { agent, files, loading, updateAgent, getFile, setFile, regenerateAgent, resummonAgent, refresh } =
    useAgentDetail(agentId);
  const { deleteAgent: deleteAgentById } = useAgents();
  const hb = useAgentHeartbeat(agentId);
  const [summoningOpen, setSummoningOpen] = useState(false);
  const [activeTab, setActiveTab] = useState("agent");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [heartbeatOpen, setHeartbeatOpen] = useState(false);
  const [promptOpen, setPromptOpen] = useState(false);
  const [hooksCreateOpen, setHooksCreateOpen] = useState(false);

  const handleResummon = async () => {
    await resummonAgent();
    setSummoningOpen(true);
  };

  const handleSummoningClose = (open: boolean) => {
    setSummoningOpen(open);
    if (!open) refresh();
  };

  if (loading || !agent) {
    return <DetailPageSkeleton tabs={3} />;
  }

  const title = agentDisplayName(agent, t("card.unnamedAgent"));

  return (
    <div>
      <AgentHeader
        agent={agent}
        heartbeat={hb.config}
        onBack={onBack}
        onDelete={() => setDeleteOpen(true)}
        onAdvanced={() => setAdvancedOpen(true)}
        onHeartbeat={() => setHeartbeatOpen(true)}
        onSystemPrompt={() => setPromptOpen(true)}
      />

      <div className="p-3 sm:p-4">
        <div className="max-w-4xl">
          <Tabs value={activeTab} onValueChange={setActiveTab}>
            <TabsList className="w-full justify-start overflow-x-auto overflow-y-hidden">
              <TabsTrigger value="agent">{t("detail.tabs.agent")}</TabsTrigger>
              <TabsTrigger value="files">{t("detail.tabs.files")}</TabsTrigger>
              <TabsTrigger value="permissions">{t("detail.tabs.permissions")}</TabsTrigger>
              <TabsTrigger value="evolution">{t("detail.tabs.evolution")}</TabsTrigger>
              <TabsTrigger value="hooks">{t("detail.tabs.hooks")}</TabsTrigger>
              {agent.agent_type === "predefined" && (
                <TabsTrigger value="instances">{t("detail.tabs.instances")}</TabsTrigger>
              )}
            </TabsList>

            <TabsContent value="agent" className="mt-4">
              <AgentOverviewTab
                key={agent.id + "-" + agent.updated_at}
                agent={agent}
                onUpdate={updateAgent}
                heartbeat={hb}
                onManageCodexPool={() => navigate(`/agents/${agent.id}/codex-pool`)}
                onViewHooks={() => setActiveTab("hooks")}
                onAddHook={() => {
                  setActiveTab("hooks");
                  setHooksCreateOpen(true);
                }}
              />
            </TabsContent>

            <TabsContent value="files" className="mt-4">
              <AgentFilesTab
                agent={agent}
                files={files}
                onGetFile={getFile}
                onSetFile={setFile}
                onRegenerate={regenerateAgent}
                onResummon={handleResummon}
                onRegenerateCompleted={refresh}
              />
            </TabsContent>

            <TabsContent value="permissions" className="mt-4">
              <AgentPermissionsTab agentId={agentId} />
            </TabsContent>

            <TabsContent value="evolution" className="mt-4">
              <AgentEvolutionTab
                agentId={agentId}
                agentOtherConfig={agent.other_config as Record<string, unknown> | undefined}
              />
            </TabsContent>

            <TabsContent value="hooks" className="mt-4">
              <AgentHooksTab
                agentId={agentId}
                initialCreateOpen={hooksCreateOpen}
                onCreateOpenChange={setHooksCreateOpen}
              />
            </TabsContent>

            {agent.agent_type === "predefined" && (
              <TabsContent value="instances" className="mt-4">
                <AgentInstancesTab agentId={agentId} />
              </TabsContent>
            )}
          </Tabs>
        </div>
      </div>

      {advancedOpen ? (
        <Suspense fallback={null}>
          <AgentAdvancedDialog
            key={agent.id}
            open={advancedOpen}
            onOpenChange={setAdvancedOpen}
            agent={agent}
            onUpdate={updateAgent}
          />
        </Suspense>
      ) : null}

      <SummoningModal
        open={summoningOpen}
        onOpenChange={handleSummoningClose}
        agentId={agentId}
        agentName={title}
        onCompleted={() => {}}
        onResummon={async () => { await resummonAgent(); }}
      />

      {heartbeatOpen && (
        <Suspense fallback={null}>
          <HeartbeatConfigDialog
            open={heartbeatOpen}
            onOpenChange={setHeartbeatOpen}
            config={hb.config}
            saving={hb.saving}
            update={hb.update}
            test={hb.test}
            getChecklist={hb.getChecklist}
            setChecklist={hb.setChecklist}
            fetchTargets={hb.fetchTargets}
            refresh={hb.refresh}
            agentProvider={agent?.provider}
            agentModel={agent?.model}
          />
        </Suspense>
      )}

      {promptOpen && (
        <SystemPromptDialog
          agentKey={agent.agent_key}
          open={promptOpen}
          onOpenChange={setPromptOpen}
        />
      )}

      <ConfirmDeleteDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title={t("delete.title")}
        description={t("delete.detailDescription", { name: title })}
        confirmValue={agent.display_name || agent.agent_key}
        confirmLabel={t("delete.confirmLabel")}
        onConfirm={async () => {
          await deleteAgentById(agentId);
          setDeleteOpen(false);
          onBack();
        }}
      />
    </div>
  );
}
