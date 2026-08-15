import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw, ChevronLeft, ChevronRight } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { EmptyState } from "@/components/shared/empty-state";
import { Inbox } from "lucide-react";
import { formatRelativeTime } from "@/lib/format";
import { useWebhookCalls } from "./hooks/use-webhooks";
import { WebhookCallDetailDialog } from "./webhook-call-detail-dialog";
import type { WebhookData, WebhookCallData } from "@/types/webhook";

const STATUS_VARIANT: Record<WebhookCallData["status"], "default" | "secondary" | "destructive" | "outline"> = {
  queued: "secondary",
  running: "outline",
  done: "default",
  failed: "destructive",
  dead: "destructive",
};

const ALL = "__all__";
const PAGE_SIZE = 20;

interface Props {
  webhook: WebhookData | null;
  onClose: () => void;
}

export function WebhookCallsDialog({ webhook, onClose }: Props) {
  const { t } = useTranslation("webhooks");
  const [status, setStatus] = useState<string>(ALL);
  const [page, setPage] = useState(0);
  const [detailCallId, setDetailCallId] = useState<string | null>(null);
  const effectiveStatus = status === ALL ? "" : status;
  const { calls, total, isFetching, refetch } = useWebhookCalls(
    webhook?.id ?? null,
    effectiveStatus,
    !!webhook,
    PAGE_SIZE,
    page * PAGE_SIZE,
  );

  // Reset to first page when the target webhook or status filter changes.
  useEffect(() => {
    setPage(0);
  }, [webhook?.id, status]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const hasNext = (page + 1) * PAGE_SIZE < total;
  const hasPrev = page > 0;

  return (
    <>
    <Dialog open={!!webhook} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t("calls.title", { name: webhook?.name ?? "" })}</DialogTitle>
          <DialogDescription>{t("calls.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Select value={status} onValueChange={setStatus}>
            <SelectTrigger className="w-40 text-base md:text-sm">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>{t("calls.statusAll")}</SelectItem>
              <SelectItem value="queued">{t("calls.status.queued")}</SelectItem>
              <SelectItem value="running">{t("calls.status.running")}</SelectItem>
              <SelectItem value="done">{t("calls.status.done")}</SelectItem>
              <SelectItem value="failed">{t("calls.status.failed")}</SelectItem>
              <SelectItem value="dead">{t("calls.status.dead")}</SelectItem>
            </SelectContent>
          </Select>
          <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching} className="gap-1">
            <RefreshCw className={isFetching ? "animate-spin h-3.5 w-3.5" : "h-3.5 w-3.5"} />
          </Button>
        </div>

        <div className="mt-2 max-h-[60vh] overflow-y-auto">
          {calls.length === 0 ? (
            page === 0 ? (
              <EmptyState icon={Inbox} title={t("calls.emptyTitle")} description={t("calls.emptyDescription")} />
            ) : (
              <p className="py-8 text-center text-sm text-muted-foreground">{t("calls.noMore")}</p>
            )
          ) : (
            <div className="overflow-x-auto rounded-md border">
              <table className="w-full min-w-[600px] text-sm">
                <thead>
                  <tr className="border-b bg-muted/50">
                    <th className="px-3 py-2 text-left font-medium">{t("calls.cols.status")}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("calls.cols.mode")}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("calls.cols.attempts")}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("calls.cols.created")}</th>
                    <th className="px-3 py-2 text-left font-medium">{t("calls.cols.detail")}</th>
                  </tr>
                </thead>
                <tbody>
                  {calls.map((c) => (
                    <tr
                      key={c.id}
                      onClick={() => setDetailCallId(c.id)}
                      className="border-b last:border-0 align-top hover:bg-muted/30 cursor-pointer"
                    >
                      <td className="px-3 py-2">
                        <Badge variant={STATUS_VARIANT[c.status]} className="text-xs">
                          {t(`calls.status.${c.status}`)}
                        </Badge>
                      </td>
                      <td className="px-3 py-2 text-muted-foreground">{c.mode}</td>
                      <td className="px-3 py-2 text-muted-foreground">{c.attempts}</td>
                      <td className="px-3 py-2 text-muted-foreground" title={new Date(c.created_at).toLocaleString()}>
                        {formatRelativeTime(c.created_at)}
                      </td>
                      <td className="px-3 py-2 text-xs text-muted-foreground max-w-[260px]">
                        {c.last_error ? (
                          <span className="text-destructive break-words">{c.last_error}</span>
                        ) : c.response ? (
                          <span className="break-words line-clamp-3">{c.response}</span>
                        ) : (
                          "—"
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {(hasPrev || hasNext) && (
          <div className="flex items-center justify-between border-t pt-3">
            <span className="text-xs text-muted-foreground">{t("calls.pageOf", { page: page + 1, total: totalPages })}</span>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={!hasPrev || isFetching}
                className="gap-1"
              >
                <ChevronLeft className="h-3.5 w-3.5" /> {t("calls.prev")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPage((p) => p + 1)}
                disabled={!hasNext || isFetching}
                className="gap-1"
              >
                {t("calls.next")} <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>
        )}
      </DialogContent>
    </Dialog>

    <WebhookCallDetailDialog
      webhookId={webhook?.id ?? null}
      callId={detailCallId}
      onClose={() => setDetailCallId(null)}
    />
    </>
  );
}
