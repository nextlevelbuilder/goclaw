// Zalo Personal wizard step components for the channel create wizard.
// Registered in channel-wizard-registry.tsx.

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useZaloQrLogin } from "./use-zalo-qr-login";
import { ZaloContactsPicker } from "./zalo-contacts-picker";
import type { WizardAuthStepProps, WizardConfigStepProps, WizardEditConfigProps } from "../channel-wizard-registry";

/** QR code authentication step for Zalo Personal */
export function ZaloAuthStep({ instanceId, onComplete, onSkip }: WizardAuthStepProps) {
  const { t } = useTranslation("channels");
  const { qrPng, status, errorMsg, loading, start, retry, reset } = useZaloQrLogin(instanceId);

  // Auto-start QR on mount
  useEffect(() => {
    start();
    return () => reset();
  }, [start, reset]);

  // Signal completion to parent
  useEffect(() => {
    if (status === "done") onComplete();
  }, [status, onComplete]);

  return (
    <>
      <div className="flex flex-col items-center gap-4 py-4 min-h-0">
        {status === "done" && <p className="text-sm text-green-600 font-medium">{t("zalo.loginSuccessLoading")}</p>}
        {status === "error" && <p className="text-sm text-destructive">{errorMsg}</p>}
        {qrPng && status === "waiting" && <img src={`data:image/png;base64,${qrPng}`} alt="Zalo QR Code" className="w-48 h-48 border rounded" />}
        {status === "waiting" && !qrPng && <p className="text-sm text-muted-foreground">{t("zalo.generatingQr")}</p>}
        {status === "waiting" && qrPng && <p className="text-xs text-muted-foreground">{t("zalo.scanHint")}</p>}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onSkip} disabled={loading}>{t("zalo.skip")}</Button>
        {status === "error" && <Button onClick={retry} disabled={loading}>{t("zalo.retry")}</Button>}
      </DialogFooter>
    </>
  );
}

/** Contacts picker step for configuring allowed users */
export function ZaloConfigStep({ instanceId, authCompleted, configValues, onConfigChange }: WizardConfigStepProps) {
  return (
    <ZaloContactsPicker
      instanceId={instanceId}
      hasCredentials={authCompleted}
      value={(configValues.allow_from as string[]) ?? []}
      onChange={(ids) => onConfigChange("allow_from", ids.length > 0 ? ids : undefined)}
    />
  );
}

/** Inline contacts picker widget for edit mode */
export function ZaloEditConfig({ instance, configValues, onConfigChange }: WizardEditConfigProps) {
  return (
    <ZaloContactsPicker
      instanceId={instance.id}
      hasCredentials={instance.has_credentials}
      value={(configValues.allow_from as string[]) ?? []}
      onChange={(ids) => onConfigChange("allow_from", ids.length > 0 ? ids : undefined)}
    />
  );
}
