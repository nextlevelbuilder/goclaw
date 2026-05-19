import { CheckCircle2, ShieldCheck, Trash2, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

interface SkillBulkActionsToolbarProps {
  selectedCount: number;
  customSelectedCount: number;
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

  return (
    <div
      className="mt-3 flex flex-wrap items-center gap-2 rounded-md border px-3 py-2 transition-colors"
      style={{ visibility: hasSelection ? "visible" : "hidden" }}
    >
      <span className="text-sm font-medium">
        {t("bulk.selected", { count: selectedCount })}
      </span>
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
