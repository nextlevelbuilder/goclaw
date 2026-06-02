import { AlertTriangle, Archive, Box, CircleOff, PackageX, ShieldAlert } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import type { SkillHealthStats } from "./lib/skills-filtering";
import type { SkillsFilter } from "./lib/skills-page-state";

interface SkillHealthSummaryProps {
  stats: SkillHealthStats;
  activeFilter: SkillsFilter;
  onFilterChange: (filter: SkillsFilter) => void;
}

export function SkillHealthSummary({ stats, activeFilter, onFilterChange }: SkillHealthSummaryProps) {
  const { t } = useTranslation("skills");
  const items: Array<{ filter: SkillsFilter; label: string; value: number; icon: typeof Box; tone?: string }> = [
    { filter: "all", label: t("health.total"), value: stats.total, icon: Box },
    { filter: "attention", label: t("health.attention"), value: stats.attention, icon: AlertTriangle, tone: "text-amber-700" },
    { filter: "missing-deps", label: t("health.missingDeps"), value: stats.missingDeps, icon: PackageX, tone: "text-amber-700" },
    { filter: "disabled", label: t("health.disabled"), value: stats.disabled, icon: CircleOff, tone: "text-red-700" },
    { filter: "archived", label: t("health.archived"), value: stats.archived, icon: Archive, tone: "text-muted-foreground" },
    { filter: "unmanaged", label: t("health.unmanaged"), value: stats.unmanaged, icon: ShieldAlert, tone: "text-orange-700" },
  ];

  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 xl:grid-cols-6">
      {items.map((item) => {
        const Icon = item.icon;
        const active = activeFilter === item.filter;
        return (
          <button
            key={item.filter}
            type="button"
            onClick={() => onFilterChange(item.filter)}
            className={cn(
              "flex min-h-16 items-center justify-between gap-3 rounded-md border px-3 py-2 text-left transition-colors",
              active ? "border-primary bg-primary/5 text-primary" : "bg-background hover:bg-muted/40",
            )}
            aria-pressed={active}
          >
            <div className="min-w-0">
              <div className="truncate text-xs text-muted-foreground">{item.label}</div>
              <div className={cn("text-lg font-semibold leading-tight", item.tone)}>{item.value}</div>
            </div>
            <Icon className={cn("h-4 w-4 shrink-0", active ? "text-primary" : "text-muted-foreground")} />
          </button>
        );
      })}
    </div>
  );
}
