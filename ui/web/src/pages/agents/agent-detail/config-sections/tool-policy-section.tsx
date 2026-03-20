import { useTranslation } from "react-i18next";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Input } from "@/components/ui/input";
import type { ToolPolicyConfig } from "@/types/agent";
import { ConfigSection, InfoLabel } from "./config-section";
import { ToolNameSelect } from "@/components/shared/tool-name-select";

interface ToolPolicySectionProps {
  enabled: boolean;
  value: ToolPolicyConfig;
  onToggle: (v: boolean) => void;
  onChange: (v: ToolPolicyConfig) => void;
}

export function ToolPolicySection({ enabled, value, onToggle, onChange }: ToolPolicySectionProps) {
  const { t } = useTranslation("agents");
  const s = "configSections.toolPolicy";
  return (
    <ConfigSection
      title={t(`${s}.title`)}
      description={t(`${s}.description`)}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="space-y-2">
        <InfoLabel tip="Base tool profile. 'full' allows all tools, 'coding' includes filesystem/runtime/sessions/memory, 'messaging' includes messaging/sessions, 'minimal' allows only session_status.">{t(`${s}.profile`)}</InfoLabel>
        <Select
          value={value.profile ?? ""}
          onValueChange={(v) => onChange({ ...value, profile: v || undefined })}
        >
          <SelectTrigger><SelectValue placeholder="full" /></SelectTrigger>
          <SelectContent>
            <SelectItem value="full">full</SelectItem>
            <SelectItem value="coding">coding</SelectItem>
            <SelectItem value="messaging">messaging</SelectItem>
            <SelectItem value="minimal">minimal</SelectItem>
          </SelectContent>
        </Select>
      </div>
      <div className="space-y-2">
        <InfoLabel tip="Strip prefix from tool call names returned by the model before registry lookup. Example: 'proxy_' strips prefix so 'proxy_exec' resolves to 'exec'. Supports {tool_name} placeholder.">{t(`${s}.toolCallPrefix`)}</InfoLabel>
        <Input
          value={value.toolCallPrefix ?? ""}
          onChange={(e) => onChange({ ...value, toolCallPrefix: e.target.value.replace(/[^a-z0-9_{}/]/g, "") || undefined })}
          placeholder="e.g. proxy_"
          className="font-mono text-sm"
        />
        <p className="text-xs text-muted-foreground">{t(`${s}.toolCallPrefixHint`)}</p>
      </div>
      <div className="space-y-2">
        <InfoLabel tip="Explicit allowlist. Only these tools will be available (overrides profile). Leave empty to use profile defaults.">{t(`${s}.allow`)}</InfoLabel>
        <ToolNameSelect
          value={value.allow ?? []}
          onChange={(v) => onChange({ ...value, allow: v.length > 0 ? v : undefined })}
          placeholder={t(`${s}.selectToolsAllow`)}
        />
      </div>
      <div className="space-y-2">
        <InfoLabel tip="Denylist. These tools will be blocked even if allowed by the profile.">{t(`${s}.deny`)}</InfoLabel>
        <ToolNameSelect
          value={value.deny ?? []}
          onChange={(v) => onChange({ ...value, deny: v.length > 0 ? v : undefined })}
          placeholder={t(`${s}.selectToolsDeny`)}
        />
      </div>
      <div className="space-y-2">
        <InfoLabel tip="Additional tools on top of profile defaults. Useful for enabling optional tools without overriding the whole profile.">{t(`${s}.alsoAllow`)}</InfoLabel>
        <ToolNameSelect
          value={value.alsoAllow ?? []}
          onChange={(v) => onChange({ ...value, alsoAllow: v.length > 0 ? v : undefined })}
          placeholder={t(`${s}.selectToolsAlsoAllow`)}
        />
      </div>
    </ConfigSection>
  );
}
