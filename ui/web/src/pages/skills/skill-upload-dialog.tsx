import { useEffect, useState, useRef } from "react";
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
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Badge } from "@/components/ui/badge";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { createSkillSubZip } from "./lib/create-skill-sub-zip";
import { resolveUploadSkills } from "./lib/resolve-upload-skills";
import { summarizeUploadEntries } from "./lib/skill-upload-summary";
import { uniqueId } from "@/lib/utils";
import type { SkillUploadOptions, SkillUploadResponse } from "./hooks/use-skills";
import type { FileEntry, SkillStatus } from "./lib/skill-upload-types";
import { FileEntryBlock } from "./skill-upload-entry";
import { useSkillUploadLimit } from "./hooks/use-skill-upload-limit";
import JSZip from "jszip";

interface SkillUploadDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpload: (file: File, options?: SkillUploadOptions) => Promise<SkillUploadResponse>;
}

export function SkillUploadDialog({ open, onOpenChange, onUpload }: SkillUploadDialogProps) {
  const { t } = useTranslation("skills");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [done, setDone] = useState(false);
  const [grantManagers, setGrantManagers] = useState(true);
  const [managerAgentIds, setManagerAgentIds] = useState<string[]>([]);
  const { agents, refresh: refreshAgents } = useAgents();
  const maxUploadSizeMB = useSkillUploadLimit(open);
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (open) refreshAgents();
  }, [open, refreshAgents]);

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
        const resolved = await resolveUploadSkills(entry.file, undefined, { maxUploadSizeMB });
        return {
          id: entry.id,
          skills: resolved.map((skill) => ({
            id: uniqueId(),
            ...skill,
          })),
        };
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

        const result = await onUpload(uploadFile, {
          managerAgentIds: grantManagers ? managerAgentIds : [],
        });

        if (result.status === "unchanged") {
          const grantDetail = result.grant_errors?.length
            ? result.grant_errors.join("; ")
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
                            status: grantDetail ? ("warning" as SkillStatus) : ("unchanged" as SkillStatus),
                            error: grantDetail,
                          }
                        : s,
                    ),
                  }
                : e,
            ),
          );
          continue;
        }

        const grantDetail = result.grant_errors?.length
          ? result.grant_errors.join("; ")
          : undefined;
        const depDetail = result.deps_warning
          ? result.deps_errors?.length
            ? `${result.deps_warning}: ${result.deps_errors.join("; ")}`
            : result.deps_warning
          : undefined;
        const warningDetail = [depDetail, grantDetail].filter(Boolean).join("; ") || undefined;

        setEntries((prev) =>
          prev.map((e) =>
            e.id === fileEntry.id
              ? {
                  ...e,
                  skills: e.skills.map((s) =>
                    s.id === skill.id
                      ? {
                          ...s,
                          status: warningDetail ? ("warning" as SkillStatus) : ("success" as SkillStatus),
                          error: warningDetail,
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
    setGrantManagers(true);
    setManagerAgentIds([]);
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

  const uploadSummary = summarizeUploadEntries(entries);
  const actionableCount = uploadSummary.valid;
  const allCurrentAgentsSelected = agents.length > 0 && managerAgentIds.length === agents.length;

  const toggleManagerAgent = (id: string) => {
    setManagerAgentIds((current) =>
      current.includes(id) ? current.filter((item) => item !== id) : [...current, id],
    );
  };

  const toggleAllAgents = () => {
    setManagerAgentIds(allCurrentAgentsSelected ? [] : agents.map((agent) => agent.id));
  };

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("upload.title")}</DialogTitle>
          <DialogDescription>{t("upload.description", { max: maxUploadSizeMB })}</DialogDescription>
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

        {entries.length > 0 && !uploading && !done && (
          <div className="space-y-3 rounded-md border p-3">
            <label className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium">{t("upload.agentManagers")}</span>
              <Switch checked={grantManagers} onCheckedChange={setGrantManagers} />
            </label>
            {grantManagers && (
              <div className="space-y-2">
                <div className="flex items-center justify-between gap-2">
                  <Label className="text-xs text-muted-foreground">{t("upload.managerAgentsHelp")}</Label>
                  <Button type="button" variant="ghost" size="sm" onClick={toggleAllAgents} disabled={agents.length === 0}>
                    {allCurrentAgentsSelected ? t("upload.clearAgents") : t("upload.selectAllCurrentAgents")}
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t("upload.selectedAgentsCount", { selected: managerAgentIds.length, total: agents.length })}
                </p>
                <div className="max-h-32 overflow-y-auto rounded-md border">
                  {agents.length === 0 ? (
                    <p className="px-3 py-2 text-sm text-muted-foreground">{t("upload.noAgents")}</p>
                  ) : agents.map((agent) => (
                    <label key={agent.id} className="flex items-center gap-2 px-3 py-2 text-sm hover:bg-muted/40">
                      <input
                        type="checkbox"
                        checked={managerAgentIds.includes(agent.id)}
                        onChange={() => toggleManagerAgent(agent.id)}
                        className="h-4 w-4"
                      />
                      <span className="min-w-0 truncate">{agent.display_name || agent.agent_key}</span>
                    </label>
                  ))}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Summary line */}
        {entries.length > 0 && !done && !uploading && (
          <p className="text-xs text-muted-foreground">
            {t("upload.validCount", { valid: actionableCount, total: uploadSummary.total })}
          </p>
        )}
        {done && (
          <div className="flex flex-wrap gap-1.5 text-sm">
            <Badge variant="success">{t("upload.summaryUploaded", { count: uploadSummary.uploaded })}</Badge>
            <Badge variant="warning">{t("upload.summaryWarnings", { count: uploadSummary.warnings })}</Badge>
            <Badge variant="outline">{t("upload.summaryUnchanged", { count: uploadSummary.unchanged })}</Badge>
            <Badge variant={uploadSummary.failed + uploadSummary.invalid > 0 ? "destructive" : "outline"}>
              {t("upload.summaryFailed", { count: uploadSummary.failed + uploadSummary.invalid })}
            </Badge>
          </div>
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
