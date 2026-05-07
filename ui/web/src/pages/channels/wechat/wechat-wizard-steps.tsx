// WeChat wizard step components for the channel create wizard.
// QR auth is driven by the iLink Bot API, delivered via WS events.
// Registered in channel-wizard-registry.tsx.

import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useWeChatQrLogin } from "./use-wechat-qr-login";
import type { WizardAuthStepProps } from "../channel-wizard-registry";

/** QR code authentication step for WeChat — displayed in create wizard after instance creation. */
export function WeChatAuthStep({ instanceId, onComplete, onSkip }: WizardAuthStepProps) {
  const { t } = useTranslation("channels");
  const { imgContent, status, errorMsg, loading, start, retry, reset } = useWeChatQrLogin(instanceId);

  // Auto-start QR on mount
  useEffect(() => {
    start();
    return () => reset();
  }, [start, reset]);

  // Signal completion to parent when QR confirms
  useEffect(() => {
    if (status === "done") onComplete();
  }, [status, onComplete]);

  return (
    <>
      <div className="flex flex-col items-center gap-4 py-4 min-h-0">
        {status === "done" && (
          <p className="text-sm text-green-600 font-medium">
            {t("wechat.loginSuccessLoading", "Login successful — loading...")}
          </p>
        )}
        {status === "error" && (
          <p className="text-sm text-destructive">{errorMsg}</p>
        )}
        {status === "waiting" && !imgContent && (
          <p className="text-sm text-muted-foreground">
            {t("wechat.generatingQr", "Generating QR code...")}
          </p>
        )}
        {status === "waiting" && imgContent && (
          <>
            <img
              src={`data:image/png;base64,${imgContent}`}
              alt="WeChat QR Code"
              className="w-52 h-52 border rounded"
            />
            <p className="text-xs text-muted-foreground text-center">
              {t("wechat.scanHint", "Scan with WeChat to log in")}
            </p>
          </>
        )}
        {status === "idle" && (
          <p className="text-sm text-muted-foreground">
            {t("wechat.initializing", "Initializing...")}
          </p>
        )}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onSkip} disabled={loading}>
          {t("wechat.skip", "Skip")}
        </Button>
        {status === "error" && (
          <Button onClick={() => retry()} disabled={loading}>
            {t("wechat.retry", "Retry")}
          </Button>
        )}
      </DialogFooter>
    </>
  );
}
