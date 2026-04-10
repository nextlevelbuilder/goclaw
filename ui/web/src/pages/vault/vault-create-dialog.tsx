import { useState, useEffect, useCallback, useRef, type ChangeEvent, type DragEvent } from "react";
import { useTranslation } from "react-i18next";
import { Upload, X, FileText } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from "@/components/ui/dialog";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useTeams } from "@/pages/teams/hooks/use-teams";
import { useVaultUpload } from "./hooks/use-vault-upload";

const ACCEPTED_EXTS = new Set([
  ".md", ".txt", ".json", ".yaml", ".yml", ".csv", ".toml", ".xml", ".html", ".htm",
  ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".rs", ".java", ".rb", ".sh", ".sql",
  ".swift", ".kt", ".c", ".cpp", ".h",
]);
const ACCEPT_ATTR = Array.from(ACCEPTED_EXTS).join(",");

function isAllowedExt(name: string): boolean {
  const dot = name.lastIndexOf(".");
  if (dot < 0) return false;
  return ACCEPTED_EXTS.has(name.slice(dot).toLowerCase());
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

type Destination = "shared" | "agent" | "team";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onUploaded?: () => void;
  defaultAgentId?: string;
  defaultTeamId?: string;
}

export function VaultCreateDialog({ open, onOpenChange, onUploaded, defaultAgentId, defaultTeamId }: Props) {
  const { t } = useTranslation("vault");
  const { agents } = useAgents();
  const { teams, load: loadTeams } = useTeams();
  const { upload, isPending } = useVaultUpload();
  const inputRef = useRef<HTMLInputElement>(null);

  const [destination, setDestination] = useState<Destination>("shared");
  const [agentId, setAgentId] = useState("");
  const [teamId, setTeamId] = useState("");
  const [files, setFiles] = useState<File[]>([]);
  const [dragging, setDragging] = useState(false);

  // Load teams on first open
  useEffect(() => { if (open) loadTeams(); }, [open, loadTeams]);

  // Reset state when dialog opens; pre-select destination from parent filters
  useEffect(() => {
    if (!open) return;
    setFiles([]);
    setDragging(false);
    if (defaultAgentId) {
      setDestination("agent");
      setAgentId(defaultAgentId);
    } else if (defaultTeamId) {
      setDestination("team");
      setTeamId(defaultTeamId);
    } else {
      setDestination("shared");
    }
  }, [open, defaultAgentId, defaultTeamId]);

  const addFiles = useCallback((incoming: File[]) => {
    const valid = incoming.filter((f) => isAllowedExt(f.name));
    setFiles((prev) => {
      const names = new Set(prev.map((f) => f.name));
      return [...prev, ...valid.filter((f) => !names.has(f.name))];
    });
  }, []);

  const onDrop = useCallback((e: DragEvent) => {
    e.preventDefault();
    setDragging(false);
    addFiles(Array.from(e.dataTransfer.files));
  }, [addFiles]);

  const onFileInput = useCallback((e: ChangeEvent<HTMLInputElement>) => {
    addFiles(Array.from(e.target.files ?? []));
    e.target.value = "";
  }, [addFiles]);

  const removeFile = (idx: number) => setFiles((prev) => prev.filter((_, i) => i !== idx));

  const handleUpload = async () => {
    const opts = destination === "agent" ? { agentId }
      : destination === "team" ? { teamId }
        : {};
    try {
      await upload(files, opts);
      onOpenChange(false);
      onUploaded?.();
    } catch {
      // error toasted in hook
    }
  };

  const canUpload = files.length > 0
    && !isPending
    && (destination !== "agent" || agentId)
    && (destination !== "team" || teamId);

  const handleClose = (v: boolean) => { if (!isPending) onOpenChange(v); };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md max-sm:inset-0">
        <DialogHeader>
          <DialogTitle>{t("upload.title", "Upload to Vault")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* Destination radio group */}
          <fieldset className="space-y-2">
            <Label>{t("upload.destination", "Destination")}</Label>
            <div className="space-y-2">
              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="radio" name="dest" checked={destination === "shared"}
                  onChange={() => setDestination("shared")} className="accent-primary" />
                {t("upload.shared", "Shared")}
              </label>

              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="radio" name="dest" checked={destination === "agent"}
                  onChange={() => setDestination("agent")} className="accent-primary" />
                {t("upload.agent", "Agent")}
              </label>
              {destination === "agent" && (
                <select value={agentId} onChange={(e) => setAgentId(e.target.value)}
                  className="ml-6 w-[calc(100%-1.5rem)] text-base md:text-sm border rounded px-2 py-1.5 bg-background">
                  <option value="">{t("upload.selectAgent", "Select agent...")}</option>
                  {(agents ?? []).map((a) => (
                    <option key={a.id} value={a.id}>{a.display_name || a.agent_key}</option>
                  ))}
                </select>
              )}

              <label className="flex items-center gap-2 text-sm cursor-pointer">
                <input type="radio" name="dest" checked={destination === "team"}
                  onChange={() => setDestination("team")} className="accent-primary" />
                {t("upload.team", "Team")}
              </label>
              {destination === "team" && (
                <select value={teamId} onChange={(e) => setTeamId(e.target.value)}
                  className="ml-6 w-[calc(100%-1.5rem)] text-base md:text-sm border rounded px-2 py-1.5 bg-background">
                  <option value="">{t("upload.selectTeam", "Select team...")}</option>
                  {(teams ?? []).map((team) => (
                    <option key={team.id} value={team.id}>{team.name}</option>
                  ))}
                </select>
              )}
            </div>
          </fieldset>

          {/* Drop zone */}
          <div
            onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
            onDragLeave={() => setDragging(false)}
            onDrop={onDrop}
            onClick={() => inputRef.current?.click()}
            className={`flex flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed p-6 cursor-pointer transition-colors ${
              dragging ? "border-primary bg-primary/5" : "border-muted-foreground/25 hover:border-primary/50"
            }`}
          >
            <Upload className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm text-muted-foreground">{t("upload.dropzone", "Drop files here or click to browse")}</p>
            <p className="text-xs text-muted-foreground/60">{t("upload.dropzoneHint", "Text files only (.md, .txt, .json, .yaml, .csv, ...)")}</p>
            <input ref={inputRef} type="file" multiple accept={ACCEPT_ATTR}
              onChange={onFileInput} className="hidden" />
          </div>

          {/* File list */}
          {files.length > 0 && (
            <div className="space-y-1">
              <Label className="text-xs text-muted-foreground">
                {t("upload.files", { count: files.length, defaultValue: `Files (${files.length})` })}
              </Label>
              <div className="max-h-48 overflow-y-auto overscroll-contain space-y-1 rounded border p-2">
                {files.map((f, i) => (
                  <div key={f.name} className="flex items-center gap-2 text-sm">
                    <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    <span className="truncate flex-1 font-mono text-xs">{f.name}</span>
                    <span className="shrink-0 text-xs text-muted-foreground">{formatBytes(f.size)}</span>
                    <button type="button" onClick={() => removeFile(i)}
                      className="shrink-0 p-0.5 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive">
                      <X className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={isPending}>
            {t("cancel")}
          </Button>
          <Button onClick={handleUpload} disabled={!canUpload}>
            {isPending ? t("upload.uploading", "Uploading...") : t("upload.upload", "Upload")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
