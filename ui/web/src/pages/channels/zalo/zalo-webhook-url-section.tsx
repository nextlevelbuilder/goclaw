import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useWsCall } from "@/hooks/use-ws-call";
import { useWebhookHost } from "./use-webhook-host";

interface WebhookURLResp {
  path: string;
  slug?: string;
  instance_id: string;
  hint: string;
  oa_id?: string;
}

interface ZaloWebhookURLSectionProps {
  instanceId: string;
  channelType: string; // "zalo_bot" | "zalo_oa"
}

/**
 * Webhook setup card. Renders the full URL using window.location.origin (or
 * persisted override) so operators can copy a paste-ready string for the
 * Zalo dev console without scrolling between sections.
 */
export function ZaloWebhookURLSection({ instanceId, channelType }: ZaloWebhookURLSectionProps) {
  const { t } = useTranslation("channels");
  const { call, loading, error } = useWsCall<WebhookURLResp>("channels.instances.zalo.webhook_url");
  const [data, setData] = useState<WebhookURLResp | null>(null);
  const [copied, setCopied] = useState(false);
  const [host, setHost] = useWebhookHost();
  const [hostError, setHostError] = useState<string | null>(null);

  function validateHost(value: string) {
    const trimmed = value.trim();
    if (!trimmed) {
      setHostError(null);
      return;
    }
    try {
      const u = new URL(trimmed);
      if (u.protocol !== "http:" && u.protocol !== "https:") {
        setHostError(t("detail.zaloWebhook.hostInvalidScheme", { defaultValue: "Host must be http(s)://" }));
        return;
      }
      setHostError(null);
    } catch {
      setHostError(t("detail.zaloWebhook.hostInvalid", { defaultValue: "Host is not a valid URL" }));
    }
  }

  useEffect(() => {
    if (!instanceId) return;
    let cancelled = false;
    call({ instance_id: instanceId })
      .then((resp) => {
        if (cancelled) return;
        setData(resp);
      })
      .catch(() => {
        // error captured by hook
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId]);

  const fullURL = useMemo(() => {
    if (!data?.path) return "";
    const trimmed = host.replace(/\/+$/, "");
    return `${trimmed}${data.path}`;
  }, [host, data?.path]);

  if (channelType !== "zalo_bot" && channelType !== "zalo_oa") {
    return null;
  }

  async function handleCopy() {
    if (!fullURL) return;
    try {
      await navigator.clipboard.writeText(fullURL);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // clipboard unavailable — operator can copy manually
    }
  }

  return (
    <section className="space-y-3 rounded-lg border p-3 sm:p-4 overflow-hidden">
      <h3 className="text-sm font-medium">{t("detail.zaloWebhook.title", { defaultValue: "Webhook setup" })}</h3>

      <div className="grid gap-1.5">
        <Label htmlFor="cd-webhook-host">{t("detail.zaloWebhook.hostLabel", { defaultValue: "Gateway host" })}</Label>
        <Input
          id="cd-webhook-host"
          value={host}
          onChange={(e) => setHost(e.target.value)}
          onBlur={(e) => validateHost(e.target.value)}
          placeholder="https://gw.example.com"
          className="text-base md:text-sm font-mono"
        />
        {hostError && (
          <p className="text-xs text-destructive">{hostError}</p>
        )}
        <p className="text-xs text-muted-foreground">
          {t("detail.zaloWebhook.hostHint", {
            defaultValue: "Override the gateway host if Zalo cannot reach this UI's origin. Stored locally per-browser.",
          })}
        </p>
      </div>

      <div className="grid gap-1.5">
        <Label>{t("detail.zaloWebhook.urlLabel", { defaultValue: "Webhook URL (paste into Zalo console)" })}</Label>
        <div className="flex gap-2">
          <Input
            value={loading ? "" : fullURL}
            placeholder={loading ? t("detail.zaloWebhook.loading", { defaultValue: "Loading..." }) : ""}
            readOnly
            className="text-base md:text-sm font-mono"
          />
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleCopy}
            disabled={!fullURL}
            aria-label={t("detail.zaloWebhook.copy", { defaultValue: "Copy URL" })}
          >
            {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
          </Button>
        </div>
        {data?.hint && (
          <p className="text-xs text-muted-foreground">{data.hint}</p>
        )}
        {error && (
          <p className="text-xs text-destructive">{error.message}</p>
        )}
      </div>

      {channelType === "zalo_oa" && (
        <div className="grid gap-1.5">
          <Label>{t("detail.zaloWebhook.oaIdLabel", { defaultValue: "OA ID" })}</Label>
          <Input
            value={data?.oa_id ?? ""}
            placeholder={t("detail.zaloWebhook.oaIdPlaceholder", { defaultValue: "Auto-discovered after Connect" })}
            readOnly
            className="text-base md:text-sm font-mono"
          />
        </div>
      )}
    </section>
  );
}
