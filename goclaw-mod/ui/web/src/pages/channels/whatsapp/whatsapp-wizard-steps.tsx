// WhatsApp wizard step components for the channel create wizard.
// QR auth is driven directly by whatsmeow's GetQRChannel(), delivered via WS events.
// Registered in channel-wizard-registry.tsx.

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useWhatsAppQrLogin } from "./use-whatsapp-qr-login";
import type { WizardAuthStepProps } from "../channel-wizard-registry";

/** QR code authentication step for WhatsApp — displayed in create wizard after instance creation. */
export function WhatsAppAuthStep({ instanceId, onComplete, onSkip }: WizardAuthStepProps) {
  const { t } = useTranslation("channels");
  const { qrPng, status, errorMsg, loading, start, retry, reset } = useWhatsAppQrLogin(instanceId);

  // Auto-start QR on mount
  useEffect(() => {
    start();
    return () => reset();
  }, [start, reset]);

  // Signal completion to parent when bridge confirms connection
  useEffect(() => {
    if (status === "done") onComplete();
  }, [status, onComplete]);

  return (
    <>
      <div className="flex flex-col items-center gap-4 py-4 min-h-0">
        {status === "done" && (
          <p className="text-sm text-green-600 font-medium">
            {t("whatsapp.loginSuccessLoading")}
          </p>
        )}
        {status === "error" && (
          <p className="text-sm text-destructive">{errorMsg}</p>
        )}
        {status === "waiting" && !qrPng && (
          <p className="text-sm text-muted-foreground">
            {t("whatsapp.waitingForQr")}
          </p>
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
      <DialogFooter>
        <Button variant="outline" onClick={onSkip} disabled={loading}>
          {t("whatsapp.skip")}
        </Button>
        {status === "error" && (
          <Button onClick={() => retry()} disabled={loading}>
            {t("whatsapp.retry")}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}
