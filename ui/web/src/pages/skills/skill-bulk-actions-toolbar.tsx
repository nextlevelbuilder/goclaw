import { CheckCircle2, Download, ShieldCheck, Trash2, XCircle } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { SkillExportFormat } from "./lib/skill-export-download";

interface SkillBulkActionsToolbarProps {
  selectedCount: number;
  customSelectedCount: number;
  skippedSystemCount: number;
  agentCount: number;
  loading: boolean;
  downloadLoading: boolean;
  exportFormat: SkillExportFormat;
  onExportFormatChange: (format: SkillExportFormat) => void;
  onDownload: () => void;
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
  downloadLoading,
  exportFormat,
  onExportFormatChange,
  onDownload,
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
        <Select value={exportFormat} onValueChange={(value) => onExportFormatChange(value as SkillExportFormat)}>
          <SelectTrigger className="h-8 w-[104px]" aria-label={t("export.format")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="zip">ZIP</SelectItem>
            <SelectItem value="tar.gz">tar.gz</SelectItem>
            <SelectItem value="tgz">tgz</SelectItem>
          </SelectContent>
        </Select>
        <Button
          size="sm"
          variant="outline"
          className="gap-1"
          disabled={loading || downloadLoading || !hasSelection}
          onClick={onDownload}
        >
          <Download className="h-3.5 w-3.5" />
          {downloadLoading ? t("export.downloading") : t("export.downloadSelected")}
        </Button>
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
