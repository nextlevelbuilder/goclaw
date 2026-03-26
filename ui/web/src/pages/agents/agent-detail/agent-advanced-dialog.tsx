import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import type {
  AgentData, ChatGPTOAuthRoutingConfig, CompactionConfig, ContextPruningConfig,
  SandboxConfig, WorkspaceSharingConfig,
} from "@/types/agent";
import {
  ChatGPTOAuthRoutingSection, ThinkingSection, WorkspaceSharingSection, CompactionSection,
  ContextPruningSection, SandboxSection,
} from "./config-sections";
import { WorkspaceSection } from "./general-sections";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { getChatGPTOAuthProviderRouting } from "@/types/provider";
import {
  buildAgentOtherConfigWithChatGPTOAuthRouting,
  normalizeChatGPTOAuthRouting,
} from "./agent-display-utils";

interface AgentAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function AgentAdvancedDialog({ open, onOpenChange, agent, onUpdate }: AgentAdvancedDialogProps) {
  const { t } = useTranslation("agents");
  const { providers } = useProviders();
  const providerByName = new Map(providers.map((provider) => [provider.name, provider]));
  const currentProvider = providerByName.get(agent.provider);
  const providerDefaults = getChatGPTOAuthProviderRouting(currentProvider?.settings);

  const deriveState = (a: AgentData) => {
    const otherObj = (a.other_config ?? {}) as Record<string, unknown>;
    const routing = normalizeChatGPTOAuthRouting(a.other_config);
    return {
      thinkingLevel: typeof otherObj.thinking_level === "string" ? otherObj.thinking_level : "off",
      chatgptRouting: {
        override_mode: routing.isExplicit
          ? routing.overrideMode
          : providerDefaults
            ? "inherit"
            : "custom",
        strategy: routing.strategy,
        extra_provider_names: routing.extraProviderNames,
      } as ChatGPTOAuthRoutingConfig,
      wsSharing: (otherObj.workspace_sharing ?? {}) as WorkspaceSharingConfig,
      comp: a.compaction_config ?? {},
      pruneEnabled: a.context_pruning != null,
      prune: a.context_pruning ?? {},
      sbEnabled: a.sandbox_config != null,
      sb: a.sandbox_config ?? {},
    };
  };

  const init = deriveState(agent);
  const [wsSharing, setWsSharing] = useState<WorkspaceSharingConfig>(init.wsSharing);
  const [thinkingLevel, setThinkingLevel] = useState(init.thinkingLevel);
  const [chatgptRouting, setChatgptRouting] = useState<ChatGPTOAuthRoutingConfig>(init.chatgptRouting);
  const [comp, setComp] = useState<CompactionConfig>(init.comp);
  const [pruneEnabled, setPruneEnabled] = useState(init.pruneEnabled);
  const [prune, setPrune] = useState<ContextPruningConfig>(init.prune);
  const [sbEnabled, setSbEnabled] = useState(init.sbEnabled);
  const [sb, setSb] = useState<SandboxConfig>(init.sb);

  // Re-sync local state when dialog opens (picks up latest agent data from React Query)
  useEffect(() => {
    if (!open) return;
    const s = deriveState(agent);
    setThinkingLevel(s.thinkingLevel);
    setChatgptRouting(s.chatgptRouting);
    setWsSharing(s.wsSharing);
    setComp(s.comp);
    setPruneEnabled(s.pruneEnabled);
    setPrune(s.prune);
    setSbEnabled(s.sbEnabled);
    setSb(s.sb);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      // Only send the keys this dialog owns to avoid overwriting keys managed by
      // the overview tab. The backend does a full column replace, so we must read
      // the latest agent data and merge our keys into it.
      const otherBase = buildAgentOtherConfigWithChatGPTOAuthRouting(
        agent,
        chatgptRouting,
        currentProvider?.settings,
      );
      delete otherBase.thinking_level;
      delete otherBase.workspace_sharing;
      if (thinkingLevel && thinkingLevel !== "off") {
        otherBase.thinking_level = thinkingLevel;
      }
      if (
        wsSharing.shared_dm || wsSharing.shared_group ||
        (wsSharing.shared_users?.length ?? 0) > 0 || wsSharing.share_memory
      ) {
        otherBase.workspace_sharing = wsSharing;
      }
      await onUpdate({
        compaction_config: comp,
        context_pruning: pruneEnabled ? prune : null,
        sandbox_config: sbEnabled ? sb : null,
        other_config: otherBase,
      });
      onOpenChange(false);
    } catch {
      // toast shown by hook — keep dialog open
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advanced")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {/* Workspace (read-only) */}
          <WorkspaceSection workspace={agent.workspace} />

          {/* Workspace Sharing */}
          <WorkspaceSharingSection value={wsSharing} onChange={setWsSharing} />

          {/* Thinking */}
          <ThinkingSection value={thinkingLevel} onChange={setThinkingLevel} />

          <ChatGPTOAuthRoutingSection
            currentProvider={agent.provider}
            providers={providers}
            value={chatgptRouting}
            onChange={setChatgptRouting}
            defaultRouting={providerDefaults}
            membershipEditable={false}
            membershipManagedByLabel={
              currentProvider?.display_name || agent.provider
            }
          />

          {/* Performance */}
          <ConfigGroupHeader
            title={t("configGroups.performance")}
            description={t("configGroups.performanceDesc")}
          />
          <div className="space-y-4">
            <CompactionSection value={comp} onChange={setComp} />
            <ContextPruningSection
              enabled={pruneEnabled}
              value={prune}
              onToggle={(v) => { setPruneEnabled(v); if (!v) setPrune({}); }}
              onChange={setPrune}
            />
            <SandboxSection
              enabled={sbEnabled}
              value={sb}
              onToggle={(v) => { setSbEnabled(v); if (!v) setSb({}); }}
              onChange={setSb}
            />
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={saving}>
            {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {saving ? t("config.saving") : t("config.saveConfig")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
