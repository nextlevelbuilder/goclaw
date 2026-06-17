import { useTranslation } from "react-i18next";
import { Check, X, Play, Loader2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { useSkillEvolution } from "./hooks/use-skill-evolution";
import type { SkillInfo } from "@/types/skill";

interface SkillEvolutionPanelProps {
  skill: SkillInfo;
  active: boolean;
}

export function SkillEvolutionPanel({ skill, active }: SkillEvolutionPanelProps) {
  const { t } = useTranslation("skills");
  const {
    settings,
    metrics,
    suggestions,
    activity,
    updateSettings,
    approveSuggestion,
    rejectSuggestion,
    applySuggestion,
  } = useSkillEvolution(skill.id, active);

  if (!skill.id) {
    return (
      <div className="rounded-md border border-dashed bg-muted/20 p-6 text-sm text-muted-foreground">
        {t("evolution.unavailable")}
      </div>
    );
  }

  const data = settings.data;
  const stats = metrics.data;
  const pending = suggestions.data?.filter((item) => item.status === "pending") ?? [];
  const isBusy = settings.isFetching || metrics.isFetching || suggestions.isFetching;

  return (
    <div className="space-y-4">
      <section className="rounded-md border p-4">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-medium">{t("evolution.title")}</h3>
              {isBusy && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
            </div>
            <p className="text-sm text-muted-foreground">{t("evolution.status", { status: data?.enabled ? t("evolution.enabled") : t("evolution.disabled") })}</p>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <div className="flex items-center gap-2">
              <Label htmlFor="skill-evolution-enabled" className="text-sm">{t("evolution.enabled")}</Label>
              <Switch
                id="skill-evolution-enabled"
                checked={!!data?.enabled}
                disabled={settings.isLoading}
                onCheckedChange={(enabled) => updateSettings({ enabled })}
              />
            </div>
            <Select
              value={data?.mode ?? "suggest_only"}
              onValueChange={(mode) => updateSettings({ mode: mode as "suggest_only" | "auto_analyze" })}
            >
              <SelectTrigger className="h-9 w-[180px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="suggest_only">{t("evolution.modes.suggest_only")}</SelectItem>
                <SelectItem value="auto_analyze">{t("evolution.modes.auto_analyze")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      </section>

      <section className="rounded-md border p-4">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium">{t("evolution.metrics")}</h3>
          {stats?.last_used_at && <span className="text-xs text-muted-foreground">{new Date(stats.last_used_at).toLocaleString()}</span>}
        </div>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <Metric label={t("evolution.total")} value={stats?.total_calls ?? 0} />
          <Metric label={t("evolution.succeeded")} value={stats?.succeeded ?? 0} />
          <Metric label={t("evolution.failed")} value={stats?.failed ?? 0} />
          <Metric label={t("evolution.successRate")} value={`${Math.round((stats?.success_rate ?? 0) * 100)}%`} />
        </div>
      </section>

      <section className="rounded-md border p-4">
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-medium">{t("evolution.suggestions")}</h3>
          <Badge variant={pending.length > 0 ? "default" : "outline"}>{pending.length}</Badge>
        </div>
        <div className="space-y-2">
          {(suggestions.data ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">{t("evolution.noSuggestions")}</p>
          ) : (
            suggestions.data?.map((item) => (
              <div key={item.id} className="rounded-md border bg-muted/20 p-3">
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0 space-y-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Badge variant="outline">{item.status}</Badge>
                      <span className="text-sm font-medium">{item.suggestion_type}</span>
                      {item.target_file && <span className="text-xs text-muted-foreground">{item.target_file}</span>}
                    </div>
                    <p className="text-sm text-muted-foreground">{item.reason || t("evolution.noReason")}</p>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {item.status === "pending" && (
                      <>
                        <Button type="button" variant="outline" size="icon" className="h-8 w-8" onClick={() => approveSuggestion(item.id)} aria-label={t("evolution.approve")}>
                          <Check className="h-3.5 w-3.5" />
                        </Button>
                        <Button type="button" variant="outline" size="icon" className="h-8 w-8" onClick={() => rejectSuggestion(item.id)} aria-label={t("evolution.reject")}>
                          <X className="h-3.5 w-3.5" />
                        </Button>
                      </>
                    )}
                    {item.status === "approved" && !skill.is_system && (
                      <Button type="button" variant="outline" size="icon" className="h-8 w-8" onClick={() => applySuggestion(item.id)} aria-label={t("evolution.apply")}>
                        <Play className="h-3.5 w-3.5" />
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            ))
          )}
        </div>
      </section>

      {!activity.isError && (
        <section className="rounded-md border p-4">
          <h3 className="mb-3 text-sm font-medium">{t("evolution.activity")}</h3>
          <div className="space-y-2">
            {(activity.data ?? []).length === 0 ? (
              <p className="text-sm text-muted-foreground">{t("evolution.noActivity")}</p>
            ) : (
              activity.data?.slice(0, 8).map((item) => (
                <div key={item.id} className="flex flex-col gap-1 rounded-md bg-muted/30 px-3 py-2 sm:flex-row sm:items-center sm:justify-between">
                  <span className="text-sm">{item.action}</span>
                  <span className="text-xs text-muted-foreground">{new Date(item.created_at).toLocaleString()}</span>
                </div>
              ))
            )}
          </div>
        </section>
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return (
    <div className="rounded-md bg-muted/30 px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold">{value}</div>
    </div>
  );
}
