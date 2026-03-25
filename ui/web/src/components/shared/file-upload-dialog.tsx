import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Upload, CheckCircle2, XCircle, Loader2, X } from "lucide-react";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { uniqueId } from "@/lib/utils";

/** Blocked extensions matching backend tools.blockedExtensions. */
const BLOCKED_EXTENSIONS = new Set([
  ".exe", ".sh", ".bat", ".cmd", ".ps1", ".com", ".msi", ".scr",
]);

const MAX_FILE_SIZE = 50 * 1024 * 1024; // 50MB — matches backend tools.MaxFileSizeBytes

type FileStatus = "checking" | "ready" | "uploading" | "success" | "error";

interface FileEntry {
  id: string;
  file: File;
  status: FileStatus;
  error?: string;
}

interface FileUploadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpload: (file: File) => Promise<void>;
  title?: string;
  description?: string;
}

export function FileUploadDialog({
  open, onOpenChange, onUpload, title, description,
}: FileUploadDialogProps) {
  const { t } = useTranslation("common");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [uploading, setUploading] = useState(false);
  const [done, setDone] = useState(false);
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const addFiles = (fileList: FileList) => {
    const existingNames = new Set(entries.map((e) => e.file.name));
    const fresh = Array.from(fileList).filter((f) => !existingNames.has(f.name));
    if (fresh.length === 0) return;

    const newEntries: FileEntry[] = fresh.map((f) => {
      const ext = "." + f.name.split(".").pop()?.toLowerCase();
      if (BLOCKED_EXTENSIONS.has(ext)) {
        return { id: uniqueId(), file: f, status: "error" as const, error: t("upload.blockedType", { ext }) };
      }
      if (f.size > MAX_FILE_SIZE) {
        return { id: uniqueId(), file: f, status: "error" as const, error: t("upload.tooLarge") };
      }
      return { id: uniqueId(), file: f, status: "ready" as const };
    });
    setEntries((prev) => [...prev, ...newEntries]);
  };

  const removeEntry = (id: string) => {
    setEntries((prev) => prev.filter((e) => e.id !== id));
  };

  const handleSubmit = async () => {
    const readyEntries = entries.filter((e) => e.status === "ready");
    if (readyEntries.length === 0) return;
    setUploading(true);

    for (const entry of readyEntries) {
      setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: "uploading" } : e)));
      try {
        await onUpload(entry.file);
        setEntries((prev) => prev.map((e) => (e.id === entry.id ? { ...e, status: "success" } : e)));
      } catch (err) {
        setEntries((prev) =>
          prev.map((e) =>
            e.id === entry.id
              ? { ...e, status: "error", error: err instanceof Error ? err.message : t("upload.failed") }
              : e,
          ),
        );
      }
    }
    setUploading(false);
    setDone(true);
  };

  const handleClose = (v: boolean) => {
    if (uploading) return;
    setEntries([]);
    setDragging(false);
    setDone(false);
    onOpenChange(v);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    setDragging(false);
    if (e.dataTransfer.files.length > 0) addFiles(e.dataTransfer.files);
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) addFiles(e.target.files);
    if (inputRef.current) inputRef.current.value = "";
  };

  const readyCount = entries.filter((e) => e.status === "ready").length;
  const successCount = entries.filter((e) => e.status === "success").length;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{title ?? t("upload.title")}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>

        {/* Drop zone */}
        {!uploading && !done && (
          <div
            role="button"
            tabIndex={0}
            className={`flex cursor-pointer flex-col items-center gap-2 rounded-md border-2 border-dashed p-6 text-center transition-colors ${
              dragging ? "border-primary bg-primary/5" : "hover:border-primary/50"
            }`}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); inputRef.current?.click(); } }}
            onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
            onDragEnter={(e) => { e.preventDefault(); setDragging(true); }}
            onDragLeave={() => setDragging(false)}
            onDrop={handleDrop}
          >
            <Upload className="h-8 w-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">
              {dragging ? t("upload.dropHere") : t("upload.dropOrClick")}
            </p>
            <p className="text-xs text-muted-foreground/60">{t("upload.maxSize")}</p>
            <input
              ref={inputRef}
              type="file"
              multiple
              className="hidden"
              onChange={handleInputChange}
            />
          </div>
        )}

        {/* File list */}
        {entries.length > 0 && (
          <div className="flex flex-col gap-1 overflow-y-auto max-h-[40dvh]">
            {entries.map((entry) => (
              <div key={entry.id} className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
                <StatusIcon status={entry.status} />
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-medium">{entry.file.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">
                      {(entry.file.size / 1024).toFixed(1)} KB
                    </span>
                  </div>
                  {(entry.status === "error") && (
                    <p className="text-xs text-destructive truncate">{entry.error}</p>
                  )}
                </div>
                {!uploading && entry.status !== "uploading" && entry.status !== "success" && (
                  <button
                    type="button"
                    onClick={(e) => { e.stopPropagation(); removeEntry(entry.id); }}
                    className="shrink-0 rounded-sm p-1 text-muted-foreground hover:text-foreground"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )}

        {/* Summary */}
        {entries.length > 0 && !done && !uploading && (
          <p className="text-xs text-muted-foreground">
            {t("upload.readyCount", { ready: readyCount, total: entries.length })}
          </p>
        )}
        {done && (
          <p className="text-sm font-medium text-muted-foreground">
            {t("upload.successCount", { success: successCount, total: entries.length })}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={uploading}>
            {t("cancel")}
          </Button>
          {done ? (
            <Button onClick={() => handleClose(false)}>{t("done", "Done")}</Button>
          ) : (
            <Button onClick={handleSubmit} disabled={readyCount === 0 || uploading}>
              {uploading
                ? t("upload.uploading")
                : t("upload.uploadCount", { count: readyCount })}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function StatusIcon({ status }: { status: FileStatus }) {
  switch (status) {
    case "checking":
    case "uploading":
      return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />;
    case "ready":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-primary" />;
    case "success":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />;
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
}
