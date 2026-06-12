import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { useWebhookCallDetail } from "./hooks/use-webhooks";
import type { WebhookCallDetail } from "@/types/webhook";

const STATUS_VARIANT: Record<WebhookCallDetail["status"], "default" | "secondary" | "destructive" | "outline"> = {
  queued: "secondary",
  running: "outline",
  done: "default",
  failed: "destructive",
  dead: "destructive",
};

// Pretty-print a JSON string; fall back to the raw text if it isn't JSON.
function prettyJSON(s?: string): string {
  if (!s) return "";
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function fmt(iso?: string): string {
  return iso ? new Date(iso).toLocaleString() : "—";
}

interface Props {
  webhookId: string | null;
  callId: string | null;
  onClose: () => void;
}

export function WebhookCallDetailDialog({ webhookId, callId, onClose }: Props) {
  const { t } = useTranslation("webhooks");
  const { detail, loading } = useWebhookCallDetail(webhookId, callId);

  const Field = ({ label, value, mono }: { label: string; value?: string; mono?: boolean }) => (
    <div className="space-y-0.5">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className={`text-sm break-all ${mono ? "font-mono" : ""}`}>{value || "—"}</div>
    </div>
  );

  return (
    <Dialog open={!!callId} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("callDetail.title")}</DialogTitle>
          <DialogDescription>{t("callDetail.description")}</DialogDescription>
        </DialogHeader>

        {loading || !detail ? (
          <p className="py-8 text-center text-sm text-muted-foreground">{t("callDetail.loading")}</p>
        ) : (
          <div className="max-h-[70vh] overflow-y-auto space-y-4">
            <div className="grid grid-cols-2 gap-x-4 gap-y-3">
              <div className="space-y-0.5">
                <div className="text-xs text-muted-foreground">{t("calls.cols.status")}</div>
                <Badge variant={STATUS_VARIANT[detail.status]} className="text-xs">
                  {t(`calls.status.${detail.status}`)}
                </Badge>
              </div>
              <Field label={t("calls.cols.mode")} value={detail.mode} />
              <Field label={t("calls.cols.attempts")} value={String(detail.attempts)} />
              <Field label={t("callDetail.deliveryId")} value={detail.delivery_id} mono />
              <Field label={t("callDetail.idempotencyKey")} value={detail.idempotency_key} mono />
              <Field label={t("callDetail.callbackUrl")} value={detail.callback_url} mono />
              <Field label={t("callDetail.created")} value={fmt(detail.created_at)} />
              <Field label={t("callDetail.started")} value={fmt(detail.started_at)} />
              <Field label={t("callDetail.completed")} value={fmt(detail.completed_at)} />
              <Field label={t("callDetail.nextAttempt")} value={fmt(detail.next_attempt_at)} />
            </div>

            {detail.last_error && (
              <div className="space-y-1">
                <div className="text-xs text-muted-foreground">{t("callDetail.lastError")}</div>
                <div className="rounded bg-destructive/10 border border-destructive/40 px-3 py-2 text-sm text-destructive break-words">
                  {detail.last_error}
                </div>
              </div>
            )}

            {detail.request_payload && (
              <div className="space-y-1">
                <div className="text-xs text-muted-foreground">{t("callDetail.requestPayload")}</div>
                <pre className="max-h-48 overflow-auto rounded bg-muted px-3 py-2 text-xs whitespace-pre-wrap break-words">
                  {prettyJSON(detail.request_payload)}
                </pre>
              </div>
            )}

            {detail.response && (
              <div className="space-y-1">
                <div className="text-xs text-muted-foreground">{t("callDetail.response")}</div>
                <pre className="max-h-48 overflow-auto rounded bg-muted px-3 py-2 text-xs whitespace-pre-wrap break-words">
                  {prettyJSON(detail.response)}
                </pre>
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
