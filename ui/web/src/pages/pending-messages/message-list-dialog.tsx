import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/format";
import type { PendingMessageGroup, PendingMessage } from "./types";

interface MessageListDialogProps {
  group: PendingMessageGroup;
  messages: PendingMessage[];
  loading: boolean;
  onClose: () => void;
  onLoad: (channel: string, key: string) => void;
}

export function MessageListDialog({
  group,
  messages,
  loading,
  onClose,
  onLoad,
}: MessageListDialogProps) {
  const { t } = useTranslation("pending-messages");
  useEffect(() => {
    onLoad(group.channel_name, group.history_key);
  }, [group.channel_name, group.history_key, onLoad]);

  return (
    <Dialog open onOpenChange={() => onClose()}>
      <DialogContent className="max-h-[85vh] flex flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 truncate">
            {t("dialog.title", { name: group.group_title || group.history_key })}
            <Badge variant="outline" className="text-xs">{group.channel_name}</Badge>
          </DialogTitle>
        </DialogHeader>

        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            </div>
          ) : messages.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">{t("dialog.noMessages")}</p>
          ) : (
            <div className="space-y-3">
              {messages.map((msg) => (
                <div
                  key={msg.id}
                  className={
                    "rounded-md border p-3 text-sm" +
                    (msg.is_summary ? " border-amber-500/30 bg-amber-500/5" : "")
                  }
                >
                  <div className="mb-1.5 flex items-center gap-2">
                    <span className="font-medium">{msg.sender}</span>
                    {msg.is_summary && (
                      <Badge variant="warning" className="text-xs">{t("dialog.summary")}</Badge>
                    )}
                    <span className="ml-auto text-xs text-muted-foreground">
                      {formatDate(msg.created_at)}
                    </span>
                  </div>
                  <p className="whitespace-pre-wrap break-words text-sm">{msg.body}</p>
                </div>
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
