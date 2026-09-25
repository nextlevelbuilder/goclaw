import { useState, useCallback, useEffect } from "react";
import {
  Save,
  Loader2,
  Link2,
  CircleCheck,
  RefreshCw,
  Copy,
  Check,
  ShieldCheck,
  ShieldAlert,
  ExternalLink,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import type { ChannelInstanceData } from "@/types/channel";
import { credentialsSchema } from "../channel-schemas";
import { ChannelFields } from "../channel-fields";
import { ZaloOAConsentDialog } from "../zalo/zalo-oa-consent-dialog";
import { zaloDeveloperAppURL, zaloWebhookSettingsURL } from "../zalo/zalo-developer-url";
import { useWsCall } from "@/hooks/use-ws-call";
import { useTranslation } from "react-i18next";

interface ChannelCredentialsTabProps {
  instance: ChannelInstanceData;
  onUpdate: (updates: Record<string, unknown>) => Promise<void>;
  onRefresh?: () => void;
}

interface CallbackURLResp {
  url: string;
}

export function ChannelCredentialsTab({ instance, onUpdate, onRefresh }: ChannelCredentialsTabProps) {
  const { t } = useTranslation("channels");
  const [values, setValues] = useState<Record<string, unknown>>({});
  const [saving, setSaving] = useState(false);
  const [consentOpen, setConsentOpen] = useState(false);
  const [callbackCopied, setCallbackCopied] = useState(false);
  const [webhookCopied, setWebhookCopied] = useState(false);
  const [webhookSecret, setWebhookSecret] = useState("");
  const [webhookSaving, setWebhookSaving] = useState(false);
  const [webhookSaved, setWebhookSaved] = useState(false);
  const [webhookSaveError, setWebhookSaveError] = useState("");

  const isZaloOA = instance.channel_type === "zalo_oa";
  const fields = (credentialsSchema[instance.channel_type] ?? []).filter(
    (field) => !isZaloOA || field.key !== "webhook_secret_key",
  );
  const oaID = instance.credentials?.oa_id;
  const connected = isZaloOA && typeof oaID === "string" && oaID.trim() !== "";
  const transport = typeof instance.config?.transport === "string" ? instance.config.transport : "webhook";
  const showWebhookSetup = isZaloOA && transport !== "polling";
  const webhookURL = instance.webhook_url ?? "";
  const developerAppURL = zaloDeveloperAppURL(instance.credentials?.app_id);
  const callbackSettingsURL = developerAppURL ? `${developerAppURL}/oa/settings` : "";
  const webhookSettingsURL = zaloWebhookSettingsURL(instance.credentials?.app_id);
  const storedWebhookSecret = instance.credentials?.webhook_secret_key;
  const hasPersistedWebhookSecret =
    typeof storedWebhookSecret === "string" && storedWebhookSecret.trim() !== "";
  const signatureMode = typeof instance.config?.webhook_signature_mode === "string"
    ? instance.config.webhook_signature_mode
    : "strict";
  const webhookSecured = (hasPersistedWebhookSecret || webhookSaved) && signatureMode === "strict";

  useEffect(() => {
    setWebhookSecret("");
    setWebhookSaved(false);
    setWebhookSaveError("");
  }, [instance.id]);

  // OAuth callback URL is separate from the inbound webhook endpoint.
  const callback = useWsCall<CallbackURLResp>("channels.instances.zalo_oa.callback_url");
  const [callbackURL, setCallbackURL] = useState("");
  const [callbackError, setCallbackError] = useState(false);
  useEffect(() => {
    if (!isZaloOA) return;
    let cancelled = false;
    const storedCallbackURL = instance.callback_url ?? "";
    setCallbackError(false);
    setCallbackURL(storedCallbackURL);
    callback
      .call({ instance_id: instance.id })
      .then((resp) => {
        if (!cancelled && resp.url) setCallbackURL(resp.url);
      })
      .catch(() => {
        if (!cancelled && !storedCallbackURL) setCallbackError(true);
      });
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isZaloOA, instance.id, instance.callback_url]);

  const handleCopyCallback = async () => {
    if (!callbackURL) return;
    try {
      await navigator.clipboard.writeText(callbackURL);
      setCallbackCopied(true);
      setTimeout(() => setCallbackCopied(false), 1500);
    } catch {
      // Clipboard access can be blocked by the browser; the input stays selectable.
    }
  };

  const handleCopyWebhook = async () => {
    if (!webhookURL) return;
    try {
      await navigator.clipboard.writeText(webhookURL);
      setWebhookCopied(true);
      setTimeout(() => setWebhookCopied(false), 1500);
    } catch {
      // Clipboard access can be blocked by the browser; the input stays selectable.
    }
  };

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }));
  }, []);

  const handleSave = async () => {
    const cleanCreds = Object.fromEntries(
      Object.entries(values).filter(([, v]) => v !== undefined && v !== "" && v !== null),
    );
    if (Object.keys(cleanCreds).length === 0) return;
    setSaving(true);
    try {
      await onUpdate({ credentials: cleanCreds });
      setValues({});
    } catch {
      // Toast shown by the channel update hook.
    } finally {
      setSaving(false);
    }
  };

  const handleSaveWebhookSecret = async () => {
    const secret = webhookSecret.trim();
    if (!secret) return;
    setWebhookSaving(true);
    setWebhookSaveError("");
    try {
      await onUpdate({
        credentials: { webhook_secret_key: secret },
        config: { ...instance.config, webhook_signature_mode: "strict" },
      });
      setWebhookSecret("");
      setWebhookSaved(true);
      onRefresh?.();
    } catch {
      setWebhookSaveError(t("zaloOa.webhookSetup.saveError"));
    } finally {
      setWebhookSaving(false);
    }
  };

  if (fields.length === 0) {
    return (
      <div>
        <p className="text-sm text-muted-foreground">{t("detail.credentials.noSchema")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      {isZaloOA && (
        <div className="flex flex-col gap-3 rounded-md border p-4 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            {connected ? (
              <>
                <p className="flex items-center gap-2 text-sm font-medium text-green-600">
                  <CircleCheck className="h-4 w-4" />
                  {t("zaloOa.connectBanner.connected", { oaId: String(oaID) })}
                </p>
                <p className="text-xs text-muted-foreground">{t("zaloOa.connectBanner.connectedHint")}</p>
              </>
            ) : (
              <>
                <p className="text-sm font-medium">{t("zaloOa.connectBanner.title")}</p>
                <p className="text-xs text-muted-foreground">{t("zaloOa.connectBanner.hint")}</p>
              </>
            )}
          </div>
          <Button variant="outline" onClick={() => setConsentOpen(true)}>
            {connected ? <RefreshCw className="h-4 w-4" /> : <Link2 className="h-4 w-4" />}
            {connected ? t("zaloOa.reconnect") : t("zaloOa.connect")}
          </Button>
        </div>
      )}

      <p className="text-sm text-muted-foreground">{t("detail.credentials.hint")}</p>

      <ChannelFields
        fields={fields}
        values={values}
        onChange={handleChange}
        idPrefix="cd-cred"
        isEdit
      />

      <div className="flex items-center justify-end gap-2">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("detail.credentials.saving") : t("detail.credentials.updateCredentials")}
        </Button>
      </div>
      {isZaloOA && (
        <div className="space-y-2 rounded-md border p-4">
          <label htmlFor="zalo-oa-callback-url" className="text-sm font-medium">
            {t("zaloOa.callbackUrlLabel")}
          </label>
          <p className="text-xs text-muted-foreground">{t("zaloOa.callbackUrlHelp")}</p>
          <div className="grid min-w-0 gap-2">
            <input
              id="zalo-oa-callback-url"
              readOnly
              value={callbackURL}
              className="min-w-0 w-full rounded-md border bg-muted px-3 py-2 font-mono text-base md:text-sm"
              placeholder={callback.loading ? "…" : t("zaloOa.callbackUrlUnavailable")}
              onFocus={(event) => event.currentTarget.select()}
            />
            <div className="flex flex-wrap items-center gap-2">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleCopyCallback}
                disabled={!callbackURL}
                aria-label={`${t("zaloOa.webhookSetup.copy")}: ${t("zaloOa.callbackUrlLabel")}`}
              >
                {callbackCopied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                {t("zaloOa.callbackUrlCopy")}
              </Button>
              {callbackSettingsURL && (
                <Button asChild variant="outline" size="sm">
                  <a href={callbackSettingsURL} target="_blank" rel="noreferrer">
                    <ExternalLink className="h-4 w-4" />
                    {t("zaloOa.openCallbackSettings")}
                  </a>
                </Button>
              )}
            </div>
          </div>
          {callbackError && (
            <p className="text-xs text-destructive">{t("zaloOa.callbackUrlUnavailable")}</p>
          )}
          <span className="sr-only" aria-live="polite">
            {callbackCopied ? t("zaloOa.webhookSetup.copied") : ""}
          </span>
        </div>
      )}

      {showWebhookSetup && (
        <section className="space-y-4 rounded-md border p-4" aria-labelledby="zalo-oa-webhook-setup-title">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="space-y-1">
              <h3 id="zalo-oa-webhook-setup-title" className="text-sm font-medium">
                {t("zaloOa.webhookSetup.title")}
              </h3>
              <p className="text-xs text-muted-foreground">{t("zaloOa.webhookSetup.help")}</p>
            </div>
            <div
              className={
                webhookSecured
                  ? "flex items-center gap-1.5 text-sm font-medium text-green-600"
                  : "flex items-center gap-1.5 text-sm font-medium text-amber-600"
              }
            >
              {webhookSecured ? <ShieldCheck className="h-4 w-4" /> : <ShieldAlert className="h-4 w-4" />}
              {webhookSecured
                ? t("zaloOa.webhookSetup.secured")
                : t("zaloOa.webhookSetup.awaitingSecret")}
            </div>
          </div>

          <div className="space-y-2">
            <label htmlFor="zalo-oa-webhook-url" className="text-sm font-medium">
              {t("zaloOa.webhookSetup.urlLabel")}
            </label>
            <p className="text-xs text-muted-foreground">{t("zaloOa.webhookSetup.urlHelp")}</p>
            <div className="grid min-w-0 gap-2">
              <input
                id="zalo-oa-webhook-url"
                readOnly
                value={webhookURL}
                className="min-w-0 w-full rounded-md border bg-muted px-3 py-2 font-mono text-base md:text-sm"
                placeholder={t("zaloOa.webhookSetup.urlUnavailable")}
                onFocus={(event) => event.currentTarget.select()}
              />
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleCopyWebhook}
                  disabled={!webhookURL}
                  aria-label={`${t("zaloOa.webhookSetup.copy")}: ${t("zaloOa.webhookSetup.urlLabel")}`}
                >
                  {webhookCopied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
                  {t("zaloOa.webhookSetup.copy")}
                </Button>
                {webhookSettingsURL && (
                  <Button asChild variant="outline" size="sm">
                    <a href={webhookSettingsURL} target="_blank" rel="noreferrer">
                      <ExternalLink className="h-4 w-4" />
                      {t("zaloOa.webhookSetup.openConsole")}
                    </a>
                  </Button>
                )}
              </div>
            </div>
            {!webhookURL && (
              <p className="text-xs text-destructive">{t("zaloOa.webhookSetup.urlUnavailable")}</p>
            )}
            <span className="sr-only" aria-live="polite">
              {webhookCopied ? t("zaloOa.webhookSetup.copied") : ""}
            </span>
          </div>

          <div className="space-y-2 border-t pt-4">
            <label htmlFor="zalo-oa-webhook-secret" className="text-sm font-medium">
              {t("zaloOa.webhookSetup.secretLabel")}
            </label>
            <p className="text-xs text-muted-foreground">
              {webhookSecured
                ? t("zaloOa.webhookSetup.secretRotateHelp")
                : t("zaloOa.webhookSetup.secretHelp")}
            </p>
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
              <input
                id="zalo-oa-webhook-secret"
                type="password"
                autoComplete="new-password"
                value={webhookSecret}
                onChange={(event) => setWebhookSecret(event.target.value)}
                disabled={webhookSaving}
                className="min-w-0 w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm"
                placeholder={t("zaloOa.webhookSetup.secretPlaceholder")}
              />
              <Button
                type="button"
                onClick={handleSaveWebhookSecret}
                disabled={webhookSaving || webhookSecret.trim() === ""}
              >
                {webhookSaving ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
                {webhookSaving ? t("zaloOa.webhookSetup.saving") : t("zaloOa.webhookSetup.saveEnable")}
              </Button>
            </div>
            {webhookSaveError && <p className="text-xs text-destructive">{webhookSaveError}</p>}
          </div>
        </section>
      )}

      {isZaloOA && (
        <ZaloOAConsentDialog
          open={consentOpen}
          onOpenChange={setConsentOpen}
          instanceId={instance.id}
          instanceName={instance.name}
          onSuccess={() => {
            onRefresh?.();
          }}
        />
      )}
    </div>
  );
}
