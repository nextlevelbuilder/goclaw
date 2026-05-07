import { useTranslation } from "react-i18next";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Copy, Check, Trash2 } from "lucide-react";
import { formatDate } from "@/lib/format";
import { useClipboard } from "@/hooks/use-clipboard";
import type { EmbeddingChunk } from "./hooks/use-embeddings";

interface EmbeddingDetailDialogProps {
  chunk: EmbeddingChunk;
  onClose: () => void;
  onDelete?: () => void;
}

export function EmbeddingDetailDialog({ chunk, onClose, onDelete }: EmbeddingDetailDialogProps) {
  const { t } = useTranslation("embeddings");
  const { copied, copy } = useClipboard();

  const fields: { label: string; value: React.ReactNode }[] = [
    { label: t("detail.agent"), value: <code className="text-xs text-muted-foreground">{chunk.agent_id}</code> },
    { label: t("detail.graphId"), value: <code className="text-xs">{chunk.graph_id}</code> },
    { label: t("detail.chat"), value: <>{chunk.chat_name && <span className="font-medium">{chunk.chat_name}</span>}{chunk.chat_name ? " " : ""}<code className="text-xs text-muted-foreground">{chunk.chat_id}</code></> },
    { label: t("detail.sender"), value: <>{chunk.sender} {chunk.sender_id && <code className="text-xs text-muted-foreground">{chunk.sender_id}</code>}</> },
    { label: t("detail.chunkIndex"), value: String(chunk.chunk_index) },
    {
      label: t("detail.embeddingStatus"),
      value: chunk.has_embedding
        ? <Badge variant="success" className="text-xs">{t("status.embedded")}</Badge>
        : <Badge variant="secondary" className="text-xs">{t("status.noVector")}</Badge>,
    },
    { label: t("detail.contentHash"), value: <code className="text-xs break-all">{chunk.content_hash}</code> },
    { label: t("detail.msgTimeRange"), value: `${formatDate(chunk.msg_time_from)} — ${formatDate(chunk.msg_time_to)}` },
    { label: t("detail.sourceMsgIds"), value: chunk.source_msg_ids?.length ? <code className="text-xs">{chunk.source_msg_ids.join(", ")}</code> : t("detail.na") },
    { label: t("detail.createdAt"), value: formatDate(chunk.created_at) },
  ];

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("detail.title")}</DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {/* Metadata grid */}
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-x-6 gap-y-3 text-sm">
            {fields.map((f) => (
              <div key={f.label} className="min-w-0">
                <div className="text-xs text-muted-foreground">{f.label}</div>
                <div className="mt-0.5 truncate">{f.value}</div>
              </div>
            ))}
          </div>

          {/* Full chunk text */}
          <div className="mt-4 border-t pt-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">{t("detail.chunkText")}</span>
              <div className="flex items-center gap-1">
                {onDelete && (
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-7 gap-1 text-xs text-destructive hover:text-destructive"
                    onClick={onDelete}
                  >
                    <Trash2 className="h-3 w-3" />
                    {t("detail.deleteChunk")}
                  </Button>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 gap-1 text-xs"
                  onClick={() => copy(chunk.text)}
                >
                  {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                  {copied ? t("detail.copied") : t("detail.copy")}
                </Button>
              </div>
            </div>
            <div className="rounded-md bg-muted/50 p-3 text-sm whitespace-pre-wrap break-words max-h-[40vh] overflow-y-auto">
              {chunk.text || t("detail.na")}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
