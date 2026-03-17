import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { StickySaveBar } from "@/components/shared/sticky-save-bar";
import type {
  AgentData,
  SubagentsConfig,
  ToolPolicyConfig,
  CompactionConfig,
  ContextPruningConfig,
  SandboxConfig,
  MemoryConfig,
  WorkspaceSharingConfig,
} from "@/types/agent";
import {
  SubagentsSection,
  ToolPolicySection,
  CompactionSection,
  ContextPruningSection,
  SandboxSection,
  MemorySection,
  OtherConfigSection,
  ThinkingSection,
  WorkspaceSharingSection,
} from "./config-sections";

interface AgentConfigTabProps {
  agent: AgentData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
}

export function AgentConfigTab({ agent, onUpdate }: AgentConfigTabProps) {
  const { t } = useTranslation("agents");

  const [subEnabled, setSubEnabled] = useState(agent.subagents_config != null);
  const [sub, setSub] = useState<SubagentsConfig>(agent.subagents_config ?? {});

  const [toolsEnabled, setToolsEnabled] = useState(agent.tools_config != null);
  const [tools, setTools] = useState<ToolPolicyConfig>(agent.tools_config ?? {});

  const [comp, setComp] = useState<CompactionConfig>(agent.compaction_config ?? {});

  const [pruneEnabled, setPruneEnabled] = useState(agent.context_pruning != null);
  const [prune, setPrune] = useState<ContextPruningConfig>(agent.context_pruning ?? {});

  const [sbEnabled, setSbEnabled] = useState(agent.sandbox_config != null);
  const [sb, setSb] = useState<SandboxConfig>(agent.sandbox_config ?? {});

  const [memEnabled, setMemEnabled] = useState(agent.memory_config != null);
  const [mem, setMem] = useState<MemoryConfig>(agent.memory_config ?? {});

  const otherObj = (agent.other_config ?? {}) as Record<string, unknown>;
  const initialThinkingLevel = (typeof otherObj.thinking_level === "string" ? otherObj.thinking_level : "off");
  const initialWsSharing = (otherObj.workspace_sharing ?? {}) as WorkspaceSharingConfig;
  // Strip all managed keys so they don't leak into the raw JSON editor.
  // General tab manages: emoji, self_evolve, description, skill_evolve, skill_nudge_interval
  // Config tab manages: workspace_sharing, thinking_level
  const {
    workspace_sharing: _ws, thinking_level: _tl,
    emoji: _emoji, self_evolve: _se, description: _desc,
    skill_evolve: _ske, skill_nudge_interval: _sni,
    ...otherWithoutManaged
  } = otherObj;

  const [wsSharing, setWsSharing] = useState<WorkspaceSharingConfig>(initialWsSharing);

  const [thinkingLevel, setThinkingLevel] = useState(initialThinkingLevel);

  const [otherEnabled, setOtherEnabled] = useState(
    agent.other_config != null && Object.keys(otherWithoutManaged).length > 0,
  );
  const [otherJson, setOtherJson] = useState(
    Object.keys(otherWithoutManaged).length > 0 ? JSON.stringify(otherWithoutManaged, null, 2) : "{}",
  );

  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    setSaveError(null);
    setSaved(false);
    try {
      const updates: Record<string, unknown> = {
        subagents_config: subEnabled ? sub : null,
        tools_config: toolsEnabled
          ? { profile: tools.profile, allow: tools.allow, deny: tools.deny, alsoAllow: tools.alsoAllow, byProvider: tools.byProvider }
          : {},
        compaction_config: comp,
        context_pruning: pruneEnabled ? prune : null,
        sandbox_config: sbEnabled ? sb : null,
        memory_config: memEnabled ? mem : null,
      };
      // Preserve existing other_config fields not managed by this tab (e.g. emoji from General tab).
      const existing = (agent.other_config as Record<string, unknown> | null) ?? {};
      let otherBase: Record<string, unknown> = { ...existing };
      // Strip fields managed by this tab — they'll be re-added below from local state.
      delete otherBase.thinking_level;
      delete otherBase.workspace_sharing;
      if (otherEnabled) {
        try { Object.assign(otherBase, JSON.parse(otherJson)); } catch { /* keep existing */ }
      }
      if (thinkingLevel && thinkingLevel !== "off") {
        otherBase.thinking_level = thinkingLevel;
      }
      if (wsSharing.shared_dm || wsSharing.shared_group || (wsSharing.shared_users?.length ?? 0) > 0 || wsSharing.share_memory) {
        otherBase.workspace_sharing = wsSharing;
      }
      updates.other_config = Object.keys(otherBase).length > 0 ? otherBase : {};
      await onUpdate(updates);
      setSaved(true);
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : t("config.failedToSave"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-4xl space-y-4">
      {/* Workspace & Security */}
      <WorkspaceSharingSection
        value={wsSharing}
        onChange={setWsSharing}
      />

      {/* Core */}
      <ThinkingSection
        value={thinkingLevel}
        onChange={setThinkingLevel}
      />

      {/* Capabilities */}
      <ConfigGroupHeader
        title={t("configGroups.capabilities")}
        description={t("configGroups.capabilitiesDesc")}
      />
      <div className="space-y-4">
        <MemorySection
          enabled={memEnabled}
          value={mem}
          onToggle={(v: boolean) => { setMemEnabled(v); if (!v) setMem({}); }}
          onChange={setMem}
        />
        <SubagentsSection
          enabled={subEnabled}
          value={sub}
          onToggle={(v: boolean) => { setSubEnabled(v); if (!v) setSub({}); }}
          onChange={setSub}
        />
        <ToolPolicySection
          enabled={toolsEnabled}
          value={tools}
          onToggle={(v: boolean) => { setToolsEnabled(v); if (!v) setTools({}); }}
          onChange={setTools}
        />
      </div>

      {/* Performance */}
      <ConfigGroupHeader
        title={t("configGroups.performance")}
        description={t("configGroups.performanceDesc")}
      />
      <div className="space-y-4">
        <CompactionSection
          value={comp}
          onChange={setComp}
        />
        <ContextPruningSection
          enabled={pruneEnabled}
          value={prune}
          onToggle={(v: boolean) => { setPruneEnabled(v); if (!v) setPrune({}); }}
          onChange={setPrune}
        />
        <SandboxSection
          enabled={sbEnabled}
          value={sb}
          onToggle={(v: boolean) => { setSbEnabled(v); if (!v) setSb({}); }}
          onChange={setSb}
        />
      </div>

      {/* Advanced */}
      <ConfigGroupHeader
        title={t("configGroups.advanced")}
        description={t("configGroups.advancedDesc")}
      />
      <div className="space-y-4">
        <OtherConfigSection
          enabled={otherEnabled}
          value={otherJson}
          onToggle={(v: boolean) => { setOtherEnabled(v); if (!v) setOtherJson("{}"); }}
          onChange={setOtherJson}
        />
      </div>

      <StickySaveBar
        onSave={handleSave}
        saving={saving}
        saved={saved}
        error={saveError}
        label={t("config.saveConfig")}
        savingLabel={t("config.saving")}
        savedLabel={t("config.saved")}
      />
    </div>
  );
}
