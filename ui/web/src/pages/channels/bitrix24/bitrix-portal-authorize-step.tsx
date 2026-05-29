import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ExternalLink, CheckCircle2, Clock, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { DialogFooter } from "@/components/ui/dialog";
import { useBitrixPortals } from "./use-bitrix-portals";

const POLL_INTERVAL_MS = 3000;
const TIMEOUT_MS = 5 * 60 * 1000; // 5 min — long enough for OAuth + 2FA prompts

interface BitrixPortalAuthorizeStepProps {
  portalName: string;
  installUrl: string;
  /** Warning surfaced when create succeeded but install_url is empty (gateway
   *  hadn't observed its own public URL yet). */
  warning?: string;
  /** Fires when polling detects installed=true. Parent closes modal. */
  onAuthorized: () => void;
  /** Cancel button — modal closes, portal pending row stays for resume later. */
  onCancel: () => void;
}

// Step 2 of the create modal: send the admin to Bitrix24 to authorize the
// app, then poll bitrix.portals.list every 3s for the freshly-installed
// state. On detection, fire onAuthorized so the parent can auto-select the
// portal in the channel form.
//
// Polling instead of WebSocket push because (a) install completes server-side
// in the gateway's install-handler, which is a separate goroutine from the
// admin's WS connection — no natural event to subscribe to without adding
// new plumbing; (b) 3s is fast enough that operators won't notice the delay.
export function BitrixPortalAuthorizeStep({
  portalName,
  installUrl,
  warning,
  onAuthorized,
  onCancel,
}: BitrixPortalAuthorizeStepProps) {
  const { t } = useTranslation("channels");
  const [timedOut, setTimedOut] = useState(false);
  const authorizedFired = useRef(false);

  const { data: portals = [] } = useBitrixPortals({ pollInterval: POLL_INTERVAL_MS });
  const portal = portals.find((p) => p.name === portalName);
  const installed = !!portal?.installed;

  // Fire onAuthorized exactly once — protect against double-dispatch when
  // react-query refetches before the parent unmounts.
  useEffect(() => {
    if (installed && !authorizedFired.current) {
      authorizedFired.current = true;
      onAuthorized();
    }
  }, [installed, onAuthorized]);

  // Soft timeout: surfaces "you can resume later" message but keeps the
  // modal open so admin can click the button again if they were slow.
  useEffect(() => {
    const id = setTimeout(() => setTimedOut(true), TIMEOUT_MS);
    return () => clearTimeout(id);
  }, []);

  // Polling-status indicator. Keeps the visual feedback explicit because
  // OAuth round-trips happen in another tab — without this, the admin can't
  // tell whether install completed or polling broke.
  const statusBlock = installed ? (
    <div className="flex items-center gap-2 rounded-md border border-green-200 bg-green-50 p-3 text-sm dark:border-green-900 dark:bg-green-950">
      <CheckCircle2 className="h-4 w-4 text-green-600" />
      <span>{t("bitrix24.create.authorize.installed", { defaultValue: "Portal installed successfully!" })}</span>
    </div>
  ) : timedOut ? (
    <div className="flex items-center gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm dark:border-amber-900 dark:bg-amber-950">
      <AlertCircle className="h-4 w-4 text-amber-600" />
      <span>
        {t("bitrix24.create.authorize.timedOut", {
          defaultValue: "Authorization not completed yet. You can resume from the dropdown.",
        })}
      </span>
    </div>
  ) : (
    <div className="flex items-center gap-2 rounded-md border bg-muted/30 p-3 text-sm text-muted-foreground">
      <Clock className="h-4 w-4 animate-pulse" />
      <span>{t("bitrix24.create.authorize.waiting", { defaultValue: "Waiting for authorization..." })}</span>
    </div>
  );

  return (
    <div className="grid gap-4">
      <div>
        <p className="text-sm font-medium">{portalName}</p>
        {portal?.domain && (
          <p className="text-xs text-muted-foreground">{portal.domain}</p>
        )}
      </div>

      {warning && (
        <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-xs dark:border-amber-900 dark:bg-amber-950">
          ⚠ {warning}
        </div>
      )}

      {installUrl ? (
        <Button
          type="button"
          variant="default"
          onClick={() => window.open(installUrl, "_blank", "noopener")}
          className="w-full"
        >
          <ExternalLink className="mr-2 h-4 w-4" />
          {t("bitrix24.create.authorize.openButton", { defaultValue: "Open authorization page" })}
        </Button>
      ) : (
        <p className="text-sm text-destructive">
          {t("bitrix24.create.authorize.urlUnknown", {
            defaultValue: "Install URL is unavailable. Open the goclaw UI via your public URL and retry from the dropdown.",
          })}
        </p>
      )}

      {statusBlock}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          {timedOut
            ? t("bitrix24.create.authorize.closeResumeLater", { defaultValue: "Close (resume later)" })
            : t("common.cancel", { defaultValue: "Cancel" })}
        </Button>
      </DialogFooter>
    </div>
  );
}
