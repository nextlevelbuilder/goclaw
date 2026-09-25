import { useEffect, useState } from "react";
import { Check, Copy, ExternalLink } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { useHttp } from "@/hooks/use-ws";
import { zaloDeveloperAppURL, zaloWebhookSettingsURL } from "./zalo-developer-url";

interface ZaloOACreateSetupProps {
  name: string;
  appID: unknown;
  usesWebhook: boolean;
}

interface SetupPreview {
  callback_url: string;
  webhook_url: string;
}

export function ZaloOACreateSetup({ name, appID, usesWebhook }: ZaloOACreateSetupProps) {
  const { t } = useTranslation("channels");
  const http = useHttp();
  const [preview, setPreview] = useState<SetupPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [copiedField, setCopiedField] = useState<"callback" | "webhook" | null>(null);

  useEffect(() => {
    const channelName = name.trim();
    if (!channelName) {
      setPreview(null);
      setLoadError(false);
      setLoading(false);
      return;
    }
    setPreview(null);
    setLoadError(false);
    setLoading(false);

    let cancelled = false;
    const timer = window.setTimeout(() => {
      setLoading(true);
      setLoadError(false);
      http.get<SetupPreview>("/v1/channels/setup/zalo-oa", { name: channelName })
        .then((result) => {
          if (!cancelled) setPreview(result);
        })
        .catch(() => {
          if (!cancelled) {
            setPreview(null);
            setLoadError(true);
          }
        })
        .finally(() => {
          if (!cancelled) setLoading(false);
        });
    }, 250);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [http, name]);

  const copyURL = async (kind: "callback" | "webhook", value: string) => {
    if (!value) return;
    try {
      await navigator.clipboard.writeText(value);
      setCopiedField(kind);
      window.setTimeout(() => setCopiedField(null), 1500);
    } catch {
      // The read-only input remains selectable when clipboard access is blocked.
    }
  };

  const developerAppURL = zaloDeveloperAppURL(appID);
  const callbackSettingsURL = developerAppURL ? `${developerAppURL}/oa/settings` : "";
  const webhookSettingsURL = zaloWebhookSettingsURL(appID);
  const callbackURL = preview?.callback_url ?? "";
  const webhookURL = preview?.webhook_url ?? "";

  return (
    <section className="space-y-4 rounded-md border border-blue-500/30 bg-blue-500/5 p-4">
      <div>
        <p className="text-sm font-semibold">{t("zaloOa.setup.title")}</p>
        <p className="mt-1 text-xs text-muted-foreground">
          {t(usesWebhook ? "zaloOa.setup.description" : "zaloOa.setup.descriptionPolling")}
        </p>
      </div>

      {loading && (
        <p className="text-xs text-muted-foreground">{t("zaloOa.setup.loading")}</p>
      )}
      {loadError && (
        <p className="text-xs text-destructive">{t("zaloOa.setup.loadError")}</p>
      )}

      <SetupURL
        id="zalo-oa-callback-preview"
        label={t("zaloOa.callbackUrlLabel")}
        help={t("zaloOa.callbackUrlHelp")}
        value={callbackURL}
        unavailable={t("zaloOa.callbackUrlUnavailable")}
        copied={copiedField === "callback"}
        onCopy={() => copyURL("callback", callbackURL)}
        copyLabel={t("zaloOa.webhookSetup.copy")}
        copiedLabel={t("zaloOa.webhookSetup.copied")}
        developerURL={callbackSettingsURL}
        developerLabel={t("zaloOa.openCallbackSettings")}
      />

      {usesWebhook && (
        <SetupURL
          id="zalo-oa-webhook-preview"
          label={t("zaloOa.webhookSetup.urlLabel")}
          help={t("zaloOa.webhookSetup.urlHelp")}
          value={webhookURL}
          unavailable={t("zaloOa.webhookSetup.urlUnavailable")}
          copied={copiedField === "webhook"}
          onCopy={() => copyURL("webhook", webhookURL)}
          copyLabel={t("zaloOa.webhookSetup.copy")}
          copiedLabel={t("zaloOa.webhookSetup.copied")}
          developerURL={webhookSettingsURL}
          developerLabel={t("zaloOa.webhookSetup.openConsole")}
        />
      )}
    </section>
  );
}

interface SetupURLProps {
  id: string;
  label: string;
  help: string;
  value: string;
  unavailable: string;
  copied: boolean;
  onCopy: () => void;
  copyLabel: string;
  copiedLabel: string;
  developerURL?: string;
  developerLabel?: string;
}

function SetupURL({
  id,
  label,
  help,
  value,
  unavailable,
  copied,
  onCopy,
  copyLabel,
  copiedLabel,
  developerURL,
  developerLabel,
}: SetupURLProps) {
  return (
    <div className="space-y-2">
      <div>
        <label htmlFor={id} className="text-sm font-medium">{label}</label>
        <p id={`${id}-help`} className="mt-1 text-xs text-muted-foreground">{help}</p>
      </div>
      <div className="grid min-w-0 gap-2">
        <input
          id={id}
          aria-describedby={`${id}-help`}
          readOnly
          value={value}
          placeholder={unavailable}
          className="min-w-0 w-full rounded-md border bg-muted px-3 py-2 font-mono text-base md:text-sm"
          onFocus={(event) => event.currentTarget.select()}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button type="button" variant="outline" size="sm" onClick={onCopy} disabled={!value} aria-label={`${copyLabel}: ${label}`}>
            {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
            {copied ? copiedLabel : copyLabel}
          </Button>
          {developerURL && developerLabel && (
            <Button asChild variant="outline" size="sm">
              <a href={developerURL} target="_blank" rel="noreferrer">
                <ExternalLink className="h-4 w-4" />
                {developerLabel}
              </a>
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
