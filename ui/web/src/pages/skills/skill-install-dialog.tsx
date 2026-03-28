import { useState, useRef } from "react";
import { useTranslation } from "react-i18next";
import {
  Upload, Globe, Loader2, CheckCircle2, XCircle, X, Package,
} from "lucide-react";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";
import { validateSkillZip } from "./lib/validate-skill-zip";
import { uniqueId } from "@/lib/utils";

/* ── Types ── */

type Tab = "file" | "url";
type FileStatus = "validating" | "valid" | "invalid" | "uploading" | "success" | "error";

interface FileEntry {
  id: string;
  file: File;
  status: FileStatus;
  name?: string;
  slug?: string;
  error?: string;
}

interface SkillPreview {
  name: string;
  slug: string;
  description: string;
  dir: string;
  has_scripts: boolean;
}

interface InstallResult {
  installed: Array<{ name: string; slug: string; deps_warning?: string }>;
  errors?: string[];
}

interface SkillInstallDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUpload: (file: File) => Promise<unknown>;
  onPreviewURL: (url: string, branch?: string) => Promise<{ skills: SkillPreview[]; total: number }>;
  onInstallURL: (url: string, slugs: string[], branch?: string) => Promise<{
    installed: Array<{ id: string; slug: string; version: number; name: string; deps_warning?: string }>;
    total: number;
    errors?: string[];
  }>;
}

type URLStep = "input" | "select" | "installing" | "done";

