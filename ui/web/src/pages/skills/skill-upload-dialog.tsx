import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Upload, CheckCircle2, XCircle, Loader2, TriangleAlert, X } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { validateSkillZip } from "./lib/validate-skill-zip";
import { uniqueId } from "@/lib/utils";
import type { SkillUploadResponse } from "./hooks/use-skills";

type FileStatus = "validating" | "valid" | "invalid" | "uploading" | "success" | "warning" | "error";

interface FileEntry {
  id: string;
  file: File;
  status: FileStatus;
  name?: string;
  slug?: string;
  error?: string;
}

interface SkillUploadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpload: (file: File) => Promise<SkillUploadResponse>;
}

export function SkillUploadDialog({ open, onOpenChange, onUpload }: SkillUploadDialogProps) {
  const { t } = useTranslation("skills");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [done, setDone] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const addFiles = async (fileList: FileList) => {
    const newFiles = Array.from(fileList);

    // Build pending list before setState — no side effects inside updater
    const existingNames = new Set(entries.map((e) => e.file.name));
    const fresh = newFiles.filter((f) => !existingNames.has(f.name));
    if (fresh.length === 0) return;

    const pending: FileEntry[] = fresh.map((f) => ({
      id: uniqueId(),
      file: f,
      status: "validating" as const,
    }));
    setEntries((prev) => [...prev, ...pending]);

    // Validate all files concurrently, catch per-file errors
    const results = await Promise.all(
      pending.map(async (entry) => {
        try {
          return { id: entry.id, result: await validateSkillZip(entry.file) };
        } catch {
          return { id: entry.id, result: { valid: false, error: "upload.invalidZip" } as const };
        }
      }),
    );
    setEntries((prev) =>
      prev.map((e) => {
        const match = results.find((r) => r.id === e.id);
        if (!match) return e;
        const { result } = match;
        return {
          ...e,
          status: result.valid ? "valid" : "invalid",
          name: "name" in result ? result.name : undefined,
          slug: "slug" in result ? result.slug : undefined,
          error: result.error,
        };
      }),
    );
  };

  const removeEntry = (id: string) => {
    setEntries((prev) => prev.filter((e) => e.id !== id));
  };

  const handleSubmit = async () => {
    const validEntries = entries.filter((e) => e.status === "valid");
    if (validEntries.length === 0) return;
    setUploading(true);

    for (const entry of validEntries) {
      setEntries((prev) =>
        prev.map((e) => (e.id === entry.id ? { ...e, status: "uploading" } : e)),
      );
      try {
        const result = await onUpload(entry.file);
        const detail = result.deps_warning
          ? result.deps_errors?.length
            ? `${result.deps_warning}: ${result.deps_errors.join("; ")}`
            : result.deps_warning
          : undefined;
        setEntries((prev) =>
          prev.map((e) => (e.id === entry.id ? {
            ...e,
            status: result.deps_warning ? "warning" : "success",
            error: detail,
          } : e)),
        );
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
    if (e.dataTransfer.files.length > 0) {
      addFiles(e.dataTransfer.files);
    }
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) {
      addFiles(e.target.files);
    }
    // Reset so same files can be re-selected
    if (inputRef.current) inputRef.current.value = "";
  };

  const validCount = entries.filter((e) => e.status === "valid").length;
  const successCount = entries.filter((e) => e.status === "success" || e.status === "warning").length;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("upload.title")}</DialogTitle>
          <DialogDescription>{t("upload.description")}</DialogDescription>
        </DialogHeader>

        {/* Drop zone — always visible unless uploading/done */}
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
            <input
              ref={inputRef}
              type="file"
              accept=".zip"
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
              <FileEntryRow
                key={entry.id}
                entry={entry}
                onRemove={() => removeEntry(entry.id)}
                uploading={uploading}
                t={t}
              />
            ))}
          </div>
        )}

        {/* Summary */}
        {entries.length > 0 && !done && !uploading && (
          <p className="text-xs text-muted-foreground">
            {t("upload.validCount", { valid: validCount, total: entries.length })}
          </p>
        )}
        {done && (
          <p className="text-sm font-medium text-muted-foreground">
            {t("upload.successCount", { success: successCount, total: entries.length })}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={uploading}>
            {t("upload.cancel")}
          </Button>
          {done ? (
            <Button onClick={() => handleClose(false)}>{t("upload.done")}</Button>
          ) : (
            <Button onClick={handleSubmit} disabled={validCount === 0 || uploading}>
              {uploading
                ? t("upload.uploading")
                : t("upload.uploadCount", { count: validCount })}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** Single file entry row with status icon, name, and remove button */
function FileEntryRow({
  entry,
  onRemove,
  uploading,
  t,
}: {
  entry: FileEntry;
  onRemove: () => void;
  uploading: boolean;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  const sizeKB = (entry.file.size / 1024).toFixed(1);

  return (
    <div className="flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
      <StatusIcon status={entry.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium">
            {entry.name || entry.file.name}
          </span>
          <span className="shrink-0 text-xs text-muted-foreground">{sizeKB} KB</span>
        </div>
        {entry.status === "invalid" || entry.status === "error" ? (
          <p className="text-xs text-destructive truncate">{entry.error ? t(entry.error) : t("upload.failed")}</p>
        ) : entry.status === "warning" ? (
          <p className="text-xs text-amber-600 truncate">{entry.error ?? t("upload.failed")}</p>
        ) : entry.status === "validating" ? (
          <p className="text-xs text-muted-foreground">{t("upload.validating")}</p>
        ) : entry.name && entry.status !== "success" ? (
          <p className="text-xs text-muted-foreground truncate">{entry.file.name}</p>
        ) : null}
      </div>
      {/* Remove button — hidden during upload and for completed entries */}
      {!uploading && entry.status !== "uploading" && entry.status !== "success" && (
        <button
          type="button"
          aria-label={t("upload.remove")}
          onClick={(e) => { e.stopPropagation(); onRemove(); }}
          className="shrink-0 rounded-sm p-1 text-muted-foreground hover:text-foreground"
        >
          <X className="h-3.5 w-3.5" />
        </button>
      )}
    </div>
  );
}

function StatusIcon({ status }: { status: FileStatus }) {
  switch (status) {
    case "validating":
    case "uploading":
      return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />;
    case "valid":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-primary" />;
    case "success":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />;
    case "warning":
      return <TriangleAlert className="h-4 w-4 shrink-0 text-amber-600" />;
    case "invalid":
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
}
