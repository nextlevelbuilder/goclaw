import { useState, useRef, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { AlertTriangle, Plus, Upload } from "lucide-react";
import { slugify, isValidSlug } from "@/lib/slug";
import type { AgentData } from "@/types/agent";

interface AgentImportDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onImport: (data: Record<string, unknown>, overrides?: { agent_key?: string; display_name?: string }) => Promise<AgentData>;
  onMergeImport?: (agentId: string, data: Record<string, unknown>, include: string[]) => Promise<unknown>;
  agents?: AgentData[];
}

interface ParsedExport {
  version: number;
  agent?: {
    display_name?: string;
    agent_key?: string;
    agent_type?: string;
    provider?: string;
    model?: string;
    [key: string]: unknown;
  };
  context_files?: { file_name: string; content: string }[];
  memories?: { path: string; content: string }[];
  knowledge_graph?: {
    entities?: unknown[];
    relations?: unknown[];
  };
}

type ImportMode = "new" | "existing";

export function AgentImportDialog({ open, onOpenChange, onImport, onMergeImport, agents }: AgentImportDialogProps) {
  const { t } = useTranslation("agents");
  const fileRef = useRef<HTMLInputElement>(null);
  const [parsed, setParsed] = useState<ParsedExport | null>(null);
  const [rawData, setRawData] = useState<Record<string, unknown> | null>(null);
  const [displayName, setDisplayName] = useState("");
  const [agentKey, setAgentKey] = useState("");
  const [keyTouched, setKeyTouched] = useState(false);
  const [error, setError] = useState("");
  const [importing, setImporting] = useState(false);
  const [mode, setMode] = useState<ImportMode>("new");
  const [targetAgentId, setTargetAgentId] = useState("");

  const reset = useCallback(() => {
    setParsed(null);
    setRawData(null);
    setDisplayName("");
    setAgentKey("");
    setKeyTouched(false);
    setError("");
    setImporting(false);
    setMode("new");
    setTargetAgentId("");
    if (fileRef.current) fileRef.current.value = "";
  }, []);

  const warnings = useMemo(() => {
    if (!parsed?.agent) return [];
    const w: string[] = [];
    if (!parsed.agent.provider) w.push(t("import.warnNoProvider"));
    if (!parsed.agent.model) w.push(t("import.warnNoModel"));
    if (parsed.agent.agent_type && parsed.agent.agent_type !== "open" && parsed.agent.agent_type !== "predefined") {
      w.push(t("import.warnInvalidType", { type: parsed.agent.agent_type }));
    }
    return w;
  }, [parsed, t]);

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
        // Data-only files (memory/KG) have no agent section — auto-switch to merge mode
        const isDataOnly = !json.agent;
        setParsed(json as ParsedExport);
        setRawData(json);
        setDisplayName(json.agent?.display_name || "");
        const key = json.agent?.agent_key || slugify(json.agent?.display_name || "");
        setAgentKey(key);
        setKeyTouched(false);
        if (isDataOnly) setMode("existing");
      } catch {
        setError(t("import.invalidJson"));
      }
    };
    reader.readAsText(file);
  };

  const handleNameChange = (val: string) => {
    setDisplayName(val);
    if (!keyTouched) {
      setAgentKey(slugify(val));
    }
  };

  const handleSubmit = async () => {
    if (!rawData) return;

    if (mode === "existing") {
      if (!targetAgentId || !onMergeImport || !parsed) return;
      // Build include list from available data sections
      const include: string[] = [];
      if ((parsed.context_files?.length ?? 0) > 0) include.push("context_files");
      if ((parsed.memories?.length ?? 0) > 0) include.push("memory");
      if ((parsed.knowledge_graph?.entities?.length ?? 0) > 0) include.push("knowledge_graph");
      if (include.length === 0) return;
      setImporting(true);
      try {
        await onMergeImport(targetAgentId, rawData, include);
        reset();
        onOpenChange(false);
      } catch {
        // error shown by toast
      } finally {
        setImporting(false);
      }
      return;
    }

    // mode === "new"
    if (!isValidSlug(agentKey)) {
      setError(t("import.invalidKey"));
      return;
    }
    if (warnings.length > 0 && !parsed?.agent?.provider) {
      // Block creation if missing critical fields
      setError(t("import.warnNoProvider"));
      return;
    }
    setImporting(true);
    try {
      await onImport(rawData, { agent_key: agentKey, display_name: displayName });
      reset();
      onOpenChange(false);
    } catch {
      // error shown by toast
    } finally {
      setImporting(false);
    }
  };

  const hasData = (parsed?.context_files?.length ?? 0) > 0
    || (parsed?.memories?.length ?? 0) > 0
    || (parsed?.knowledge_graph?.entities?.length ?? 0) > 0;

  const canMerge = onMergeImport && agents && agents.length > 0;

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v) reset(); onOpenChange(v); }}>
      <DialogContent className="max-sm:inset-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("import.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* File picker */}
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

          {/* Preview after file loaded */}
          {parsed && (
            <>
              <div className="rounded-md border bg-muted/50 p-3 text-xs space-y-1">
                {parsed.agent && (
                  <>
                    <div><span className="text-muted-foreground">{t("import.type")}:</span> {parsed.agent.agent_type || "open"}</div>
                    <div><span className="text-muted-foreground">{t("import.model")}:</span> {[parsed.agent.provider, parsed.agent.model].filter(Boolean).join(" / ") || "—"}</div>
                  </>
                )}
                <div><span className="text-muted-foreground">{t("import.files")}:</span> {parsed.context_files?.length ?? 0}</div>
                {(parsed.memories?.length ?? 0) > 0 && (
                  <div><span className="text-muted-foreground">{t("mergeImport.memories")}:</span> {parsed.memories?.length}</div>
                )}
                {(parsed.knowledge_graph?.entities?.length ?? 0) > 0 && (
                  <div><span className="text-muted-foreground">{t("mergeImport.kgEntities")}:</span> {parsed.knowledge_graph?.entities?.length} entities, {parsed.knowledge_graph?.relations?.length ?? 0} relations</div>
                )}
              </div>

              {/* Warnings */}
              {warnings.length > 0 && (
                <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/30 p-3 text-xs space-y-1">
                  {warnings.map((w, i) => (
                    <div key={i} className="flex items-start gap-1.5 text-amber-700 dark:text-amber-400">
                      <AlertTriangle className="h-3.5 w-3.5 mt-0.5 shrink-0" />
                      <span>{w}</span>
                    </div>
                  ))}
                </div>
              )}

              {/* Import mode selector — only show for full exports (has agent config). Data-only files are locked to merge mode. */}
              {canMerge && parsed.agent && (
                <div>
                  <Label>{t("import.importMode")}</Label>
                  <Select value={mode} onValueChange={(v) => setMode(v as ImportMode)}>
                    <SelectTrigger className="mt-1 text-base md:text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="new">
                        <div className="flex items-center gap-1.5">
                          <Plus className="h-3.5 w-3.5" />
                          {t("import.modeNew")}
                        </div>
                      </SelectItem>
                      <SelectItem value="existing">
                        <div className="flex items-center gap-1.5">
                          <Upload className="h-3.5 w-3.5" />
                          {t("import.modeExisting")}
                        </div>
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}

              {/* New agent fields */}
              {mode === "new" && (
                <>
                  <div>
                    <Label>{t("create.displayName")}</Label>
                    <Input
                      value={displayName}
                      onChange={(e) => handleNameChange(e.target.value)}
                      placeholder={t("create.displayNamePlaceholder")}
                      className="mt-1 text-base md:text-sm"
                    />
                  </div>

                  <div>
                    <Label>{t("create.agentKey")}</Label>
                    <Input
                      value={agentKey}
                      onChange={(e) => { setAgentKey(e.target.value); setKeyTouched(true); }}
                      placeholder={t("create.agentKeyPlaceholder")}
                      className="mt-1 font-mono text-base md:text-sm"
                    />
                    <p className="mt-1 text-xs text-muted-foreground">{t("create.agentKeyHint")}</p>
                  </div>
                </>
              )}

              {/* Existing agent selector */}
              {mode === "existing" && agents && (
                <div>
                  <Label>{t("import.targetAgent")}</Label>
                  <Select value={targetAgentId} onValueChange={setTargetAgentId}>
                    <SelectTrigger className="mt-1 text-base md:text-sm">
                      <SelectValue placeholder={t("import.selectAgent")} />
                    </SelectTrigger>
                    <SelectContent>
                      {agents.map((a) => (
                        <SelectItem key={a.id} value={a.id}>
                          {a.display_name || a.agent_key}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {!hasData && (
                    <p className="mt-1 text-xs text-muted-foreground">{t("mergeImport.noData")}</p>
                  )}
                </div>
              )}
            </>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => { reset(); onOpenChange(false); }}>
            {t("create.cancel")}
          </Button>
          <Button
            onClick={handleSubmit}
            disabled={
              !parsed || importing
              || (mode === "new" && (warnings.length > 0 || !parsed.agent))
              || (mode === "existing" && (!targetAgentId || !hasData))
            }
          >
            <Upload className="mr-1.5 h-4 w-4" />
            {importing ? t("import.importing") : mode === "existing" ? t("mergeImport.import") : t("import.import")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
