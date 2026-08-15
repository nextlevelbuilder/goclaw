import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Sparkles } from "lucide-react";
import { useV3Flags } from "@/hooks/use-v3-flags";
import { useEvolutionMetrics } from "@/hooks/use-evolution-metrics";
import { useEvolutionSuggestions } from "@/hooks/use-evolution-suggestions";
import { EvolutionMetricsCharts } from "./evolution-metrics-charts";
import { EvolutionSuggestionsTable } from "./evolution-suggestions-table";
import { EvolutionGuardrailsCard } from "./evolution-guardrails-card";
import type { AdaptationGuardrails } from "@/types/evolution";

const TIME_RANGES = ["7d", "30d", "90d"] as const;

/** Default guardrails when agent has none configured. */
const DEFAULT_GUARDRAILS: AdaptationGuardrails = {
  max_delta_per_cycle: 0.1,
  min_data_points: 100,
  rollback_on_drop_pct: 20,
  locked_params: [],
};

interface AgentEvolutionTabProps {
  agentId: string;
  agentOtherConfig?: Record<string, unknown>;
}

export function AgentEvolutionTab({ agentId, agentOtherConfig }: AgentEvolutionTabProps) {
  const { t } = useTranslation("agents");
  const [timeRange, setTimeRange] = useState<(typeof TIME_RANGES)[number]>("7d");

  const { flags, loading: flagsLoading } = useV3Flags(agentId);
  const { toolAggs, retrievalAggs, loading: metricsLoading } = useEvolutionMetrics(agentId, timeRange);
  const { suggestions, loading: suggestionsLoading, updateStatus } = useEvolutionSuggestions(agentId);

  // Parse guardrails from agent other_config, fallback to defaults.
  const guardrails: AdaptationGuardrails = {
    ...DEFAULT_GUARDRAILS,
    ...((agentOtherConfig?.evolution_guardrails ?? {}) as Partial<AdaptationGuardrails>),
  };

  // Empty state when evolution metrics flag is not enabled.
  if (!flagsLoading && flags && !flags.self_evolution_metrics) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center space-y-3">
        <Sparkles className="h-10 w-10 text-muted-foreground/40" />
        <h3 className="text-sm font-medium">{t("detail.evolution.notEnabled")}</h3>
        <p className="text-xs text-muted-foreground max-w-sm">
          {t("detail.evolution.notEnabledHint")}
        </p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {/* Time range selector */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-muted-foreground">{t("detail.evolution.timeRange")}:</span>
        <div className="flex rounded-md border">
          {TIME_RANGES.map((r) => (
            <button
              key={r}
              onClick={() => setTimeRange(r)}
              className={`px-3 py-1 text-xs transition-colors ${
                timeRange === r
                  ? "bg-primary text-primary-foreground"
                  : "hover:bg-muted"
              } ${r === "7d" ? "rounded-l-md" : ""} ${r === "90d" ? "rounded-r-md" : ""}`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>

      {/* Metrics charts */}
      <EvolutionMetricsCharts
        toolAggs={toolAggs}
        retrievalAggs={retrievalAggs}
        loading={metricsLoading}
      />

      {/* Suggestions table */}
      <div className="space-y-2">
        <h4 className="text-sm font-medium">{t("detail.evolution.suggestions")}</h4>
        <EvolutionSuggestionsTable
          suggestions={suggestions}
          loading={suggestionsLoading}
          onUpdateStatus={updateStatus}
        />
      </div>

      {/* Guardrails display (read-only) */}
      <EvolutionGuardrailsCard guardrails={guardrails} />
    </div>
  );
}
