import { useState, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { Upload, FileArchive, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { OperationProgress } from "@/components/shared/operation-progress";
import { useTeamImport } from "./hooks/use-team-import";

export function TeamImportPanel() {
  const { t } = useTranslation("import-export");
  const fileRef = useRef<HTMLInputElement>(null);
  const [file, setFile] = useState<File | null>(null);
  const [dragging, setDragging] = useState(false);

  const imp = useTeamImport();

  const handleFile = useCallback((f: File) => { setFile(f); }, []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
    const f = e.dataTransfer.files[0];
    if (f) handleFile(f);
  }, [handleFile]);

  const handleSubmit = () => {
    if (!file) return;
    imp.startImport(file);
  };

  // Running / complete / error
  if (imp.status !== "idle") {
    return (
      <div className="space-y-4">
        <h3 className="text-sm font-medium">
          {imp.status === "complete" ? t("import.done") : imp.status === "error" ? t("import.errorTitle") : t("import.importing")}
        </h3>
        <OperationProgress steps={imp.steps} elapsed={imp.elapsed} />
        {imp.status === "error" && imp.error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm">
            <p className="text-destructive font-medium">{imp.error.detail}</p>
          </div>
        )}
        <div className="flex items-center justify-end gap-2 pt-2">
          {imp.status === "running" && (
            <>
              <p className="text-xs text-amber-600 mr-auto flex items-center gap-1">
                <AlertTriangle className="h-3.5 w-3.5" />
                {t("teamDontClose")}
              </p>
              <Button variant="outline" onClick={imp.cancel}>{t("common.cancel", { ns: "common" })}</Button>
            </>
          )}
          {(imp.status === "complete" || imp.status === "error") && (
            <Button variant="outline" onClick={() => { setFile(null); imp.reset(); }}>
              {imp.status === "complete" ? t("teamImportAnother") : t("teamTryAgain")}
            </Button>
          )}
        </div>
      </div>
    );
  }

  // Idle
  return (
    <div className="space-y-4">
      {!file && (
        <div
          className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-8 transition-colors cursor-pointer ${
            dragging ? "border-primary bg-primary/5" : "border-muted-foreground/25 hover:border-muted-foreground/50"
          }`}
          onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={handleDrop}
          onClick={() => fileRef.current?.click()}
        >
          <Upload className="h-8 w-8 text-muted-foreground/50" />
          <p className="text-sm text-muted-foreground">{t("teamDropHere")}</p>
          <p className="text-xs text-muted-foreground/60">{t("teamDropFormats")}</p>
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
              <span className="text-xs text-muted-foreground ml-auto">{(file.size / 1024).toFixed(0)} KB</span>
            </div>
          </div>

          <div className="flex items-center justify-end gap-2 pt-2">
            <Button variant="outline" onClick={() => setFile(null)}>{t("teamChangeFile")}</Button>
            <Button onClick={handleSubmit}>
              <Upload className="mr-1.5 h-4 w-4" />
              {t("import.startImport")}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
