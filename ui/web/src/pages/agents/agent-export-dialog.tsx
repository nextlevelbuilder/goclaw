import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Download } from "lucide-react";

interface AgentExportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  agentKey: string;
  agentName?: string;
  onExport: (id: string, agentKey: string, include: string[]) => Promise<void>;
}

export function AgentExportDialog({ open, onOpenChange, agentId, agentKey, agentName, onExport }: AgentExportDialogProps) {
  const { t } = useTranslation("agents");
  const [includeContextFiles, setIncludeContextFiles] = useState(true);
  const [includeMemory, setIncludeMemory] = useState(false);
  const [includeKG, setIncludeKG] = useState(false);
  const [exporting, setExporting] = useState(false);

  const handleExport = async () => {
    const include: string[] = [];
    if (includeContextFiles) include.push("context_files");
    if (includeMemory) include.push("memory");
    if (includeKG) include.push("knowledge_graph");

    setExporting(true);
    try {
      await onExport(agentId, agentKey, include);
      onOpenChange(false);
    } catch {
      // error shown by toast
    } finally {
      setExporting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("export.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-1 py-2">
          {agentName && (
            <p className="text-sm text-muted-foreground mb-3">{agentName}</p>
          )}

          <div className="flex items-center justify-between py-2">
            <Label htmlFor="export-context" className="cursor-pointer">{t("export.contextFiles")}</Label>
            <Switch id="export-context" checked={includeContextFiles} onCheckedChange={setIncludeContextFiles} />
          </div>

          <div className="flex items-center justify-between py-2">
            <div>
              <Label htmlFor="export-memory" className="cursor-pointer">{t("export.memory")}</Label>
              <p className="text-xs text-muted-foreground mt-0.5">{t("export.memoryHint")}</p>
            </div>
            <Switch id="export-memory" checked={includeMemory} onCheckedChange={setIncludeMemory} />
          </div>

          <div className="flex items-center justify-between py-2">
            <div>
              <Label htmlFor="export-kg" className="cursor-pointer">{t("export.knowledgeGraph")}</Label>
              <p className="text-xs text-muted-foreground mt-0.5">{t("export.knowledgeGraphHint")}</p>
            </div>
            <Switch id="export-kg" checked={includeKG} onCheckedChange={setIncludeKG} />
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleExport} disabled={exporting || (!includeContextFiles && !includeMemory && !includeKG)}>
            <Download className="mr-1.5 h-4 w-4" />
            {exporting ? t("export.exporting") : t("export.download")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
