import { useTranslation } from "react-i18next";
import { Shield } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { AdaptationGuardrails } from "@/types/evolution";

interface Props {
  guardrails: AdaptationGuardrails;
}

export function EvolutionGuardrailsCard({ guardrails }: Props) {
  const { t } = useTranslation("agents");

  return (
    <section className="rounded-lg border p-3 sm:p-4 space-y-3">
      <div className="flex items-center gap-2">
        <Shield className="h-4 w-4 text-green-600 shrink-0" />
        <h4 className="text-sm font-medium">{t("detail.evolution.guardrails")}</h4>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
        <div className="space-y-0.5">
          <p className="text-xs text-muted-foreground">{t("detail.evolution.maxDelta")}</p>
          <p className="font-mono">{guardrails.max_delta_per_cycle}</p>
        </div>
        <div className="space-y-0.5">
          <p className="text-xs text-muted-foreground">{t("detail.evolution.minDataPoints")}</p>
          <p className="font-mono">{guardrails.min_data_points}</p>
        </div>
        <div className="space-y-0.5">
          <p className="text-xs text-muted-foreground">{t("detail.evolution.rollbackDrop")}</p>
          <p className="font-mono">{guardrails.rollback_on_drop_pct}%</p>
        </div>
      </div>

      {guardrails.locked_params.length > 0 && (
        <div className="space-y-1">
          <p className="text-xs text-muted-foreground">{t("detail.evolution.lockedParams")}</p>
          <div className="flex flex-wrap gap-1">
            {guardrails.locked_params.map((p) => (
              <Badge key={p} variant="secondary" className="text-xs">{p}</Badge>
            ))}
          </div>
        </div>
      )}
    </section>
  );
}
