// Re-authentication dialog for WhatsApp — triggered from the channels table.
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
import { useWhatsAppQrLogin } from "./use-whatsapp-qr-login";
import type { ReauthDialogProps } from "../channel-wizard-registry";

export function WhatsAppReauthDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  onSuccess,
}: ReauthDialogProps) {
  const { t } = useTranslation("channels");
  const {
    qrPng, status, errorMsg, loading, start, reset, retry, triggerReauth,
  } = useWhatsAppQrLogin(instanceId);

  // Auto-start QR when dialog opens; intentionally omits `start` from deps
  // because we only want to trigger on open/close transitions, not on identity changes.
  useEffect(() => {
    if (open && status === "idle") start();
  }, [open]);  

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) reset();
  }, [open, reset]);

  // Auto-close after a fresh QR scan completes (not "already connected")
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
          <DialogTitle>{t("whatsapp.reauthTitle", { name: instanceName })}</DialogTitle>
          <DialogDescription>
            {t("whatsapp.scanHint")}
          </DialogDescription>
        </DialogHeader>

        {/* Already connected state */}
        {status === "connected" && (
          <div className="flex flex-col items-center gap-4 py-2">
            <div className="text-center space-y-2">
              <p className="text-sm text-green-600 font-medium">{t("whatsapp.alreadyLinked")}</p>
              <p className="text-xs text-muted-foreground">
                {t("whatsapp.alreadyLinkedDetail")}
              </p>
            </div>
            <div className="flex justify-end gap-2 w-full">
              <Button variant="outline" onClick={() => onOpenChange(false)}>{t("whatsapp.close")}</Button>
              <Button variant="destructive" onClick={triggerReauth} disabled={loading}>{t("whatsapp.relinkDevice")}</Button>
            </div>
          </div>
        )}

        {/* Done state */}
        {status === "done" && (
          <div className="flex flex-col items-center gap-4 py-4">
            <p className="text-sm text-green-600 font-medium">{t("whatsapp.connectedSuccess")}</p>
          </div>
        )}

        {/* QR scan flow */}
        {status !== "connected" && status !== "done" && (
          <>
            <div className="flex flex-col items-center gap-4 py-4 min-h-[200px]">
              {status === "error" && (
                <p className="text-sm text-destructive">{errorMsg}</p>
              )}
              {status === "waiting" && !qrPng && (
                <p className="text-sm text-muted-foreground">{t("whatsapp.waitingForQr")}</p>
              )}
              {status === "waiting" && qrPng && (
                <>
                  <img
                    src={`data:image/png;base64,${qrPng}`}
                    alt="WhatsApp QR Code"
                    className="w-52 h-52 border rounded"
                  />
                  <p className="text-xs text-muted-foreground text-center">
                    {t("whatsapp.scanHint")}
                  </p>
                </>
              )}
              {status === "idle" && (
                <p className="text-sm text-muted-foreground">{t("whatsapp.initializing")}</p>
              )}
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => onOpenChange(false)}>{t("whatsapp.close")}</Button>
              {status === "error" && (
                <Button onClick={() => retry()} disabled={loading}>{t("whatsapp.retry")}</Button>
              )}
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
