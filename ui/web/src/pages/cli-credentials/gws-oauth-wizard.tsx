import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useHttp } from "@/hooks/use-ws";

interface Props {
  /** Called when OAuth completes — provides the written credentials file path */
  onAuthorized: (credentialsFilePath: string) => void;
}

/**
 * GwsOAuthWizard renders a self-contained step-by-step Google OAuth flow
 * for the gws (Google Workspace CLI) preset. Steps:
 *  1. Enter Client ID → get authorization URL
 *  2. User opens URL and pastes the auth code
 *  3. Exchange code → receive credentials file path
 */
export function GwsOAuthWizard({ onAuthorized }: Props) {
  const { t } = useTranslation("cli-credentials");
  const http = useHttp();

  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [authUrl, setAuthUrl] = useState("");
  const [code, setCode] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [authorized, setAuthorized] = useState(false);

  const handleGetUrl = async () => {
    const id = clientId.trim();
    if (!id) {
      setError(t("gws.clientIdRequired"));
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await http.post<{ url: string }>("/v1/cli-credentials/gws/oauth-url", { client_id: id });
      setAuthUrl(res.url);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("gws.urlFailed"));
    } finally {
      setLoading(false);
    }
  };

  const handleAuthorize = async () => {
    const id = clientId.trim();
    const secret = clientSecret.trim();
    const authCode = code.trim();
    if (!id || !secret || !authCode) {
      setError(t("gws.allFieldsRequired"));
      return;
    }
    setLoading(true);
    setError("");
    try {
      const res = await http.post<{ credentials_file: string; status: string }>(
        "/v1/cli-credentials/gws/oauth-exchange",
        { client_id: id, client_secret: secret, code: authCode },
      );
      setAuthorized(true);
      onAuthorized(res.credentials_file);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("gws.exchangeFailed"));
    } finally {
      setLoading(false);
    }
  };

  if (authorized) {
    return (
      <div className="rounded-md border border-green-200 bg-green-50 dark:border-green-900 dark:bg-green-950 p-3">
        <p className="text-sm font-medium text-green-700 dark:text-green-400">{t("gws.authorized")}</p>
        <p className="text-xs text-muted-foreground mt-0.5">{t("gws.credFilledHint")}</p>
      </div>
    );
  }

  return (
    <div className="grid gap-3 rounded-md border p-3">
      <p className="text-sm font-medium">{t("gws.wizardTitle")}</p>
      <p className="text-xs text-muted-foreground">{t("gws.wizardHint")}</p>

      {/* Step 1 — Client credentials */}
      <div className="grid gap-1.5">
        <Label htmlFor="gws-client-id">{t("gws.clientId")}</Label>
        <Input
          id="gws-client-id"
          value={clientId}
          onChange={(e) => setClientId(e.target.value)}
          placeholder="xxxxxxxxxx.apps.googleusercontent.com"
          className="text-base md:text-sm"
          autoComplete="off"
        />
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="gws-client-secret">{t("gws.clientSecret")}</Label>
        <Input
          id="gws-client-secret"
          type="password"
          value={clientSecret}
          onChange={(e) => setClientSecret(e.target.value)}
          placeholder={t("gws.clientSecretPlaceholder")}
          className="text-base md:text-sm"
          autoComplete="off"
        />
      </div>

      <Button
        type="button"
        variant="secondary"
        size="sm"
        disabled={loading || !clientId.trim()}
        onClick={handleGetUrl}
      >
        {t("gws.getAuthUrl")}
      </Button>

      {/* Step 2 — Authorization URL */}
      {authUrl && (
        <div className="grid gap-1.5">
          <p className="text-xs text-muted-foreground">{t("gws.openUrlHint")}</p>
          <a
            href={authUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-xs text-primary underline break-all"
          >
            {authUrl}
          </a>
          <Label htmlFor="gws-code">{t("gws.authCode")}</Label>
          <Input
            id="gws-code"
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder={t("gws.authCodePlaceholder")}
            className="text-base md:text-sm"
            autoComplete="off"
          />
          <Button
            type="button"
            size="sm"
            disabled={loading || !code.trim()}
            onClick={handleAuthorize}
          >
            {loading ? t("gws.authorizing") : t("gws.authorize")}
          </Button>
        </div>
      )}

      {error && <p className="text-sm text-destructive">{error}</p>}
    </div>
  );
}
