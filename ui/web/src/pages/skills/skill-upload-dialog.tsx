import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Upload } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { validateMultiSkillZip } from "./lib/validate-skill-zip";
import { createSkillSubZip } from "./lib/create-skill-sub-zip";
import { uniqueId } from "@/lib/utils";
import type { SkillUploadResponse } from "./hooks/use-skills";
import type { FileEntry, SkillStatus } from "./lib/skill-upload-types";
import { FileEntryBlock } from "./skill-upload-entry";
import JSZip from "jszip";

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

  // ---------------------------------------------------------------------------
  // File handling
  // ---------------------------------------------------------------------------

  const addFiles = async (fileList: FileList) => {
    const newFiles = Array.from(fileList);

    const existingNames = new Set(entries.map((e) => e.file.name));
    const fresh = newFiles.filter((f) => !existingNames.has(f.name));
    if (fresh.length === 0) return;

    // Add placeholder entries with validating status
    const pending: FileEntry[] = fresh.map((f) => ({
      id: uniqueId(),
      file: f,
      skills: [{ id: uniqueId(), dir: "", status: "validating" as const }],
    }));
    setEntries((prev) => [...prev, ...pending]);

    // Validate all files concurrently
    const results = await Promise.all(
      pending.map(async (entry) => {
        try {
          const validation = await validateMultiSkillZip(entry.file);
          const placeholderId = entry.skills[0]?.id ?? uniqueId();
          if (validation.error) {
            return {
              id: entry.id,
              skills: [{ id: placeholderId, dir: "", status: "invalid" as SkillStatus, error: validation.error }],
            };
          }
          if (validation.skills.length === 0) {
            return {
              id: entry.id,
              skills: [{ id: placeholderId, dir: "", status: "invalid" as SkillStatus, error: "upload.noSkillMd" }],
            };
          }
          return {
            id: entry.id,
            skills: validation.skills.map((s) => ({
              id: uniqueId(),
              dir: s.dir,
              status: s.valid ? ("valid" as SkillStatus) : ("invalid" as SkillStatus),
              name: s.name,
              slug: s.slug,
              contentHash: s.contentHash,
              error: s.error,
            })),
          };
        } catch {
          return {
            id: entry.id,
            skills: [{ id: entry.skills[0]?.id ?? uniqueId(), dir: "", status: "invalid" as SkillStatus, error: "upload.invalidZip" }],
          };
        }
      }),
    );

    setEntries((prev) =>
      prev.map((e) => {
        const match = results.find((r) => r.id === e.id);
        return match ? { ...e, skills: match.skills } : e;
      }),
    );
  };

  const removeEntry = (id: string) => {
    setEntries((prev) => prev.filter((e) => e.id !== id));
  };

  // ---------------------------------------------------------------------------
  // Upload — parse each ZIP once, reuse across all skills in that file
  // ---------------------------------------------------------------------------

  const handleSubmit = async () => {
    const actionable = entries.flatMap((e) =>
      e.skills
        .filter((s) => s.status === "valid")
        .map((s) => ({ fileEntry: e, skill: s })),
    );
    if (actionable.length === 0) return;

    setUploading(true);

    // Cache parsed JSZip instances keyed by FileEntry id — avoids re-parsing
    // the same ZIP blob for every skill in a multi-skill archive (O(N) vs O(N*M)).
    const parsedZips = new Map<string, JSZip>();

    for (const { fileEntry, skill } of actionable) {
      setEntries((prev) =>
        prev.map((e) =>
          e.id === fileEntry.id
            ? { ...e, skills: e.skills.map((s) => s.id === skill.id ? { ...s, status: "uploading" as SkillStatus } : s) }
            : e,
        ),
      );

      try {
        let uploadFile: File;
        if (skill.dir && fileEntry.skills.length > 1) {
          // Parse the ZIP once per FileEntry; reuse on subsequent skills
          if (!parsedZips.has(fileEntry.id)) {
            parsedZips.set(fileEntry.id, await JSZip.loadAsync(fileEntry.file));
          }
          uploadFile = await createSkillSubZip(parsedZips.get(fileEntry.id)!, skill.dir);
        } else {
          uploadFile = fileEntry.file;
        }

        const result = await onUpload(uploadFile);

        if (result.status === "unchanged") {
          setEntries((prev) =>
            prev.map((e) =>
              e.id === fileEntry.id
                ? { ...e, skills: e.skills.map((s) => s.id === skill.id ? { ...s, status: "unchanged" as SkillStatus } : s) }
                : e,
            ),
          );
          continue;
        }

        const depDetail = result.deps_warning
          ? result.deps_errors?.length
            ? `${result.deps_warning}: ${result.deps_errors.join("; ")}`
            : result.deps_warning
          : undefined;

        setEntries((prev) =>
          prev.map((e) =>
            e.id === fileEntry.id
              ? {
                  ...e,
                  skills: e.skills.map((s) =>
                    s.id === skill.id
                      ? {
                          ...s,
                          status: result.deps_warning ? ("warning" as SkillStatus) : ("success" as SkillStatus),
                          error: depDetail,
                        }
                      : s,
                  ),
                }
              : e,
          ),
        );
      } catch (err) {
        setEntries((prev) =>
          prev.map((e) =>
            e.id === fileEntry.id
              ? {
                  ...e,
                  skills: e.skills.map((s) =>
                    s.id === skill.id
                      ? {
                          ...s,
                          status: "error" as SkillStatus,
                          error: err instanceof Error ? err.message : t("upload.failed"),
                        }
                      : s,
                  ),
                }
              : e,
          ),
        );
      }
    }

    setUploading(false);
    setDone(true);
  };

  // ---------------------------------------------------------------------------
  // Dialog housekeeping
  // ---------------------------------------------------------------------------

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

  // ---------------------------------------------------------------------------
  // Derived counts (skill level, not file level)
  // ---------------------------------------------------------------------------

  const allSkills = entries.flatMap((e) => e.skills);
  const actionableCount = allSkills.filter((s) => s.status === "valid").length;
  const successCount = allSkills.filter((s) => s.status === "success" || s.status === "warning").length;

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("upload.title")}</DialogTitle>
          <DialogDescription>{t("upload.description")}</DialogDescription>
        </DialogHeader>

        {/* Drop zone — hidden once upload starts or finishes */}
        {!uploading && !done && (
          <div
            role="button"
            tabIndex={0}
            className={`flex cursor-pointer flex-col items-center gap-2 rounded-md border-2 border-dashed p-6 text-center transition-colors ${
              dragging ? "border-primary bg-primary/5" : "hover:border-primary/50"
            }`}
            onClick={() => inputRef.current?.click()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                inputRef.current?.click();
              }
            }}
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

        {/* File + skill list */}
        {entries.length > 0 && (
          <div className="flex flex-col gap-1 overflow-y-auto max-h-[40dvh]">
            {entries.map((entry) => (
              <FileEntryBlock
                key={entry.id}
                entry={entry}
                onRemove={() => removeEntry(entry.id)}
                uploading={uploading}
                t={t}
              />
            ))}
          </div>
        )}

        {/* Summary line */}
        {entries.length > 0 && !done && !uploading && (
          <p className="text-xs text-muted-foreground">
            {t("upload.validCount", { valid: actionableCount, total: allSkills.length })}
          </p>
        )}
        {done && (
          <p className="text-sm font-medium text-muted-foreground">
            {t("upload.successCount", { success: successCount, total: allSkills.filter((s) => s.status !== "unchanged" && s.status !== "invalid").length })}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => handleClose(false)} disabled={uploading}>
            {t("upload.cancel")}
          </Button>
          {done ? (
            <Button onClick={() => handleClose(false)}>{t("upload.done")}</Button>
          ) : (
            <Button onClick={handleSubmit} disabled={actionableCount === 0 || uploading}>
              {uploading
                ? t("upload.uploading")
                : t("upload.uploadCount", { count: actionableCount })}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
