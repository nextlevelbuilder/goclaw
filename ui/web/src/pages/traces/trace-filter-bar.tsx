import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Filter, X } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SearchInput } from "@/components/shared/search-input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  clearTraceFilter,
  getActiveTraceFilterChips,
  type TraceFilters,
} from "./trace-filter-params";

interface TraceFilterBarProps {
  filters: TraceFilters;
  agents: Array<{ id: string; display_name?: string; agent_key?: string }>;
  channels: Array<{ id: string; name: string; display_name?: string }>;
  onChange: (filters: TraceFilters) => void;
}

const STATUS_OPTIONS = ["running", "completed", "error", "cancelled"];

export function TraceFilterBar({ filters, agents, channels, onChange }: TraceFilterBarProps) {
  const { t } = useTranslation("traces");
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const chips = useMemo(() => getActiveTraceFilterChips(filters), [filters]);

  const update = (patch: Partial<TraceFilters>) => {
    onChange(cleanFilters({ ...filters, ...patch }));
  };
  const clearOne = (key: keyof TraceFilters) => onChange(clearTraceFilter(filters, key));
  const clearAll = () => onChange({});

  return (
    <div className="mt-4 space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <SearchInput
          value={filters.query ?? ""}
          onChange={(query) => update({ query })}
          placeholder={t("filters.search")}
          className="min-w-[220px] flex-1 sm:max-w-sm"
        />
        <Select value={filters.agentId ?? "__all__"} onValueChange={(v) => update({ agentId: v === "__all__" ? undefined : v })}>
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allAgents")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allAgents")}</SelectItem>
            {agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>{a.display_name || a.agent_key || a.id}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={filters.channel ?? "__all__"} onValueChange={(v) => update({ channel: v === "__all__" ? undefined : v })}>
          <SelectTrigger className="h-8 w-44 text-xs">
            <SelectValue placeholder={t("allChannels")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("allChannels")}</SelectItem>
            {channels.map((ch) => (
              <SelectItem key={ch.id} value={ch.name}>{ch.display_name || ch.name}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={filters.status ?? "__all__"} onValueChange={(v) => update({ status: v === "__all__" ? undefined : v })}>
          <SelectTrigger className="h-8 w-36 text-xs">
            <SelectValue placeholder={t("filters.status")} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all__">{t("filters.allStatuses")}</SelectItem>
            {STATUS_OPTIONS.map((status) => (
              <SelectItem key={status} value={status}>{t(`status.${status}`)}</SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button type="button" variant="outline" size="sm" className="gap-1" onClick={() => setAdvancedOpen((v) => !v)}>
          <Filter className="h-3.5 w-3.5" />
          {t("filters.advanced")}
        </Button>
      </div>

      {advancedOpen && (
        <div className="grid gap-3 rounded-md border bg-muted/20 p-3 sm:grid-cols-2 lg:grid-cols-4">
          <FilterField label={t("filters.from")} type="datetime-local" value={filters.from} onChange={(from) => update({ from })} />
          <FilterField label={t("filters.to")} type="datetime-local" value={filters.to} onChange={(to) => update({ to })} />
          <FilterField label={t("filters.minInputTokens")} type="number" value={filters.minInputTokens} onChange={(minInputTokens) => update({ minInputTokens })} />
          <FilterField label={t("filters.maxInputTokens")} type="number" value={filters.maxInputTokens} onChange={(maxInputTokens) => update({ maxInputTokens })} />
          <FilterField label={t("filters.minOutputTokens")} type="number" value={filters.minOutputTokens} onChange={(minOutputTokens) => update({ minOutputTokens })} />
          <FilterField label={t("filters.maxOutputTokens")} type="number" value={filters.maxOutputTokens} onChange={(maxOutputTokens) => update({ maxOutputTokens })} />
          <FilterField label={t("filters.minToolCalls")} type="number" value={filters.minToolCalls} onChange={(minToolCalls) => update({ minToolCalls })} />
          <FilterField label={t("filters.maxToolCalls")} type="number" value={filters.maxToolCalls} onChange={(maxToolCalls) => update({ maxToolCalls })} />
          <FilterField label={t("filters.toolName")} value={filters.toolName} onChange={(toolName) => update({ toolName })} />
          <div className="space-y-1.5">
            <Label className="text-xs text-muted-foreground">{t("filters.hasToolCalls")}</Label>
            <Select value={filters.hasToolCalls ?? "__all__"} onValueChange={(v) => update({ hasToolCalls: v === "__all__" ? undefined : v as "true" | "false" })}>
              <SelectTrigger className="h-9 text-xs">
                <SelectValue placeholder={t("filters.hasToolCalls")} />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="__all__">{t("filters.anyToolCalls")}</SelectItem>
                <SelectItem value="true">{t("filters.withToolCalls")}</SelectItem>
                <SelectItem value="false">{t("filters.withoutToolCalls")}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </div>
      )}

      {chips.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {chips.map((chip) => (
            <Badge key={chip.key} variant="secondary" className="gap-1 pr-1">
              <span>{t(`filters.chips.${chip.key}`)}: {chip.value}</span>
              <button type="button" className="rounded-full p-0.5 hover:bg-background/70" onClick={() => clearOne(chip.key)} aria-label={t("filters.clearFilter")}>
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
          <Button type="button" variant="ghost" size="sm" onClick={clearAll}>{t("filters.clearAll")}</Button>
        </div>
      )}
    </div>
  );
}

function FilterField({ label, value, onChange, type = "text" }: { label: string; value?: string; onChange: (value: string) => void; type?: string }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-xs text-muted-foreground">{label}</Label>
      <Input type={type} value={value ?? ""} min={type === "number" ? 0 : undefined} onChange={(e) => onChange(e.target.value)} className="text-base md:text-sm" />
    </div>
  );
}

function cleanFilters(filters: TraceFilters): TraceFilters {
  return Object.fromEntries(
    Object.entries(filters).filter(([, value]) => value !== undefined && String(value).trim() !== ""),
  ) as TraceFilters;
}
