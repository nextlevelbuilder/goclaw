import { useTranslation } from "react-i18next";
import { TagInput } from "@/components/shared/tag-input";
import { Switch } from "@/components/ui/switch";
import type { TeamMemberData } from "@/types/team";
import type { TeamSettingsFormData } from "@/schemas/team-settings.schema";

type RoutingRow = TeamSettingsFormData["routingRules"][number];

interface TeamRoutingSectionProps {
  members: TeamMemberData[];
  enabled: boolean;
  rules: RoutingRow[];
  onEnabledChange: (v: boolean) => void;
  onRulesChange: (rules: RoutingRow[]) => void;
}

/** Per-member routing rules — first matching rule wins. */
export function TeamRoutingSection({
  members, enabled, rules,
  onEnabledChange, onRulesChange,
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
