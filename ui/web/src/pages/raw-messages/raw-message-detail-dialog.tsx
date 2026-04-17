import { useTranslation } from "react-i18next";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Copy, Check } from "lucide-react";
import { formatDate } from "@/lib/format";
import { useClipboard } from "@/hooks/use-clipboard";
import type { RawMessage } from "./hooks/use-raw-messages";

interface RawMessageDetailDialogProps {
  message: RawMessage;
  onClose: () => void;
}

export function RawMessageDetailDialog({ message, onClose }: RawMessageDetailDialogProps) {
  const { t } = useTranslation("raw-messages");
  const { copied, copy } = useClipboard();

  const fields: { label: string; value: React.ReactNode }[] = [
    { label: t("detail.agent"), value: <>{message.agent_name && <span className="font-medium">{message.agent_name}</span>}{message.agent_name ? " " : ""}<code className="text-xs text-muted-foreground">{message.agent_id}</code></> },
    { label: t("detail.group"), value: <>{message.chat_name && <span className="font-medium">{message.chat_name}</span>}{message.chat_name ? " " : ""}<code className="text-xs text-muted-foreground">{message.chat_id}</code></> },
    { label: t("detail.sender"), value: message.sender },
    { label: t("detail.senderId"), value: <code className="text-xs">{message.sender_id}</code> },
    { label: t("detail.graphId"), value: <code className="text-xs">{message.graph_id}</code> },
    { label: t("detail.channel"), value: message.channel_name },
    {
      label: t("detail.status"),
      value: message.processed_at
        ? <Badge variant="success" className="text-xs">{t("status.processed")}</Badge>
        : <Badge variant="secondary" className="text-xs">{t("status.pending")}</Badge>,
    },
    { label: t("detail.messageTime"), value: formatDate(message.msg_timestamp) },
    { label: t("detail.createdAt"), value: formatDate(message.created_at) },
    {
      label: t("detail.processedAt"),
      value: message.processed_at ? formatDate(message.processed_at) : t("detail.na"),
    },
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

          {/* Full message body */}
          <div className="mt-4 border-t pt-4">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-xs font-medium text-muted-foreground">{t("detail.messageBody")}</span>
              <Button
                variant="ghost"
                size="sm"
                className="h-7 gap-1 text-xs"
                onClick={() => copy(message.body)}
              >
                {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
                {copied ? t("detail.copied") : t("detail.copy")}
              </Button>
            </div>
            <div className="rounded-md bg-muted/50 p-3 text-sm whitespace-pre-wrap break-words max-h-[40vh] overflow-y-auto">
              {message.body || t("detail.na")}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
