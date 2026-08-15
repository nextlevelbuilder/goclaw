import { useTranslation } from "react-i18next";
import { Network } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { useOrchestration } from "@/hooks/use-orchestration";

const MODE_COLORS: Record<string, string> = {
  spawn: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
  delegate: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  team: "bg-purple-100 text-purple-700 dark:bg-purple-900 dark:text-purple-300",
};

interface OrchestrationSectionProps {
  agentId: string;
}

export function OrchestrationSection({ agentId }: OrchestrationSectionProps) {
  const { t } = useTranslation("agents");
  const { mode, delegateTargets, team, loading } = useOrchestration(agentId);

  if (loading) return null;

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Network className="h-4 w-4 text-indigo-500 shrink-0" />
          <h3 className="text-sm font-medium">{t("detail.orchestration.title")}</h3>
        </div>
        <Badge variant="outline" className={MODE_COLORS[mode] ?? ""}>
          {mode}
        </Badge>
      </div>

      {/* Delegate targets */}
      {delegateTargets.length > 0 && (
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-muted-foreground">
            {t("detail.orchestration.delegateTargets")}
          </p>
          <div className="flex flex-wrap gap-1.5">
            {delegateTargets.map((target) => (
              <Badge key={target.agent_key} variant="secondary" className="text-xs">
                {target.display_name || target.agent_key}
              </Badge>
            ))}
          </div>
        </div>
      )}

      {/* Team info */}
      {team && (
        <div className="space-y-1">
          <p className="text-xs font-medium text-muted-foreground">{t("detail.orchestration.team")}</p>
          <Badge variant="secondary" className="text-xs">
            {team.name}
          </Badge>
        </div>
      )}

      {delegateTargets.length === 0 && !team && mode === "spawn" && (
        <p className="text-xs text-muted-foreground">
          {t("detail.orchestration.noDelegates")}
        </p>
      )}
    </section>
  );
}