export function SkillInstallDialog({
  open, onOpenChange, onUpload, onPreviewURL, onInstallURL,
}: SkillInstallDialogProps) {
  const { t } = useTranslation("skills");
  const [tab, setTab] = useState<Tab>("file");

  /* ── File upload state ── */
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [uploading, setUploading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [fileDone, setFileDone] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  /* ── URL install state ── */
  const [url, setUrl] = useState("");
  const [branch, setBranch] = useState("");
  const [urlStep, setUrlStep] = useState<URLStep>("input");
  const [urlLoading, setUrlLoading] = useState(false);
  const [previews, setPreviews] = useState<SkillPreview[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [urlResult, setUrlResult] = useState<InstallResult | null>(null);
  const [urlError, setUrlError] = useState("");

  const busy = uploading || urlStep === "installing";

  /* ── Reset ── */
  const resetAll = () => {
    setEntries([]); setUploading(false); setDragging(false); setFileDone(false);
    setUrl(""); setBranch(""); setUrlStep("input"); setUrlLoading(false);
    setPreviews([]); setSelected(new Set()); setUrlResult(null); setUrlError("");
  };

  const handleClose = (v: boolean) => {
    if (busy) return;
    resetAll();
    onOpenChange(v);
  };

  const switchTab = (t: Tab) => {
    if (busy) return;
    resetAll();
    setTab(t);
  };

  /* ═══════════ File upload ═══════════ */

  const addFiles = async (fileList: FileList) => {
    const newFiles = Array.from(fileList);
    const existingNames = new Set(entries.map((e) => e.file.name));
    const fresh = newFiles.filter((f) => !existingNames.has(f.name));
    if (fresh.length === 0) return;

    const pending: FileEntry[] = fresh.map((f) => ({
      id: uniqueId(), file: f, status: "validating" as const,
    }));
    setEntries((prev) => [...prev, ...pending]);

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

  const removeEntry = (id: string) => setEntries((prev) => prev.filter((e) => e.id !== id));

  const handleFileSubmit = async () => {
    const validEntries = entries.filter((e) => e.status === "valid");
    if (validEntries.length === 0) return;
    setUploading(true);
    for (const entry of validEntries) {
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
    setFileDone(true);
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault(); setDragging(false);
    if (e.dataTransfer.files.length > 0) addFiles(e.dataTransfer.files);
  };
  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    if (e.target.files && e.target.files.length > 0) addFiles(e.target.files);
    if (inputRef.current) inputRef.current.value = "";
  };

  const validCount = entries.filter((e) => e.status === "valid").length;
  const successCount = entries.filter((e) => e.status === "success").length;

  /* ═══════════ URL install ═══════════ */

  const handlePreview = async () => {
    if (!url.trim()) return;
    setUrlLoading(true); setUrlError("");
    try {
      const res = await onPreviewURL(url.trim(), branch.trim() || undefined);
      const skills = res.skills ?? [];
      setPreviews(skills);
      setSelected(new Set(skills.map((s) => s.slug)));
      setUrlStep("select");
    } catch (err) {
      setUrlError(err instanceof Error ? err.message : "Failed to fetch");
    } finally {
      setUrlLoading(false);
    }
  };

  const handleURLInstall = async () => {
    const slugs = [...selected];
    if (slugs.length === 0) return;
    setUrlStep("installing"); setUrlError("");
    try {
      const res = await onInstallURL(url.trim(), slugs, branch.trim() || undefined);
      setUrlResult({ installed: res.installed ?? [], errors: res.errors });
      setUrlStep("done");
    } catch (err) {
      setUrlResult({ installed: [], errors: [err instanceof Error ? err.message : "Install failed"] });
      setUrlStep("done");
    }
  };

  const toggleSkill = (slug: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(slug)) next.delete(slug); else next.add(slug);
      return next;
    });
  };
  const toggleAll = () => {
    setSelected(selected.size === previews.length ? new Set() : new Set(previews.map((s) => s.slug)));
  };

  const isGitHub = /github\.com\/[^/]+\/[^/]+/.test(url);

  /* ═══════════ Render ═══════════ */

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-lg max-h-[80dvh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("install.title")}</DialogTitle>
          <DialogDescription>{t("install.description")}</DialogDescription>
        </DialogHeader>

        {/* Tab switcher — hidden during multi-step URL flow */}
        {!(tab === "url" && urlStep !== "input") && !fileDone && !uploading && (
          <div className="flex gap-1 border-b">
            <button
              type="button"
              className={cn(
                "flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
                tab === "file" ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              onClick={() => switchTab("file")}
            >
              <Upload className="h-3.5 w-3.5" /> {t("install.tabFile")}
            </button>
            <button
              type="button"
              className={cn(
                "flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium border-b-2 -mb-px",
                tab === "url" ? "border-primary text-primary" : "border-transparent text-muted-foreground hover:text-foreground",
              )}
              onClick={() => switchTab("url")}
            >
              <Globe className="h-3.5 w-3.5" /> {t("install.tabUrl")}
            </button>
          </div>
        )}

        <div className="flex flex-col gap-4 overflow-y-auto flex-1">
          {/* ═══ FILE TAB ═══ */}
          {tab === "file" && (
            <>
              {!uploading && !fileDone && (
                <div
                  role="button"
                  tabIndex={0}
                  className={cn(
                    "flex cursor-pointer flex-col items-center gap-2 rounded-md border-2 border-dashed p-6 text-center transition-colors",
                    dragging ? "border-primary bg-primary/5" : "hover:border-primary/50",
                  )}
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
                  <input ref={inputRef} type="file" accept=".zip" multiple className="hidden" onChange={handleInputChange} />
                </div>
              )}

              {entries.length > 0 && (
                <div className="flex flex-col gap-1 overflow-y-auto max-h-[40dvh]">
                  {entries.map((entry) => (
                    <FileEntryRow key={entry.id} entry={entry} onRemove={() => removeEntry(entry.id)} uploading={uploading} t={t} />
                  ))}
                </div>
              )}
              {entries.length > 0 && !fileDone && !uploading && (
                <p className="text-xs text-muted-foreground">{t("upload.validCount", { valid: validCount, total: entries.length })}</p>
              )}
              {fileDone && (
                <p className="text-sm font-medium text-muted-foreground">{t("upload.successCount", { success: successCount, total: entries.length })}</p>
              )}
            </>
          )}

          {/* ═══ URL TAB ═══ */}
          {tab === "url" && (
            <>
              {/* Step 1: URL input */}
              {urlStep === "input" && (
                <>
                  <div className="flex flex-col gap-2">
                    <Label htmlFor="skill-url">{t("installUrl.urlLabel")}</Label>
                    <Input
                      id="skill-url"
                      placeholder="https://github.com/owner/repo"
                      value={url}
                      onChange={(e) => setUrl(e.target.value)}
                      disabled={urlLoading}
                      className="text-base md:text-sm"
                      onKeyDown={(e) => { if (e.key === "Enter") handlePreview(); }}
                    />
                  </div>
                  {isGitHub && (
                    <div className="flex flex-col gap-2">
                      <Label htmlFor="skill-branch">{t("installUrl.branchLabel")}</Label>
                      <Input
                        id="skill-branch"
                        placeholder="main"
                        value={branch}
                        onChange={(e) => setBranch(e.target.value)}
                        disabled={urlLoading}
                        className="text-base md:text-sm"
                      />
                      <p className="text-xs text-muted-foreground">{t("installUrl.branchHint")}</p>
                    </div>
                  )}
                  {urlError && (
                    <div className="flex items-center gap-2 text-sm text-destructive">
                      <XCircle className="h-4 w-4 shrink-0" /><span>{urlError}</span>
                    </div>
                  )}
                </>
              )}

              {/* Step 2: Select skills */}
              {urlStep === "select" && (
                <>
                  <div className="flex items-center justify-between">
                    <p className="text-sm text-muted-foreground">{t("installUrl.foundSkills", { count: previews.length })}</p>
                    <Button variant="ghost" size="sm" onClick={toggleAll} className="text-xs h-7">
                      {selected.size === previews.length ? t("installUrl.deselectAll") : t("installUrl.selectAll")}
                    </Button>
                  </div>
                  <div className="flex flex-col gap-1 overflow-y-auto max-h-[40dvh]">
                    {previews.map((skill) => (
                      <label
                        key={skill.slug}
                        className="flex items-start gap-3 rounded-md border px-3 py-2.5 cursor-pointer hover:bg-muted/30 transition-colors"
                      >
                        <input
                          type="checkbox"
                          checked={selected.has(skill.slug)}
                          onChange={() => toggleSkill(skill.slug)}
                          className="mt-0.5 h-4 w-4 rounded border-input accent-primary"
                        />
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center gap-2">
                            <Package className="h-4 w-4 text-muted-foreground shrink-0" />
                            <span className="font-medium text-sm">{skill.name}</span>
                            <span className="text-xs text-muted-foreground">({skill.slug})</span>
                          </div>
                          {skill.description && (
                            <p className="text-xs text-muted-foreground mt-0.5 line-clamp-2">{skill.description}</p>
                          )}
                          {skill.dir && (
                            <p className="text-[10px] text-muted-foreground/60 mt-0.5">{skill.dir}/</p>
                          )}
                        </div>
                      </label>
                    ))}
                  </div>
                </>
              )}

              {/* Step 3: Installing */}
              {urlStep === "installing" && (
                <div className="flex flex-col items-center gap-3 py-6">
                  <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
                  <p className="text-sm text-muted-foreground">{t("installUrl.installingCount", { count: selected.size })}</p>
                </div>
              )}

              {/* Step 4: Results */}
              {urlStep === "done" && urlResult && (
                <div className="flex flex-col gap-2">
                  {urlResult.installed.length > 0 && (
                    <div className="flex flex-col gap-1">
                      {urlResult.installed.map((s) => (
                        <div key={s.slug} className="flex items-center gap-2 text-sm">
                          <CheckCircle2 className="h-4 w-4 text-green-600 shrink-0" />
                          <span className="font-medium">{s.name}</span>
                          {s.deps_warning && <span className="text-xs text-amber-600">{s.deps_warning}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                  {urlResult.errors && urlResult.errors.length > 0 && (
                    <div className="flex flex-col gap-1">
                      {urlResult.errors.map((err, i) => (
                        <div key={i} className="flex items-center gap-2 text-sm">
                          <XCircle className="h-4 w-4 text-destructive shrink-0" />
                          <span className="text-destructive">{err}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}
        </div>

        {/* ═══ Footer ═══ */}
        <DialogFooter>
          {tab === "file" && (
            <>
              <Button variant="outline" onClick={() => handleClose(false)} disabled={uploading}>
                {t("upload.cancel")}
              </Button>
              {fileDone ? (
                <Button onClick={() => handleClose(false)}>{t("upload.done")}</Button>
              ) : (
                <Button onClick={handleFileSubmit} disabled={validCount === 0 || uploading}>
                  {uploading ? t("upload.uploading") : t("upload.uploadCount", { count: validCount })}
                </Button>
              )}
            </>
          )}
          {tab === "url" && urlStep === "input" && (
            <>
              <Button variant="outline" onClick={() => handleClose(false)}>{t("upload.cancel")}</Button>
              <Button onClick={handlePreview} disabled={!url.trim() || urlLoading}>
                {urlLoading ? (
                  <><Loader2 className="h-4 w-4 animate-spin mr-1" />{t("installUrl.scanning")}</>
                ) : (
                  <><Globe className="h-4 w-4 mr-1" />{t("installUrl.scan")}</>
                )}
              </Button>
            </>
          )}
          {tab === "url" && urlStep === "select" && (
            <>
              <Button variant="outline" onClick={() => setUrlStep("input")}>{t("installUrl.back")}</Button>
              <Button onClick={handleURLInstall} disabled={selected.size === 0}>
                {t("installUrl.installCount", { count: selected.size })}
              </Button>
            </>
          )}
          {tab === "url" && urlStep === "done" && (
            <Button onClick={() => handleClose(false)}>{t("installUrl.close")}</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/* ── File entry row (reused from original upload dialog) ── */

function FileEntryRow({
  entry, onRemove, uploading, t,
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
          <span className="truncate font-medium">{entry.name || entry.file.name}</span>
          <span className="shrink-0 text-xs text-muted-foreground">{sizeKB} KB</span>
        </div>
        {entry.status === "invalid" || entry.status === "error" ? (
          <p className="text-xs text-destructive truncate">{entry.error ? t(entry.error) : t("upload.failed")}</p>
        ) : entry.status === "validating" ? (
          <p className="text-xs text-muted-foreground">{t("upload.validating")}</p>
        ) : entry.name && entry.status !== "success" ? (
          <p className="text-xs text-muted-foreground truncate">{entry.file.name}</p>
        ) : null}
      </div>
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
    case "invalid":
    case "error":
      return <XCircle className="h-4 w-4 shrink-0 text-destructive" />;
  }
}
