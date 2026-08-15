import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Check, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useClipboard } from "@/hooks/use-clipboard";

// BitrixPortalHelpSection is the collapsible inline help shown beneath the
// Create-Portal form. It guides the operator through creating a Local App
// inside Bitrix24 admin panel — the prerequisite step we cannot automate
// because Bitrix24 only issues client_id/client_secret to a human-authorized
// app registration.
//
// The handler URL shown here is derived from window.location.origin: any
// admin opening the goclaw UI is by definition opening it via the public
// URL, so this matches what they should paste into Bitrix24's app config.
// If they're on localhost, we surface a warning — Bitrix24 cannot reach
// localhost, so the install handler URL must be a public one.
export function BitrixPortalHelpSection() {
  const { t } = useTranslation("channels");
  const [open, setOpen] = useState(false);
  const { copied, copy } = useClipboard();

  const handlerURL = `${window.location.origin}/bitrix24/install`;
  const isLocalDev =
    typeof window !== "undefined" &&
    (window.location.hostname === "localhost" ||
      window.location.hostname.startsWith("127."));

  return (
    <div className="rounded-md border bg-muted/30 px-3 py-2 text-xs">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 text-muted-foreground hover:text-foreground transition-colors"
      >
        {open ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        ℹ {t("bitrix24.create.help.title", { defaultValue: "How to get Client ID and Secret?" })}
      </button>
      {open && (
        <ol className="mt-2 space-y-1.5 pl-4 list-decimal text-muted-foreground">
          <li>{t("bitrix24.create.help.step1", { defaultValue: "Open your Bitrix24 portal as Administrator." })}</li>
          <li>{t("bitrix24.create.help.step2", { defaultValue: "Go to Developer resources → Other → Local application → Add." })}</li>
          <li>
            {t("bitrix24.create.help.step3", { defaultValue: "Set the Handler URL to:" })}
            <div className="mt-1 flex items-center gap-2 rounded bg-background p-2 font-mono">
              <code className="flex-1 truncate text-foreground" title={handlerURL}>
                {handlerURL}
              </code>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => copy(handlerURL)}
                className="h-7 px-2"
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </Button>
            </div>
            {isLocalDev && (
              <p className="mt-1 text-amber-600">
                ⚠ {t("bitrix24.create.help.localDevWarning", {
                  defaultValue: "You're on localhost — Bitrix24 cannot reach this URL. Use Cloudflare Tunnel or your public domain first.",
                })}
              </p>
            )}
          </li>
          <li>
            {t("bitrix24.create.help.step4", { defaultValue: "Tick the required scopes:" })}{" "}
            <code className="text-foreground">im, imbot, user, disk</code>
          </li>
          <li>
            {t("bitrix24.create.help.step5", {
              defaultValue: "Save → copy the Application ID and Key into the form above.",
            })}
          </li>
        </ol>
      )}
    </div>
  );
}
