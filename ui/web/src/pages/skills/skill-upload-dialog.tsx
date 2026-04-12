import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import { Upload, CheckCircle2, XCircle, Loader2, TriangleAlert, X, Package } from "lucide-react";
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
import { uniqueId } from "@/lib/utils";
import type { SkillUploadResponse } from "./hooks/use-skills";
import JSZip from "jszip";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** Per-skill lifecycle status */
type SkillStatus =
  | "validating"
  | "valid"        // new skill, ready to upload
  | "unchanged"    // server returned identical hash — skipped
  | "invalid"
  | "uploading"
  | "success"
  | "warning"      // uploaded but deps_warning present
  | "error";

interface SkillEntry {
  id: string;
  dir: string;
  status: SkillStatus;
  name?: string;
  slug?: string;
  contentHash?: string;
  error?: string;
}

interface FileEntry {
  id: string;
  file: File;
  /** One entry per detected skill in this ZIP */
  skills: SkillEntry[];
}

interface SkillUploadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpload: (file: File) => Promise<SkillUploadResponse>;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

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
            // Top-level ZIP error (corrupt, too large, not a zip)
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
          const skills: SkillEntry[] = validation.skills.map((s) => ({
            id: uniqueId(),
            dir: s.dir,
            status: s.valid ? ("valid" as SkillStatus) : ("invalid" as SkillStatus),
            name: s.name,
            slug: s.slug,
            contentHash: s.contentHash,
            error: s.error,
          }));
          return { id: entry.id, skills };
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
  // Upload
  // ---------------------------------------------------------------------------

  const handleSubmit = async () => {
    // Gather all actionable skills across all file entries
    const actionable = entries.flatMap((e) =>
      e.skills
        .filter((s) => s.status === "valid")
        .map((s) => ({ fileEntry: e, skill: s })),
    );
    if (actionable.length === 0) return;

    setUploading(true);

    for (const { fileEntry, skill } of actionable) {
      // Mark this skill as uploading
      setEntries((prev) =>
        prev.map((e) =>
          e.id === fileEntry.id
            ? { ...e, skills: e.skills.map((s) => s.id === skill.id ? { ...s, status: "uploading" as SkillStatus } : s) }
            : e,
        ),
      );

      try {
        // For multi-skill ZIPs extract just this skill's files into a sub-ZIP.
        // Single-skill ZIPs (dir === "") upload the original file directly.
        const uploadFile =
          skill.dir && fileEntry.skills.length > 1
            ? await createSkillSubZip(fileEntry.file, skill.dir)
            : fileEntry.file;

        const result = await onUpload(uploadFile);

        // Handle "unchanged" response (content hash matched on server)
        if (result.status === "unchanged") {
          setEntries((prev) =>
            prev.map((e) =>
              e.id === fileEntry.id
                ? {
                    ...e,
                    skills: e.skills.map((s) =>
                      s.id === skill.id ? { ...s, status: "unchanged" as SkillStatus } : s,
                    ),
                  }
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
  // Derived counts (operate on skill level, not file level)
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

// ---------------------------------------------------------------------------
// FileEntryBlock — renders a ZIP file with its skill rows
// ---------------------------------------------------------------------------

function FileEntryBlock({
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
  const isMulti = entry.skills.length > 1;
  const sizeKB = (entry.file.size / 1024).toFixed(1);
  const isValidating = entry.skills.some((s) => s.status === "validating");

  if (!isMulti) {
    // Single-skill: render as before — one row, ZIP filename as subtitle
    const skill = entry.skills[0]!;
    return (
      <SkillRow
        skill={skill}
        subtitle={skill.name ? entry.file.name : undefined}
        primaryLabel={skill.name || entry.file.name}
        sizeKB={sizeKB}
        showSize
        onRemove={onRemove}
        uploading={uploading}
        t={t}
      />
    );
  }

  // Multi-skill: group header + individual skill rows
  return (
    <div className="rounded-md border overflow-hidden">
      {/* ZIP group header */}
      <div className="flex items-center gap-2 bg-muted/40 px-3 py-1.5 text-xs text-muted-foreground">
        <Package className="h-3.5 w-3.5 shrink-0" />
        <span className="flex-1 truncate font-medium">{entry.file.name}</span>
        <span className="shrink-0">{sizeKB} KB</span>
        {isValidating ? null : (
          <span className="shrink-0">
            {t("upload.multiDetected", { count: entry.skills.length })}
          </span>
        )}
        {!uploading && (
          <button
            type="button"
            aria-label={t("upload.remove")}
            onClick={(e) => { e.stopPropagation(); onRemove(); }}
            className="shrink-0 rounded-sm p-0.5 text-muted-foreground hover:text-foreground"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* Individual skill rows — indented */}
      <div className="flex flex-col divide-y">
        {entry.skills.map((skill) => (
          <SkillRow
            key={skill.id}
            skill={skill}
            primaryLabel={skill.name || skill.dir || skill.slug || "…"}
            subtitle={skill.dir || undefined}
            showSize={false}
            onRemove={undefined}
            uploading={uploading}
            t={t}
            indent
          />
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// SkillRow — single skill entry row
// ---------------------------------------------------------------------------

function SkillRow({
  skill,
  primaryLabel,
  subtitle,
  sizeKB,
  showSize,
  onRemove,
  uploading,
  t,
  indent = false,
}: {
  skill: SkillEntry;
  primaryLabel: string;
  subtitle?: string;
  sizeKB?: string;
  showSize: boolean;
  onRemove?: () => void;
  uploading: boolean;
  t: (key: string, opts?: Record<string, unknown>) => string;
  indent?: boolean;
}) {
  const canRemove = !uploading && skill.status !== "uploading" && skill.status !== "success" && onRemove;

  return (
    <div className={`flex items-center gap-2 px-3 py-2 text-sm ${indent ? "pl-6" : "rounded-md border"}`}>
      <SkillStatusIcon status={skill.status} />
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-1.5">
          <span className="truncate font-medium">{primaryLabel}</span>
          <SkillBadge status={skill.status} t={t} />
          {showSize && sizeKB && (
            <span className="shrink-0 text-xs text-muted-foreground">{sizeKB} KB</span>
          )}
        </div>
        <SkillSubtitle skill={skill} subtitle={subtitle} t={t} />
      </div>
      {canRemove && (
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

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function SkillSubtitle({
  skill,
  subtitle,
  t,
}: {
  skill: SkillEntry;
  subtitle?: string;
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  if (skill.status === "invalid" || skill.status === "error") {
    return (
      <p className="text-xs text-destructive truncate">
        {skill.error ? t(skill.error) : t("upload.failed")}
      </p>
    );
  }
  if (skill.status === "warning") {
    return <p className="text-xs text-amber-600 truncate">{skill.error ?? t("upload.failed")}</p>;
  }
  if (skill.status === "validating") {
    return <p className="text-xs text-muted-foreground">{t("upload.validating")}</p>;
  }
  if (skill.status === "unchanged") {
    return (
      <p className="text-xs text-muted-foreground truncate">
        {t("upload.skillUnchanged")}
      </p>
    );
  }
  if (subtitle && skill.status !== "success") {
    return <p className="text-xs text-muted-foreground truncate">{subtitle}</p>;
  }
  return null;
}

/** Colored badge for NEW / UNCHANGED / ERROR states */
function SkillBadge({ status, t }: { status: SkillStatus; t: (key: string) => string }) {
  const base = "shrink-0 text-[10px] font-semibold uppercase px-1.5 py-0.5 rounded";
  switch (status) {
    case "valid":
      return <span className={`${base} bg-green-50 text-green-700 border border-green-200`}>{t("upload.new")}</span>;
    case "unchanged":
      return <span className={`${base} bg-purple-50 text-purple-700 border border-purple-200`}>{t("upload.unchanged")}</span>;
    case "invalid":
    case "error":
      return <span className={`${base} bg-red-50 text-red-700 border border-red-200`}>{t("upload.failed")}</span>;
    default:
      return null;
  }
}

function SkillStatusIcon({ status }: { status: SkillStatus }) {
  switch (status) {
    case "validating":
    case "uploading":
      return <Loader2 className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" />;
    case "valid":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-primary" />;
    case "unchanged":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-muted-foreground" />;
    case "success":
      return <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />;
    case "warning":
      return <TriangleAlert className="h-4 w-4 shrink-0 text-amber-600" />;
    case "invalid":
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
}

// ---------------------------------------------------------------------------
// createSkillSubZip — extract one skill's directory into a standalone ZIP
// ---------------------------------------------------------------------------

/** Extract files under `dir/` from the original ZIP, strip the prefix, return a new File */
async function createSkillSubZip(originalFile: File, dir: string): Promise<File> {
  const zip = await JSZip.loadAsync(originalFile);
  const sub = new JSZip();
  const prefix = dir + "/";

  for (const [path, entry] of Object.entries(zip.files)) {
    if (path.startsWith(prefix) && !entry.dir) {
      const relativePath = path.slice(prefix.length);
      if (relativePath) {
        sub.file(relativePath, await entry.async("blob"));
      }
    }
  }

  const blob = await sub.generateAsync({ type: "blob" });
  return new File([blob], `${dir}.zip`, { type: "application/zip" });
}
