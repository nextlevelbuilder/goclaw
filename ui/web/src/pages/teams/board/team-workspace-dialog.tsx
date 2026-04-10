import { useState, useEffect, useCallback, useMemo, lazy, Suspense } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { RefreshCw, Upload } from "lucide-react";
import { useTranslation } from "react-i18next";
import { FileBrowser } from "@/components/shared/file-browser";
import { buildTree, isTextFile } from "@/lib/file-helpers";

const FileUploadDialog = lazy(() =>
  import("@/components/shared/file-upload-dialog").then((m) => ({ default: m.FileUploadDialog }))
);
import { useTeamWorkspace } from "../hooks/use-team-workspace";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import type { ScopeEntry } from "@/types/team";

/** Strip chatID prefix from name for WS file ops (backend already scopes by chat_id). */
function wsFileName(name: string, chatID: string | undefined): string {
  if (chatID && name.startsWith(chatID + "/")) return name.slice(chatID.length + 1);
  return name;
}

interface TeamWorkspaceDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  teamId: string;
  scopes: ScopeEntry[];
}

export function TeamWorkspaceDialog({ open, onOpenChange, teamId, scopes }: TeamWorkspaceDialogProps) {
  const { t } = useTranslation("teams");
  const http = useHttp();
  const { files, loading, listFiles, readFile, deleteFile } = useTeamWorkspace();
  const [selectedScope, setSelectedScope] = useState<string>("__all__");
  const [fileContent, setFileContent] = useState<{ content: string; path: string; size: number } | null>(null);
  const [contentLoading, setContentLoading] = useState(false);
  const [activePath, setActivePath] = useState<string | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);

  const scopeValue = selectedScope === "__all__" ? "" : selectedScope;
  // Stable scope list: populated once from "all" listing, not recalculated on filter.
  const [cachedScopes, setCachedScopes] = useState<ScopeEntry[]>([]);

  // Load files for the selected scope. On "all" load, also cache scope list.
  // silent=true skips loading spinner (used after move to avoid tree flash).
  const load = useCallback((opts?: { silent?: boolean }) => {
    listFiles(teamId, scopeValue || undefined, opts).then((result) => {
      // Only update cached scopes from the "all" listing (no filter applied).
      if (!scopeValue && result.length > 0) {
        const seen = new Set<string>();
        const derived: ScopeEntry[] = [];
        for (const f of result) {
          if (f.chat_id && !seen.has(f.chat_id)) {
            seen.add(f.chat_id);
            derived.push({ channel: "", chat_id: f.chat_id });
          }
        }
        derived.sort((a, b) => a.chat_id.localeCompare(b.chat_id));
        setCachedScopes(derived);
      }
    });
    if (!opts?.silent) {
      setFileContent(null);
      setActivePath(null);
    }
  }, [teamId, listFiles, scopeValue]);

  useEffect(() => {
    if (open) {
      setSelectedScope("__all__"); // reset filter on open
      load();
    }
  }, [open]); // intentionally only depend on `open`, not `load`

  // Re-fetch when scope changes (but not on initial open — handled above).
  useEffect(() => {
    if (open && scopeValue) {
      listFiles(teamId, scopeValue);
      setFileContent(null);
      setActivePath(null);
    }
  }, [open, scopeValue, teamId, listFiles]);

  // Map relative name → absolute disk path (for HTTP file serving).
  const nameToAbsPath = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of files) if (!f.is_dir && f.path) m.set(f.name, f.path);
    return m;
  }, [files]);

  // Use provided scopes, or cached scopes from first "all" load.
  const effectiveScopes = scopes.length > 0 ? scopes : cachedScopes;

  const tree = useMemo(
    () => buildTree(files.map((f) => ({
      path: f.name,
      name: f.name.includes("/") ? f.name.split("/").pop()! : f.name,
      isDir: f.is_dir ?? false,
      size: f.size,
    }))),
    [files],
  );

  const handleSelect = useCallback(async (path: string) => {
    const match = files.find((f) => f.name === path);
    if (!match || match.is_dir) return;
    setActivePath(path);
    if (isTextFile(path)) {
      setContentLoading(true);
      try {
        const fname = wsFileName(match.name, match.chat_id);
        const res = await readFile(teamId, fname, match.chat_id || undefined);
        setFileContent({ content: res.content ?? "", path: match.name, size: match.size ?? 0 });
      } catch { setFileContent(null); }
      finally { setContentLoading(false); }
    } else {
      // Non-text files (images, binaries): set metadata only — ImageViewer/UnsupportedViewer handle display.
      setFileContent({ content: "", path: match.name, size: match.size ?? 0 });
    }
  }, [teamId, files, readFile]);

  const handleDelete = useCallback(async (path: string) => {
    const match = files.find((f) => f.name === path);
    if (!match) return;
    try {
      const fname = wsFileName(match.name, match.chat_id);
      await deleteFile(teamId, fname, match.chat_id || undefined);
      if (activePath === path) { setFileContent(null); setActivePath(null); }
      load();
    } catch { /* ignore */ }
  }, [teamId, files, deleteFile, activePath, load]);

  /** Fetch raw blob from HTTP file endpoint (correct MIME, no corruption). */
  const fetchBlobByName = useCallback(async (name: string): Promise<Blob> => {
    const absPath = nameToAbsPath.get(name);
    if (!absPath) throw new Error("file path not found");
    return http.fetchBlob("/v1/files" + absPath);
  }, [http, nameToAbsPath]);

  const handleDownload = useCallback(async (path: string) => {
    try {
      const blob = await fetchBlobByName(path);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = path.includes("/") ? path.split("/").pop()! : path;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch { /* silent */ }
  }, [fetchBlobByName]);

  const handleUploadFile = useCallback(async (file: File) => {
    const params: Record<string, string> = {};
    if (scopeValue) params["chat_id"] = scopeValue;
    await http.upload(`/v1/teams/${teamId}/workspace/upload?` + new URLSearchParams(params).toString(), (() => {
      const fd = new FormData();
      fd.append("file", file);
      return fd;
    })());
  }, [http, teamId, scopeValue]);

  const handleUploadClose = useCallback((v: boolean) => {
    setUploadOpen(v);
    if (!v) load(); // refresh file list after upload dialog closes
  }, [load]);

  const handleMove = useCallback(async (fromPath: string, toFolder: string) => {
    // In workspace, file names may have chatID prefix — strip it for the API call.
    const match = files.find((f) => f.name === fromPath);
    if (!match) return;

    // Prevent cross-scope moves (e.g. dragging from chat123/ to chat456/).
    if (match.chat_id && toFolder) {
      const toMatch = files.find((f) => f.name === toFolder && f.is_dir);
      if (toMatch && toMatch.chat_id && toMatch.chat_id !== match.chat_id) {
        toast.error(t("workspace.crossScopeMoveError"));
        return;
      }
    }

    const fromName = wsFileName(match.name, match.chat_id);
    const fileName = fromName.split("/").pop() ?? fromName;
    // toFolder may also have chatID prefix — strip it.
    const toClean = match.chat_id && toFolder.startsWith(match.chat_id + "/")
      ? toFolder.slice(match.chat_id.length + 1)
      : toFolder;
    const toName = toClean ? `${toClean}/${fileName}` : fileName;
    if (fromName === toName) return;

    const params = new URLSearchParams();
    params.set("from", fromName);
    params.set("to", toName);
    if (match.chat_id) params.set("chat_id", match.chat_id);

    try {
      await http.put(`/v1/teams/${teamId}/workspace/move?${params.toString()}`);
      load({ silent: true });
    } catch (err) {
      const msg = err instanceof Error ? err.message : t("workspace.moveFailed");
      toast.error(msg);
    }
  }, [http, teamId, files, load, t]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[90vh] w-[95vw] overflow-hidden sm:max-w-6xl flex flex-col">
        <DialogHeader className="shrink-0">
          <div className="flex items-center justify-between">
            <DialogTitle>{t("workspace.title")}</DialogTitle>
            <div className="flex items-center gap-2">
              {effectiveScopes.length > 1 && (
                <Select value={selectedScope} onValueChange={setSelectedScope}>
                  <SelectTrigger className="h-8 w-40 text-xs">
                    <SelectValue placeholder={t("scope.all")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__all__">{t("scope.all")}</SelectItem>
                    {effectiveScopes.map((s) => (
                      <SelectItem key={s.chat_id} value={s.chat_id}>
                        {s.chat_id.length > 16 ? s.chat_id.slice(0, 16) + "\u2026" : s.chat_id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Button variant="outline" size="sm" onClick={() => setUploadOpen(true)} className="gap-1">
                <Upload className="h-3.5 w-3.5" />
              </Button>
              <Button variant="outline" size="sm" onClick={() => load()} disabled={loading} className="gap-1">
                <RefreshCw className={"h-3.5 w-3.5" + (loading ? " animate-spin" : "")} />
              </Button>
            </div>
          </div>
        </DialogHeader>

        <FileBrowser
          tree={tree}
          filesLoading={loading}
          activePath={activePath}
          onSelect={handleSelect}
          contentLoading={contentLoading}
          fileContent={fileContent}
          onDelete={handleDelete}
          onMove={handleMove}
          onDownload={handleDownload}
          fetchBlob={fetchBlobByName}
          showSize
        />

        <Suspense fallback={null}>
          <FileUploadDialog
            open={uploadOpen}
            onOpenChange={handleUploadClose}
            onUpload={handleUploadFile}
            title={t("workspace.upload.title")}
            description={t("workspace.upload.description")}
          />
        </Suspense>
      </DialogContent>
    </Dialog>
  );
}
