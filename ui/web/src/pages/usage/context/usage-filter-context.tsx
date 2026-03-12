import { createContext, useContext, useState, useCallback, ReactNode } from "react";
import { subDays, subHours } from "date-fns";

export type Period = "24h" | "7d" | "30d" | "custom";

export interface UsageFilters {
  from: string;
  to: string;
  period: Period;
  agentId?: string;
  provider?: string;
  model?: string;
  channel?: string;
  granularity: "hour" | "day";
}

interface UsageFilterContextValue {
  filters: UsageFilters;
  setFilter: (key: keyof UsageFilters, value: string | undefined) => void;
  toggleFilter: (key: "provider" | "model" | "channel" | "agentId", value: string) => void;
  setPeriod: (period: Period) => void;
  clearFilters: () => void;
  activeFilterCount: number;
}

function buildTimeRange(period: Period): { from: string; to: string; granularity: "hour" | "day" } {
  const now = new Date();
  let from: Date;
  let granularity: "hour" | "day";
  if (period === "24h") {
    from = subHours(now, 24);
    granularity = "hour";
  } else if (period === "7d") {
    from = subDays(now, 7);
    granularity = "hour";
  } else {
    from = subDays(now, 30);
    granularity = "day";
  }
  return { from: from.toISOString(), to: now.toISOString(), granularity };
}

function defaultFilters(): UsageFilters {
  const { from, to, granularity } = buildTimeRange("7d");
  return { from, to, period: "7d", granularity };
}

const UsageFilterContext = createContext<UsageFilterContextValue | null>(null);

export function UsageFilterProvider({ children }: { children: ReactNode }) {
  const [filters, setFilters] = useState<UsageFilters>(defaultFilters);

  const setFilter = useCallback((key: keyof UsageFilters, value: string | undefined) => {
    setFilters((prev) => ({ ...prev, [key]: value }));
  }, []);

  const toggleFilter = useCallback(
    (key: "provider" | "model" | "channel" | "agentId", value: string) => {
      setFilters((prev) => ({
        ...prev,
        [key]: prev[key] === value ? undefined : value,
      }));
    },
    [],
  );

  const setPeriod = useCallback((period: Period) => {
    if (period === "custom") {
      setFilters((prev) => ({ ...prev, period }));
      return;
    }
    const { from, to, granularity } = buildTimeRange(period);
    setFilters((prev) => ({
      ...prev,
      from,
      to,
      period,
      granularity,
    }));
  }, []);

  const clearFilters = useCallback(() => {
    setFilters((prev) => ({
      ...prev,
      agentId: undefined,
      provider: undefined,
      model: undefined,
      channel: undefined,
    }));
  }, []);

  const activeFilterCount = [
    filters.agentId,
    filters.provider,
    filters.model,
    filters.channel,
  ].filter(Boolean).length;

  return (
    <UsageFilterContext.Provider value={{ filters, setFilter, toggleFilter, setPeriod, clearFilters, activeFilterCount }}>
      {children}
    </UsageFilterContext.Provider>
  );
}

export function useUsageFilterContext(): UsageFilterContextValue {
  const ctx = useContext(UsageFilterContext);
  if (!ctx) throw new Error("useUsageFilterContext must be used within UsageFilterProvider");
  return ctx;
}
