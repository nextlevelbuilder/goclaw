import { CheckCircle2, ShieldCheck, Trash2, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface SkillBulkActionsToolbarProps {
  selectedCount: number;
  customSelectedCount: number;
  skippedSystemCount: number;
  agentCount: number;
  loading: boolean;
  onEnable: () => void;
  onDisable: () => void;
  onGrantAllAgents: () => void;
  onDelete: () => void;
  onClear: () => void;
}

export function SkillBulkActionsToolbar({
  selectedCount,
  customSelectedCount,
  skippedSystemCount,
  agentCount,
  loading,
  onEnable,
  onDisable,
  onGrantAllAgents,
  onDelete,
  onClear,
}: SkillBulkActionsToolbarProps) {
  const { t } = useTranslation("skills");
  const hasSelection = selectedCount > 0;
  const customActionDisabledReason = agentCount === 0
    ? t("bulk.noAgentsReason")
    : customSelectedCount === 0
      ? t("bulk.customOnlyReason")
      : undefined;

  if (!hasSelection) return null;

  return (
    <div className="mt-3 flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 transition-colors">
      <div className="flex flex-col gap-0.5">
        <span className="text-sm font-medium">
          {t("bulk.selected", { count: selectedCount })}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("bulk.customAvailable", { count: customSelectedCount })}
          {skippedSystemCount > 0 ? ` · ${t("bulk.skippedSystem", { count: skippedSystemCount })}` : ""}
        </span>
      </div>
      <div className="ml-auto flex flex-wrap gap-2">
        <Button size="sm" variant="outline" className="gap-1" disabled={loading || !hasSelection} onClick={onEnable}>
          <CheckCircle2 className="h-3.5 w-3.5" />
          {t("bulk.enable")}
        </Button>
        <Button size="sm" variant="outline" className="gap-1" disabled={loading || !hasSelection} onClick={onDisable}>
          <XCircle className="h-3.5 w-3.5" />
          {t("bulk.disable")}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="gap-1"
          disabled={loading || customSelectedCount === 0 || agentCount === 0}
          title={customActionDisabledReason}
          onClick={onGrantAllAgents}
        >
          <ShieldCheck className="h-3.5 w-3.5" />
          {t("bulk.grantAllAgents")}
        </Button>
        <Button
          size="sm"
          variant="outline"
          className="gap-1 text-destructive hover:text-destructive"
          disabled={loading || customSelectedCount === 0}
          title={customSelectedCount === 0 ? t("bulk.customOnlyReason") : undefined}
          onClick={onDelete}
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t("bulk.delete")}
        </Button>
        <Button size="sm" variant="ghost" disabled={loading || !hasSelection} onClick={onClear}>
          {t("bulk.clear")}
        </Button>
      </div>
    </div>
  );
}
