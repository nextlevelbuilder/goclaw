import { useTranslation } from "react-i18next";
import { TagInput } from "@/components/shared/tag-input";
import { Switch } from "@/components/ui/switch";
import type { TeamMemberData } from "@/types/team";
import type { TeamSettingsFormData } from "@/schemas/team-settings.schema";

type RoutingRow = TeamSettingsFormData["routingRules"][number];

interface TeamRoutingSectionProps {
  members: TeamMemberData[];
  enabled: boolean;
  fallbackToSelf: boolean;
  confidenceThreshold: number;
  rules: RoutingRow[];
  onEnabledChange: (v: boolean) => void;
  onFallbackToSelfChange: (v: boolean) => void;
  onConfidenceChange: (v: number) => void;
  onRulesChange: (rules: RoutingRow[]) => void;
}

/** Per-member routing rules — first matching rule wins. */
export function TeamRoutingSection({
  members, enabled, fallbackToSelf, confidenceThreshold, rules,
  onEnabledChange, onFallbackToSelfChange, onConfidenceChange, onRulesChange,
}: TeamRoutingSectionProps) {
  const { t } = useTranslation("teams");

  const ruleFor = (key: string): RoutingRow =>
    rules.find((r) => r.agent_key === key) ?? { agent_key: key, keywords: [], file_patterns: [], criteria: "", enabled: true };

  const patch = (key: string, p: Partial<RoutingRow>) => {
    const next = rules.some((r) => r.agent_key === key)
      ? rules.map((r) => (r.agent_key === key ? { ...r, ...p } : r))
      : [...rules, { agent_key: key, keywords: [], file_patterns: [], criteria: "", enabled: true, ...p }];
    onRulesChange(next);
  };

  return (
    <div className="space-y-4">
      <h3 className="text-sm font-medium">{t("settings.routing")}</h3>
      <div className="rounded-lg border p-4 space-y-3">
        <div className="flex items-center justify-between">
          <div>
            <span className="text-sm font-semibold">{t("settings.routingEnabled")}</span>
            <p className="text-xs text-muted-foreground">{t("settings.routingEnabledDesc")}</p>
          </div>
          <Switch checked={enabled} onCheckedChange={onEnabledChange} />
        </div>
        {enabled && (
          <>
            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <span className="text-sm font-semibold">{t("settings.routingFallbackToSelf")}</span>
                <p className="text-xs text-muted-foreground">{t("settings.routingFallbackToSelfDesc")}</p>
              </div>
              <Switch checked={fallbackToSelf} onCheckedChange={onFallbackToSelfChange} />
            </div>
            <div className="border-t pt-3">
              <label className="text-sm font-semibold">{t("settings.routingConfidence")}</label>
              <p className="text-xs text-muted-foreground">{t("settings.routingConfidenceDesc")}</p>
              <input type="number" min={0.01} max={1} step={0.05} value={confidenceThreshold}
                onChange={(e) => onConfidenceChange(Math.min(1, Math.max(0.01, parseFloat(e.target.value) || 0.65)))}
                className="mt-1 w-24 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm" />
            </div>
            <p className="text-xs text-muted-foreground border-t pt-3">{t("settings.routingRuleOrderHint")}</p>
            {members.map((m) => {
              const r = ruleFor(m.agent_key ?? "");
              return (
                <div key={m.agent_id} className="rounded-lg border p-3 space-y-2">
                  <div className="flex items-center justify-between">
                    <span className="text-sm font-medium">{m.emoji} {m.display_name || m.agent_key}</span>
                    <Switch checked={r.enabled !== false} onCheckedChange={(v) => patch(m.agent_key ?? "", { enabled: v })} />
                  </div>
                  <div>
                    <label className="text-xs text-muted-foreground">{t("settings.routingKeywords")}</label>
                    <TagInput value={r.keywords ?? []} onChange={(v) => patch(m.agent_key ?? "", { keywords: v })} placeholder={t("settings.routingKeywordsHint")} />
                  </div>
                  <div>
                    <label className="text-xs text-muted-foreground">{t("settings.routingFilePatterns")}</label>
                    <TagInput value={r.file_patterns ?? []} onChange={(v) => patch(m.agent_key ?? "", { file_patterns: v })} placeholder={t("settings.routingFilePatternsHint")} />
                  </div>
                  <div>
                    <label className="text-xs text-muted-foreground">{t("settings.routingCriteria")}</label>
                    <textarea value={r.criteria ?? ""} onChange={(e) => patch(m.agent_key ?? "", { criteria: e.target.value })}
                      rows={2} className="w-full rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                      placeholder={t("settings.routingCriteriaHint")} />
                  </div>
                </div>
              );
            })}
          </>
        )}
      </div>
    </div>
  );
}
