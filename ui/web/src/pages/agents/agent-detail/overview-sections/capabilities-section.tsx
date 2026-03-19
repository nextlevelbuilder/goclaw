import { useTranslation } from "react-i18next";
import type { MemoryConfig, SubagentsConfig, ToolPolicyConfig } from "@/types/agent";
import { MemorySection, SubagentsSection, ToolPolicySection } from "../config-sections";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";

interface CapabilitiesSectionProps {
  memEnabled: boolean;
  mem: MemoryConfig;
  onMemToggle: (v: boolean) => void;
  onMemChange: (v: MemoryConfig) => void;

  subEnabled: boolean;
  sub: SubagentsConfig;
  onSubToggle: (v: boolean) => void;
  onSubChange: (v: SubagentsConfig) => void;

  toolsEnabled: boolean;
  tools: ToolPolicyConfig;
  onToolsToggle: (v: boolean) => void;
  onToolsChange: (v: ToolPolicyConfig) => void;
}

export function CapabilitiesSection({
  memEnabled, mem, onMemToggle, onMemChange,
  subEnabled, sub, onSubToggle, onSubChange,
  toolsEnabled, tools, onToolsToggle, onToolsChange,
}: CapabilitiesSectionProps) {
  const { t } = useTranslation("agents");

  return (
    <section className="space-y-4">
      <ConfigGroupHeader
        title={t("detail.capabilities")}
        description={t("configGroups.capabilitiesDesc")}
      />
      <div className="space-y-4">
        <MemorySection
          enabled={memEnabled}
          value={mem}
          onToggle={(v) => { onMemToggle(v); if (!v) onMemChange({}); }}
          onChange={onMemChange}
        />
        <SubagentsSection
          enabled={subEnabled}
          value={sub}
          onToggle={(v) => { onSubToggle(v); if (!v) onSubChange({}); }}
          onChange={onSubChange}
        />
        <ToolPolicySection
          enabled={toolsEnabled}
          value={tools}
          onToggle={(v) => { onToolsToggle(v); if (!v) onToolsChange({}); }}
          onChange={onToolsChange}
        />
      </div>
    </section>
  );
}
