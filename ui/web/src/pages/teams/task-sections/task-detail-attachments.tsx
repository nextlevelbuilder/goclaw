import {
  Paperclip, Image, FileText, FileCode, FileJson2,
  Video, Music, Archive, File, Download,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { useTranslation } from "react-i18next";
import { formatFileSize } from "@/lib/format";
import { CollapsibleSection } from "./task-detail-content";
import type { TeamTaskAttachment } from "@/types/team";
import type { LucideIcon } from "lucide-react";

/* ── Mime-type icon mapping ───────────────────────────────────── */

function getMimeIcon(mime?: string, path?: string): LucideIcon {
  if (!mime && path) {
    const ext = path.split(".").pop()?.toLowerCase();
    if (ext && ["png", "jpg", "jpeg", "gif", "webp", "svg", "bmp"].includes(ext)) return Image;
    if (ext === "pdf") return FileText;
    if (ext === "json") return FileJson2;
    if (ext && ["zip", "tar", "gz", "rar", "7z"].includes(ext)) return Archive;
    if (ext && ["mp4", "webm", "mov", "avi"].includes(ext)) return Video;
    if (ext && ["mp3", "wav", "ogg", "flac"].includes(ext)) return Music;
  }
  if (mime?.startsWith("image/")) return Image;
  if (mime === "application/pdf") return FileText;
  if (mime?.startsWith("text/")) return FileCode;
  if (mime?.includes("json")) return FileJson2;
  if (mime?.startsWith("video/")) return Video;
  if (mime?.startsWith("audio/")) return Music;
  if (mime?.includes("zip") || mime?.includes("tar") || mime?.includes("gzip") || mime?.includes("archive")) return Archive;
  return File;
}

/* ── Attachment list ──────────────────────────────────────────── */

interface TaskDetailAttachmentsProps {
  attachments: TeamTaskAttachment[];
}

export function TaskDetailAttachments({ attachments }: TaskDetailAttachmentsProps) {
  const { t } = useTranslation("teams");

  if (attachments.length === 0) return null;

  return (
    <CollapsibleSection icon={Paperclip} title={t("tasks.detail.attachments")} count={attachments.length} defaultOpen={false}>
      <div className="space-y-2">
        {attachments.map((a) => {
          const Icon = getMimeIcon(a.mime_type, a.path);
          const fileName = a.path?.split("/").pop() || a.path || "file";
          return (
            <div key={a.id} className="flex items-center gap-3 rounded-lg border bg-card p-3">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10">
                <Icon className="h-5 w-5 text-primary" />
              </div>
              <div className="flex-1 min-w-0">
                <p className="truncate text-sm font-medium">{fileName}</p>
                {a.file_size > 0 && (
                  <p className="text-xs text-muted-foreground">{formatFileSize(a.file_size)}</p>
                )}
              </div>
              {a.download_url && (
                <Button asChild variant="outline" size="sm" className="shrink-0">
                  <a href={a.download_url} download onClick={(e) => e.stopPropagation()}>
                    <Download className="mr-1.5 h-3.5 w-3.5" />
                    {t("tasks.detail.download")}
                  </a>
                </Button>
              )}
            </div>
          );
        })}
      </div>
    </CollapsibleSection>
  );
}
