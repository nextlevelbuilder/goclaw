// Re-authentication dialog for WeChat — triggered from the channels table.
// QR code scan only.

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useWeChatQrLogin } from "./use-wechat-qr-login";
import type { ReauthDialogProps } from "../channel-wizard-registry";

export function WeChatReauthDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  onSuccess,
}: ReauthDialogProps) {
  const { t } = useTranslation("channels");
  const {
    imgContent, status, errorMsg, loading, start, reset, retry,
  } = useWeChatQrLogin(instanceId);

  // Auto-start QR when dialog opens
  useEffect(() => {
    if (open && status === "idle") {
      start();
    }
  }, [open, status, start]);

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) {
      reset();
    }
  }, [open, reset]);

  // Auto-close after successful scan
  useEffect(() => {
    if (status !== "done") return;
    onSuccess();
    const id = setTimeout(() => onOpenChange(false), 1500);
    return () => clearTimeout(id);
  }, [status, onSuccess, onOpenChange]);

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!loading) onOpenChange(v); }}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{t("wechat.reauthTitle", { name: instanceName })}</DialogTitle>
          <DialogDescription>
            {t("wechat.scanHint")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-4 min-h-[200px]">
          {status === "done" && (
            <p className="text-sm text-green-600 font-medium">{t("wechat.connectedSuccess")}</p>
          )}
          {status === "error" && (
            <p className="text-sm text-destructive">{errorMsg}</p>
          )}
          {status === "waiting" && !imgContent && (
            <p className="text-sm text-muted-foreground">{t("wechat.waitingForQr")}</p>
          )}
          {status === "waiting" && imgContent && (
            <>
              <img
                src={`data:image/png;base64,${imgContent}`}
                alt="WeChat QR Code"
                className="w-52 h-52 border rounded shadow-sm"
              />
              <p className="text-xs text-muted-foreground text-center">
                {t("wechat.scanHint")}
              </p>
            </>
          )}
          {status === "idle" && (
            <p className="text-sm text-muted-foreground">{t("wechat.initializing")}</p>
          )}
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("wechat.close")}</Button>
          {status === "error" && (
            <Button onClick={() => retry()} disabled={loading}>{t("wechat.retry")}</Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
