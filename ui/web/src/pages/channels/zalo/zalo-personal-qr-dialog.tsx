import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useZaloQrLogin } from "./use-zalo-qr-login";

interface ZaloPersonalQRDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  instanceId: string;
  instanceName: string;
  onSuccess: () => void;
}

export function ZaloPersonalQRDialog({
  open,
  onOpenChange,
  instanceId,
  instanceName,
  onSuccess,
}: ZaloPersonalQRDialogProps) {
  const { t } = useTranslation("channels");
  const { qrPng, status, errorMsg, loading, start, reset } = useZaloQrLogin(instanceId);

  // Auto-start when dialog opens
  useEffect(() => {
    if (open && status === "idle") start();
  }, [open]);  

  // Reset state when dialog closes
  useEffect(() => {
    if (!open) reset();
  }, [open, reset]);

  // Auto-close on success
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
          <DialogTitle>{t("zalo.loginQr", { name: instanceName })}</DialogTitle>
        </DialogHeader>

        <div className="flex flex-col items-center gap-4 py-4">
          {status === "done" && (
            <p className="text-sm text-green-600 font-medium">{t("zalo.loginSuccessful")}</p>
          )}
          {status === "error" && (
            <p className="text-sm text-destructive">{errorMsg}</p>
          )}
          {qrPng && status === "waiting" && (
            <img
              src={`data:image/png;base64,${qrPng}`}
              alt="Zalo QR Code"
              className="w-48 h-48 border rounded"
            />
          )}
          {status === "waiting" && !qrPng && (
            <p className="text-sm text-muted-foreground">{t("zalo.generatingQr")}</p>
          )}
          {status === "waiting" && qrPng && (
            <p className="text-xs text-muted-foreground">{t("zalo.scanHint")}</p>
          )}
        </div>

        <div className="flex justify-end gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("zalo.close")}</Button>
          {status === "error" && (
            <Button onClick={start} disabled={loading}>{t("zalo.retry")}</Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
