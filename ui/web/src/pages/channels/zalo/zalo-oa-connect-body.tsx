import { useTranslation } from "react-i18next";
import { Check, Copy, ExternalLink } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { UseZaloOAConnectResult } from "./use-zalo-oa-connect";

// Shared two-step body for the zalo_oa paste-code flow. Rendered inside
// either a Dialog (reauth) or the create-wizard step container. The caller
// provides the hook state via `flow` and renders the action row themselves
// (so wizard Skip/Connect buttons differ from reauth Cancel/Connect).

interface Props {
  flow: UseZaloOAConnectResult;
  disabled?: boolean; // wizard may disable while parent is busy
}

export function ZaloOAConnectBody({ flow, disabled }: Props) {
  const { t } = useTranslation("channels");
  const { url, code, setCode, copied, done, handleCopy, handleOpenInTab,
    submitting, loadingConsent, consentError, exchangeError, clientErrorKey } = flow;
  const clientError = clientErrorKey ? t(clientErrorKey) : null;

  const inputDisabled = submitting || done || disabled;

  return (
    <div className="flex flex-col gap-5 py-2">
      <section className="space-y-2">
        <h3 className="text-sm font-medium">{t("zaloOa.step1Heading")}</h3>
        <p className="text-xs text-muted-foreground">{t("zaloOa.authorizeHelp")}</p>
        {loadingConsent && (
          <p className="text-sm text-muted-foreground">{t("zaloOa.consentLoading")}</p>
        )}
        {consentError && (
          <p className="text-sm text-destructive">{consentError}</p>
        )}
        {url && (
          <div className="flex items-center gap-2">
            <Input value={url} readOnly className="text-base md:text-sm" />
            <Button type="button" variant="outline" size="sm" onClick={handleCopy} aria-label={t("zaloOa.copyUrl")}>
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={handleOpenInTab} aria-label={t("zaloOa.openInTab")}>
              <ExternalLink className="h-4 w-4" />
            </Button>
          </div>
        )}
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-medium">{t("zaloOa.step2Heading")}</h3>
        <p className="text-xs text-muted-foreground">{t("zaloOa.pasteHelp")}</p>
        <Input
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder={t("zaloOa.pastePlaceholder")}
          disabled={inputDisabled}
          autoFocus
        />
        {clientError && (
          <p className="text-sm text-destructive">{clientError}</p>
        )}
        {exchangeError && !clientError && (
          <p className="text-sm text-destructive">{exchangeError}</p>
        )}
        {done && (
          <p className="text-sm text-green-600 font-medium">{t("zaloOa.connectedClosing")}</p>
        )}
      </section>
    </div>
  );
}
