import { useTranslation } from "react-i18next";
import { X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useUsageFilterContext, type Period } from "../context/usage-filter-context";
import type { SnapshotBreakdown } from "../hooks/use-usage-analytics";

interface FilterBarProps {
  agents: { id: string; name: string }[];
  providerBreakdown: SnapshotBreakdown[];
  channelBreakdown: SnapshotBreakdown[];
  onExportCsv?: () => void;
}

const PERIODS: { value: Period; labelKey: string }[] = [
  { value: "24h", labelKey: "analytics.period24h" },
  { value: "7d", labelKey: "analytics.period7d" },
  { value: "30d", labelKey: "analytics.period30d" },
];

export function FilterBar({ agents, providerBreakdown, channelBreakdown, onExportCsv }: FilterBarProps) {
  const { t } = useTranslation("usage");
  const { filters, setPeriod, setFilter, clearFilters, activeFilterCount } = useUsageFilterContext();

  const chips: { label: string; onRemove: () => void }[] = [];
  if (filters.provider) chips.push({ label: `provider: ${filters.provider}`, onRemove: () => setFilter("provider", undefined) });
  if (filters.model) chips.push({ label: `model: ${filters.model}`, onRemove: () => setFilter("model", undefined) });
  if (filters.channel) chips.push({ label: `channel: ${filters.channel}`, onRemove: () => setFilter("channel", undefined) });
  if (filters.agentId) {
    const name = agents.find((a) => a.id === filters.agentId)?.name ?? filters.agentId;
    chips.push({ label: `agent: ${name}`, onRemove: () => setFilter("agentId", undefined) });
  }

  return (
    <div className="rounded-lg border bg-card p-3 space-y-3">
      {/* Top row: period + export */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex items-center gap-1">
          {PERIODS.map((p) => (
            <Button
              key={p.value}
              size="sm"
              variant={filters.period === p.value ? "default" : "outline"}
              className="h-7 px-3 text-xs"
              onClick={() => setPeriod(p.value)}
            >
              {t(p.labelKey)}
            </Button>
          ))}
        </div>

        <div className="ml-auto flex items-center gap-2">
          {/* Agent dropdown */}
          {agents.length > 0 && (
            <Select
              value={filters.agentId ?? "__all__"}
              onValueChange={(v) => setFilter("agentId", v === "__all__" ? undefined : v)}
            >
              <SelectTrigger className="h-7 w-40 text-xs">
                <SelectValue placeholder={t("analytics.allAgents")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{t("analytics.allAgents")}</SelectItem>
                {agents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>{a.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}

          {/* Provider dropdown */}
          {providerBreakdown.length > 0 && (
            <Select
              value={filters.provider ?? "__all__"}
              onValueChange={(v) => setFilter("provider", v === "__all__" ? undefined : v)}
            >
              <SelectTrigger className="h-7 w-36 text-xs">
                <SelectValue placeholder={t("analytics.allProviders")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{t("analytics.allProviders")}</SelectItem>
                {providerBreakdown.map((b) => (
                  <SelectItem key={b.key} value={b.key}>{b.key}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}

          {/* Channel dropdown */}
          {channelBreakdown.length > 0 && (
            <Select
              value={filters.channel ?? "__all__"}
              onValueChange={(v) => setFilter("channel", v === "__all__" ? undefined : v)}
            >
              <SelectTrigger className="h-7 w-36 text-xs">
                <SelectValue placeholder={t("analytics.allChannels")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{t("analytics.allChannels")}</SelectItem>
                {channelBreakdown.map((b) => (
                  <SelectItem key={b.key} value={b.key}>{b.key}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}

          {onExportCsv && (
            <Button size="sm" variant="outline" className="h-7 text-xs" onClick={onExportCsv}>
              {t("analytics.exportCsv")}
            </Button>
          )}
        </div>
      </div>

      {/* Active filter chips */}
      {chips.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-muted-foreground">{t("analytics.activeFilters")}:</span>
          {chips.map((chip) => (
            <Badge key={chip.label} variant="secondary" className="gap-1 text-xs">
              {chip.label}
              <button onClick={chip.onRemove} className="ml-0.5 hover:text-foreground">
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
          {activeFilterCount > 0 && (
            <Button size="sm" variant="ghost" className="h-5 px-2 text-xs text-muted-foreground" onClick={clearFilters}>
              {t("analytics.clearAll")}
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
