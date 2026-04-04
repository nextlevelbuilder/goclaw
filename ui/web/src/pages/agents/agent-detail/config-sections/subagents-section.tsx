import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Combobox } from "@/components/ui/combobox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { SubagentsConfig } from "@/types/agent";
import { ConfigSection, InfoLabel, numOrUndef } from "./config-section";
import { useProviders } from "@/pages/providers/hooks/use-providers";
import { useProviderModels } from "@/pages/providers/hooks/use-provider-models";

interface SubagentsSectionProps {
  enabled: boolean;
  value: SubagentsConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: SubagentsConfig) => void;
}

export function SubagentsSection({ enabled, value, onToggle, onChange }: SubagentsSectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.subagents";
  const { providers } = useProviders();
  const enabledProviders = providers.filter((p) => p.enabled);
  const defaultProvider = useMemo(() => enabledProviders[0], [enabledProviders]);
  const { models, loading: modelsLoading } = useProviderModels(defaultProvider?.id);

  return (
    <ConfigSection
      title={t(`${s}.title`)}
      description={t(`${s}.description`)}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Maximum number of sub-agents that can run simultaneously for this agent.">{t(`${s}.maxConcurrent`)}</InfoLabel>
          <Input
            type="number"
            placeholder="8"
            value={value.maxConcurrent ?? ""}
            onChange={(e) => onChange({ ...value, maxConcurrent: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="How many levels deep sub-agents can spawn other sub-agents. Depth 1 means only the parent can spawn.">{t(`${s}.maxSpawnDepth`)}</InfoLabel>
          <Select
            value={String(value.maxSpawnDepth ?? "")}
            onValueChange={(v) => onChange({ ...value, maxSpawnDepth: Number(v) })}
          >
            <SelectTrigger><SelectValue placeholder="1" /></SelectTrigger>
            <SelectContent>
              {[1, 2, 3, 4, 5].map((n) => (
                <SelectItem key={n} value={String(n)}>{n}</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <InfoLabel tip="Maximum number of sub-agents a single parent agent can spawn in one session.">{t(`${s}.maxChildrenPerAgent`)}</InfoLabel>
          <Input
            type="number"
            placeholder="5"
            value={value.maxChildrenPerAgent ?? ""}
            onChange={(e) => onChange({ ...value, maxChildrenPerAgent: numOrUndef(e.target.value) })}
          />
        </div>
        <div className="space-y-2">
          <InfoLabel tip="Idle time in minutes before a sub-agent session is automatically archived and cleaned up.">{t(`${s}.archiveAfter`)}</InfoLabel>
          <Input
            type="number"
            placeholder="60"
            value={value.archiveAfterMinutes ?? ""}
            onChange={(e) => onChange({ ...value, archiveAfterMinutes: numOrUndef(e.target.value) })}
          />
        </div>
      </div>
      <div className="space-y-2">
        <InfoLabel tip="LLM model for sub-agents. Leave empty to inherit the parent agent's model.">{t(`${s}.modelOverride`)}</InfoLabel>
        <Combobox
          value={value.model ?? ""}
          onChange={(v) => onChange({ ...value, model: v || undefined })}
          options={models.map((m) => ({ value: m.id, label: m.name }))}
          placeholder={modelsLoading ? "Loading models..." : t(`${s}.inheritFromAgent`)}
        />
      </div>
    </ConfigSection>
  );
}
