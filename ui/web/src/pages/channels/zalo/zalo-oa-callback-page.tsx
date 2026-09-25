import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, Copy, ShieldAlert } from "lucide-react";
import { Button } from "@/components/ui/button";

// ZaloOACallbackPage is the static landing page for the OA OAuth redirect.
// Zalo sends the browser here after consent:
//
//   /oauth/zalo/callback?oa_id=...&code=...&state=...
//
// The SPA catch-all would otherwise rewrite this URL to the overview root
// and lose the query string. This route matches the exact path, does NOT
// redirect, and shows the code for copy-paste. When opened as a popup
// (window.opener set), it signals the parent via postMessage and closes
// itself so the consent dialog auto-completes.
export function ZaloOACallbackPage() {
  const { t } = useTranslation("channels");
  const [copied, setCopied] = useState(false);
  const { fullURL, oaID, hasCode } = useMemo(() => {
    const href = window.location.href;
    let code = "";
    let oaID = "";
    try {
      const u = new URL(href);
      code = (u.searchParams.get("code") ?? "").trim();
      oaID = (u.searchParams.get("oa_id") ?? "").trim();
    } catch {
      // non-URL href; keep empties
    }
    return { fullURL: href, oaID, hasCode: code !== "" };
  }, []);

  // Popup mode: hand the full href to the opener, then close.
  // Must run in an effect — render-time window.close() double-fires under StrictMode.
  useEffect(() => {
    if (!hasCode || !window.opener) return;
    try {
      window.opener.postMessage(
        { type: "zalo-oa-callback", href: fullURL },
        window.location.origin,
      );
      window.close();
    } catch {
      // opener gone or cross-origin — fall through to the manual UI
    }
  }, [hasCode, fullURL]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(fullURL);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable
    }
  };

  return (
    <div className="flex min-h-dvh items-center justify-center bg-background p-4">
      <div className="w-full max-w-lg space-y-4 rounded-lg border p-6">
        <h1 className="text-lg font-semibold">
          {hasCode ? t("zaloOa.callback.title") : t("zaloOa.callback.errorTitle")}
        </h1>

        {hasCode ? (
          <>
            <p className="text-sm text-muted-foreground">{t("zaloOa.callback.successHint")}</p>
            {oaID && (
              <p className="text-sm">
                {t("zaloOa.callback.oaId")}: <span className="font-mono">{oaID}</span>
              </p>
            )}
            <div className="flex items-center gap-2">
              <input
                readOnly
                value={fullURL}
                className="w-full rounded-md border bg-muted px-3 py-2 font-mono text-base md:text-sm"
                onFocus={(e) => e.currentTarget.select()}
              />
              <Button type="button" variant="outline" size="sm" onClick={handleCopy}>
                {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {t("zaloOa.callback.copy")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{t("zaloOa.callback.manualHint")}</p>
          </>
        ) : (
          <p className="flex items-center gap-2 text-sm text-destructive">
            <ShieldAlert className="h-4 w-4" />
            {t("zaloOa.callback.missingCode")}
          </p>
        )}
      </div>
    </div>
  );
}
