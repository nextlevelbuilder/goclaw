import { useState, useEffect, useCallback, useMemo } from "react";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { RefreshCw } from "lucide-react";
import { useTranslation } from "react-i18next";
import { FileBrowser } from "@/components/shared/file-browser";
import { buildTree, isTextFile } from "@/lib/file-helpers";
import { useTeamWorkspace } from "../hooks/use-team-workspace";
import { useHttp } from "@/hooks/use-ws";
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

  const scopeValue = selectedScope === "__all__" ? "" : selectedScope;

  const load = useCallback(() => {
    listFiles(teamId, scopeValue || undefined);
    setFileContent(null);
    setActivePath(null);
  }, [teamId, listFiles, scopeValue]);

  useEffect(() => {
    if (open) load();
  }, [open, load]);

  // Map relative name → absolute disk path (for HTTP file serving).
  const nameToAbsPath = useMemo(() => {
    const m = new Map<string, string>();
    for (const f of files) if (!f.is_dir && f.path) m.set(f.name, f.path);
    return m;
  }, [files]);

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

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="h-[90vh] w-[95vw] overflow-hidden sm:max-w-6xl flex flex-col">
        <DialogHeader className="shrink-0">
          <div className="flex items-center justify-between">
            <DialogTitle>{t("workspace.title")}</DialogTitle>
            <div className="flex items-center gap-2">
              {scopes.length > 0 && (
                <Select value={selectedScope} onValueChange={setSelectedScope}>
                  <SelectTrigger className="h-8 w-40 text-xs">
                    <SelectValue placeholder={t("scope.all")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__all__">{t("scope.all")}</SelectItem>
                    {scopes.map((s) => (
                      <SelectItem key={s.chat_id} value={s.chat_id}>
                        {s.chat_id.length > 16 ? s.chat_id.slice(0, 16) + "\u2026" : s.chat_id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              <Button variant="outline" size="sm" onClick={load} disabled={loading} className="gap-1">
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
          onDownload={handleDownload}
          fetchBlob={fetchBlobByName}
          showSize
        />
      </DialogContent>
    </Dialog>
  );
}
