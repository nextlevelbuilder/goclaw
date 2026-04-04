import { useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Upload, FileArchive, Info, CheckCircle2, SkipForward } from "lucide-react";
import { Button } from "@/components/ui/button";
import { OperationProgress, type ProgressStep } from "@/components/shared/operation-progress";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import type { SseCompleteEvent } from "@/hooks/use-sse-progress";
import { useSkillsImport, useMcpImport } from "./hooks/use-capabilities-import";

interface ImportSummaryEntry {
  label: string;
  value: number;
  type?: "created" | "skipped";
}

interface DropZoneImportTabProps {
  dropLabel: string;
  dropFormats: string;
  startLabel: string;
  doneLabel: string;
  errorLabel: string;
  importingLabel: string;
  infoNote?: string;
  onImport: (file: File) => void;
  status: string;
  steps: ProgressStep[];
  elapsed: number;
  error: { detail: string } | null;
  result: SseCompleteEvent | null;
  summaryEntries?: (result: SseCompleteEvent) => ImportSummaryEntry[];
  reset: () => void;
  cancel: () => void;
}

function DropZoneImportTab({
  dropLabel,
  dropFormats,
  startLabel,
  doneLabel,
  errorLabel,
  importingLabel,
  infoNote,
  onImport,
  status,
  steps,
  elapsed,
  error,
  result,
  summaryEntries,
  reset,
  cancel,
}: DropZoneImportTabProps) {
  const { t } = useTranslation("import-export");
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);

  const handleFile = useCallback((f: File) => setFile(f), []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragging(false);
      const f = e.dataTransfer.files[0];
      if (f) handleFile(f);
    },
    [handleFile],
  );

  if (status !== "idle") {
    const entries = status === "complete" && result && summaryEntries ? summaryEntries(result) : [];
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          {status === "complete" ? doneLabel : status === "error" ? errorLabel : importingLabel}
        </h3>
        {status === "running" && <OperationProgress steps={steps} elapsed={elapsed} />}
        {status === "complete" && entries.length > 0 && (
          <div className="rounded-lg border bg-card divide-y text-sm">
            {entries.map((e, i) => (
              <div key={i} className="flex items-center gap-2 px-4 py-2">
                {e.type === "skipped"
                  ? <SkipForward className="h-4 w-4 text-muted-foreground" />
                  : <CheckCircle2 className="h-4 w-4 text-green-500" />}
                <span>{e.label}</span>
                <span className="ml-auto text-xs font-medium tabular-nums">{e.value}</span>
              </div>
            ))}
            <div className="text-xs text-muted-foreground px-4 py-2">
              {elapsed < 60 ? `${elapsed}s` : `${Math.floor(elapsed / 60)}m ${elapsed % 60}s`}
            </div>
          </div>
        )}
        {status === "complete" && entries.length === 0 && (
          <OperationProgress steps={steps} elapsed={elapsed} />
        )}
        {status === "error" && error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
            <p className="text-destructive font-medium">{error.detail}</p>
          </div>
        )}
        <div className="flex items-center justify-end gap-2 pt-2">
          {status === "running" && (
            <Button variant="outline" onClick={cancel}>
              {t("common.cancel", { ns: "common" })}
            </Button>
          )}
          {(status === "complete" || status === "error") && (
            <Button
              variant="outline"
              onClick={() => {
                setFile(null);
                reset();
              }}
            >
              {status === "complete" ? t("teamImportAnother") : t("teamTryAgain")}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {infoNote && (
        <div className="flex items-start gap-2 rounded-md border bg-muted/30 px-3 py-2">
          <Info className="h-4 w-4 mt-0.5 shrink-0 text-muted-foreground" />
          <p className="text-xs text-muted-foreground">{infoNote}</p>
        </div>
      )}

      {!file && (
        <div
          className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 transition-colors cursor-pointer ${
            dragging
              ? "border-primary bg-primary/5"
              : "border-muted-foreground/25 hover:border-muted-foreground/50"
          }`}
          onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
        >
          <Upload className="h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">{dropLabel}</p>
          <p className="text-xs text-muted-foreground/60">{dropFormats}</p>
          <input
            ref={fileRef}
            type="file"
            className="hidden"
            accept=".tar.gz,.gz"
            onChange={(e) => { const f = e.target.files?.[0]; if (f) handleFile(f); }}
          />
        </div>
      )}

      {file && (
        <>
          <div className="rounded-md border bg-muted/50 p-3 text-sm">
            <div className="flex items-center gap-2">
              <FileArchive className="h-4 w-4 text-muted-foreground" />
              <span className="font-medium">{file.name}</span>
              <span className="text-xs text-muted-foreground ml-auto">
                {(file.size / 1024).toFixed(0)} KB
              </span>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => setFile(null)}>
              {t("teamChangeFile")}
            </Button>
            <Button onClick={() => onImport(file)}>
              <Upload className="mr-1.5 h-4 w-4" />
              {startLabel}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}

function SkillsImportTab() {
  const { t } = useTranslation("import-export");
  const imp = useSkillsImport();

  return (
    <DropZoneImportTab
      dropLabel={t("skillsMcp.dropSkills")}
      dropFormats={t("teamDropFormats")}
      startLabel={t("skillsMcp.importSkills")}
      doneLabel={t("import.done")}
      errorLabel={t("import.errorTitle")}
      importingLabel={t("import.importing")}
      onImport={imp.startImport}
      status={imp.status}
      steps={imp.steps}
      elapsed={imp.elapsed}
      error={imp.error}
      result={imp.result}
      summaryEntries={(r) => [
        { label: t("skillsMcp.importedSkills"), value: (r.skills_imported as number) ?? 0 },
        ...(r.skills_skipped ? [{ label: t("skillsMcp.skippedSkills"), value: r.skills_skipped as number, type: "skipped" as const }] : []),
        { label: t("skillsMcp.grants"), value: (r.grants_applied as number) ?? 0 },
      ]}
      reset={imp.reset}
      cancel={imp.cancel}
    />
  );
}

function McpImportTab() {
  const { t } = useTranslation("import-export");
  const imp = useMcpImport();

  return (
    <DropZoneImportTab
      dropLabel={t("skillsMcp.dropMcp")}
      dropFormats={t("teamDropFormats")}
      startLabel={t("skillsMcp.importMcp")}
      doneLabel={t("import.done")}
      errorLabel={t("import.errorTitle")}
      importingLabel={t("import.importing")}
      infoNote={t("skillsMcp.mcpNote")}
      onImport={imp.startImport}
      status={imp.status}
      steps={imp.steps}
      elapsed={imp.elapsed}
      error={imp.error}
      result={imp.result}
      summaryEntries={(r) => [
        { label: t("skillsMcp.importedServers"), value: (r.servers_imported as number) ?? 0 },
        ...(r.servers_skipped ? [{ label: t("skillsMcp.skippedServers"), value: r.servers_skipped as number, type: "skipped" as const }] : []),
        { label: t("skillsMcp.grants"), value: (r.grants_applied as number) ?? 0 },
      ]}
      reset={imp.reset}
      cancel={imp.cancel}
    />
  );
}

export function CapabilitiesImportPanel() {
  const { t } = useTranslation("import-export");

  return (
    <Tabs defaultValue="skills">
      <TabsList>
        <TabsTrigger value="skills">{t("skillsMcp.skillsTab")}</TabsTrigger>
        <TabsTrigger value="mcp">{t("skillsMcp.mcpTab")}</TabsTrigger>
      </TabsList>
      <TabsContent value="skills" className="mt-4">
        <SkillsImportTab />
      </TabsContent>
      <TabsContent value="mcp" className="mt-4">
        <McpImportTab />
      </TabsContent>
    </Tabs>
  );
}
