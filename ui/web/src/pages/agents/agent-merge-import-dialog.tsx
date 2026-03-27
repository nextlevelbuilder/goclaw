import { useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Upload } from "lucide-react";

interface ParsedExport {
  version: number;
  agent: Record<string, unknown>;
  context_files?: { file_name: string; content: string }[];
  memories?: { path: string; content: string }[];
  knowledge_graph?: {
    entities?: { external_id: string }[];
    relations?: { relation_type: string }[];
  };
}

interface AgentMergeImportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  agentId: string;
  agentName: string;
  onMergeImport: (agentId: string, data: Record<string, unknown>, include: string[]) => Promise<unknown>;
}

export function AgentMergeImportDialog({ open, onOpenChange, agentId, agentName, onMergeImport }: AgentMergeImportDialogProps) {
  const { t } = useTranslation("agents");
  const fileRef = useRef<HTMLInputElement>(null);
  const [parsed, setParsed] = useState<ParsedExport | null>(null);
  const [rawData, setRawData] = useState<Record<string, unknown> | null>(null);
  const [error, setError] = useState("");
  const [importing, setImporting] = useState(false);
  const [includeFiles, setIncludeFiles] = useState(true);
  const [includeMemory, setIncludeMemory] = useState(true);
  const [includeKG, setIncludeKG] = useState(true);

  const reset = useCallback(() => {
    setParsed(null);
    setRawData(null);
    setError("");
    setImporting(false);
    setIncludeFiles(true);
    setIncludeMemory(true);
    setIncludeKG(true);
    if (fileRef.current) fileRef.current.value = "";
  }, []);

  const handleFile = (e: React.ChangeEvent<HTMLInputElement>) => {
    setError("");
    const file = e.target.files?.[0];
    if (!file) return;

    const reader = new FileReader();
    reader.onload = (ev) => {
      try {
        const json = JSON.parse(ev.target?.result as string);
        if (!json.version) {
          setError(t("import.invalidFormat"));
          return;
        }
        setParsed(json as ParsedExport);
        setRawData(json);
      } catch {
        setError(t("import.invalidJson"));
      }
    };
    reader.readAsText(file);
  };

  const handleSubmit = async () => {
    if (!rawData) return;
    const include: string[] = [];
    if (includeFiles && fileCount > 0) include.push("context_files");
    if (includeMemory && memoryCount > 0) include.push("memory");
    if (includeKG && entityCount > 0) include.push("knowledge_graph");
    if (include.length === 0) return;

    setImporting(true);
    try {
      await onMergeImport(agentId, rawData, include);
      reset();
      onOpenChange(false);
    } catch {
      // error shown by toast
    } finally {
      setImporting(false);
    }
  };

  const fileCount = parsed?.context_files?.length ?? 0;
  const memoryCount = parsed?.memories?.length ?? 0;
  const entityCount = parsed?.knowledge_graph?.entities?.length ?? 0;
  const relationCount = parsed?.knowledge_graph?.relations?.length ?? 0;
  const hasAnyData = fileCount > 0 || memoryCount > 0 || entityCount > 0;
  const hasSelected = (includeFiles && fileCount > 0)
    || (includeMemory && memoryCount > 0)
    || (includeKG && entityCount > 0);

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) reset(); onOpenChange(v); }}>
      <DialogContent className="max-sm:inset-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("mergeImport.title")}</DialogTitle>
          <DialogDescription>{t("mergeImport.description", { name: agentName })}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          <div>
            <Label>{t("import.selectFile")}</Label>
            <div className="mt-1.5">
              <Input
                ref={fileRef}
                type="file"
                accept=".json,.agent.json"
                onChange={handleFile}
                className="text-base md:text-sm"
              />
            </div>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}

          {parsed && !hasAnyData && (
            <p className="text-sm text-muted-foreground">{t("mergeImport.noData")}</p>
          )}

          {parsed && hasAnyData && (
            <div className="space-y-2">
              <p className="text-xs text-muted-foreground">{t("mergeImport.selectSections")}</p>

              {fileCount > 0 && (
                <div className="flex items-center justify-between py-1.5">
                  <div>
                    <Label htmlFor="merge-files" className="cursor-pointer">{t("export.contextFiles")}</Label>
                    <p className="text-xs text-muted-foreground">{t("mergeImport.fileCount", { count: fileCount })}</p>
                  </div>
                  <Switch id="merge-files" checked={includeFiles} onCheckedChange={setIncludeFiles} />
                </div>
              )}

              {memoryCount > 0 && (
                <div className="flex items-center justify-between py-1.5">
                  <div>
                    <Label htmlFor="merge-memory" className="cursor-pointer">{t("export.memory")}</Label>
                    <p className="text-xs text-muted-foreground">{t("mergeImport.memoryCount", { count: memoryCount })}</p>
                    <p className="text-xs text-muted-foreground">{t("mergeImport.memoryMergeHint")}</p>
                  </div>
                  <Switch id="merge-memory" checked={includeMemory} onCheckedChange={setIncludeMemory} />
                </div>
              )}

              {entityCount > 0 && (
                <div className="flex items-center justify-between py-1.5">
                  <div>
                    <Label htmlFor="merge-kg" className="cursor-pointer">{t("export.knowledgeGraph")}</Label>
                    <p className="text-xs text-muted-foreground">{t("mergeImport.kgCount", { entities: entityCount, relations: relationCount })}</p>
                    <p className="text-xs text-muted-foreground">{t("mergeImport.kgMergeHint")}</p>
                  </div>
                  <Switch id="merge-kg" checked={includeKG} onCheckedChange={setIncludeKG} />
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => { reset(); onOpenChange(false); }}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={!parsed || !hasSelected || importing}>
            <Upload className="mr-1.5 h-4 w-4" />
            {importing ? t("mergeImport.importing") : t("mergeImport.import")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
