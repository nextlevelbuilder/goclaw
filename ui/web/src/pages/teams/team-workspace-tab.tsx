import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { RefreshCw, Trash2, FileText, Pin } from "lucide-react";
import { ConfirmDeleteDialog } from "@/components/shared/confirm-delete-dialog";
import { useTranslation } from "react-i18next";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useTeamWorkspace } from "./hooks/use-team-workspace";
import type { TeamWorkspaceFile, ScopeEntry } from "@/types/team";

interface TeamWorkspaceTabProps {
  teamId: string;
  scopes?: ScopeEntry[];
}

function formatBytes(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  if (bytes >= 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${bytes} B`;
}

function formatDate(dateStr?: string): string {
  if (!dateStr) return "-";
  const d = new Date(dateStr);
  return d.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

export function TeamWorkspaceTab({ teamId, scopes }: TeamWorkspaceTabProps) {
  const { t } = useTranslation("teams");
  const { files, loading, listFiles, readFile, deleteFile } =
    useTeamWorkspace();
  const [selectedFile, setSelectedFile] = useState<{
    file: TeamWorkspaceFile;
    content: string;
  } | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null);
  const [selectedScope, setSelectedScope] = useState<ScopeEntry | null>(null);
  const [initialized, setInitialized] = useState(false);
  const spinning = useMinLoading(loading);

  const load = useCallback(() => {
    listFiles(teamId, selectedScope?.channel, selectedScope?.chat_id).then(() => {
      setInitialized(true);
    });
  }, [teamId, listFiles, selectedScope]);

  useEffect(() => {
    load();
  }, [load]);

  const handleRowClick = useCallback(
    async (file: TeamWorkspaceFile) => {
      try {
        const res = await readFile(teamId, file.file_name, file.channel, file.chat_id);
        setSelectedFile({ file: res.file ?? file, content: res.content ?? "" });
      } catch {
        // ignore
      }
    },
    [teamId, readFile],
  );

  const handleDelete = useCallback(
    async (fileName: string) => {
      const file = files.find((f) => f.file_name === fileName);
      try {
        await deleteFile(teamId, fileName, file?.channel, file?.chat_id);
        setDeleteTarget(null);
        load();
      } catch {
        // ignore
      }
    },
    [teamId, files, deleteFile, load],
  );

  if (!initialized) {
    return (
      <div className="py-8 text-center text-sm text-muted-foreground">
        Loading...
      </div>
    );
  }

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium">{t("workspace.title")}</h3>
          {scopes && scopes.length > 0 && (
            <select
              value={selectedScope ? `${selectedScope.channel}:${selectedScope.chat_id}` : ""}
              onChange={(e) => {
                if (!e.target.value) {
                  setSelectedScope(null);
                } else {
                  const idx = e.target.value.indexOf(":");
                  setSelectedScope({ channel: e.target.value.slice(0, idx), chat_id: e.target.value.slice(idx + 1) });
                }
              }}
              className="rounded-md border bg-background px-2 py-1 text-base md:text-sm"
            >
              <option value="">{t("scope.all")}</option>
              {scopes.map((s) => (
                <option key={`${s.channel}:${s.chat_id}`} value={`${s.channel}:${s.chat_id}`}>
                  {s.channel}:{s.chat_id.length > 12 ? s.chat_id.slice(0, 12) + "…" : s.chat_id}
                </option>
              ))}
            </select>
          )}
        </div>
        <Button variant="ghost" size="sm" onClick={load} disabled={spinning}>
          <RefreshCw className={`mr-1.5 h-3.5 w-3.5 ${spinning ? "animate-spin" : ""}`} />
          {t("workspace.refresh")}
        </Button>
      </div>

      {files.length === 0 ? (
        <div className="py-12 text-center">
          <FileText className="mx-auto mb-3 h-10 w-10 text-muted-foreground/30" />
          <p className="text-sm font-medium text-muted-foreground">
            {t("workspace.noFiles")}
          </p>
          <p className="mt-1 text-xs text-muted-foreground/70">
            {t("workspace.noFilesDescription")}
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full min-w-[600px] text-sm">
            <thead>
              <tr className="border-b text-left text-muted-foreground">
                <th className="pb-2 font-medium">{t("workspace.columns.fileName")}</th>
                <th className="pb-2 font-medium">{t("workspace.columns.size")}</th>
                <th className="pb-2 font-medium">{t("workspace.columns.uploadedBy")}</th>
                <th className="pb-2 font-medium">{t("workspace.columns.scope")}</th>
                <th className="pb-2 font-medium">{t("workspace.columns.updated")}</th>
                <th className="pb-2 font-medium" />
              </tr>
            </thead>
            <tbody>
              {files.map((file) => (
                <tr
                  key={file.id}
                  className="cursor-pointer border-b border-border/50 transition-colors hover:bg-muted/50"
                  onClick={() => handleRowClick(file)}
                >
                  <td className="py-2.5 pr-3">
                    <div className="flex items-center gap-1.5">
                      {file.pinned && <Pin className="h-3 w-3 text-amber-500" />}
                      <span className="font-mono text-xs">{file.file_name}</span>
                      {file.missing && (
                        <Badge variant="destructive" className="text-[10px]">
                          missing
                        </Badge>
                      )}
                      {file.tags?.map((tag) => (
                        <Badge
                          key={tag}
                          variant={tag === "deliverable" ? "success" : "secondary"}
                          className="text-[10px]"
                        >
                          {tag}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td className="py-2.5 pr-3 text-muted-foreground">
                    {formatBytes(file.size_bytes)}
                  </td>
                  <td className="py-2.5 pr-3 text-muted-foreground">
                    {file.uploaded_by_key || file.uploaded_by?.slice(0, 8)}
                  </td>
                  <td className="py-2.5 pr-3 text-muted-foreground">
                    {file.channel || "default"}:{file.chat_id?.slice(0, 12) || "default"}
                  </td>
                  <td className="py-2.5 pr-3 text-muted-foreground">
                    {formatDate(file.updated_at)}
                  </td>
                  <td className="py-2.5">
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-7 w-7 text-muted-foreground hover:text-destructive"
                      onClick={(e) => {
                        e.stopPropagation();
                        setDeleteTarget(file.file_name);
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* File content dialog */}
      <Dialog open={!!selectedFile} onOpenChange={() => setSelectedFile(null)}>
        <DialogContent className="max-h-[80vh] max-w-2xl overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">
              {selectedFile?.file.file_name}
            </DialogTitle>
          </DialogHeader>
          <pre className="mt-2 max-h-[60vh] overflow-auto rounded-md bg-muted p-4 text-xs">
            {selectedFile?.content || t("workspace.detail.empty")}
          </pre>
        </DialogContent>
      </Dialog>

      {/* Delete confirmation */}
      <ConfirmDeleteDialog
        open={!!deleteTarget}
        onOpenChange={() => setDeleteTarget(null)}
        title={t("workspace.delete")}
        description={t("workspace.confirmDelete")}
        confirmValue={deleteTarget ?? ""}
        onConfirm={() => deleteTarget && handleDelete(deleteTarget)}
      />
    </div>
  );
}
