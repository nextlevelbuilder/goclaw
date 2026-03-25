import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Loader2, ExternalLink } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";

interface Props {
  clientId: string;
  clientSecret: string;
  /** Called with the refresh_token_preview after successful exchange */
  onAuthorized: (previewToken: string) => void;
}

/**
 * Two-step Google OAuth wizard embedded inside the GWS settings dialog.
 * Step 1: generate auth URL  →  Step 2: paste code and exchange for refresh token.
 */
export function GWSOAuthWizard({ clientId, clientSecret, onAuthorized }: Props) {
  const http = useHttp();
  const [authURL, setAuthURL] = useState("");
  const [authCode, setAuthCode] = useState("");
  const [gettingURL, setGettingURL] = useState(false);
  const [exchanging, setExchanging] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const hasCredentials = clientId.trim() !== "" && clientSecret.trim() !== "";

  const handleGetAuthURL = async () => {
    setError("");
    setSuccess("");
    setAuthURL("");
    if (!hasCredentials) {
      setError("Client ID and Client Secret are required.");
      return;
    }
    setGettingURL(true);
    try {
      const res = await http.post<{ url: string }>("/v1/tools/builtin/gws/oauth-url", {
        client_id: clientId.trim(),
        client_secret: clientSecret.trim(),
      });
      setAuthURL(res.url);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to generate URL");
    } finally {
      setGettingURL(false);
    }
  };

  const handleExchange = async () => {
    setError("");
    setSuccess("");
    if (!authCode.trim()) {
      setError("Paste the authorization code first.");
      return;
    }
    setExchanging(true);
    try {
      const res = await http.post<{ status: string; refresh_token_preview: string }>(
        "/v1/tools/builtin/gws/oauth-exchange",
        {
          client_id: clientId.trim(),
          client_secret: clientSecret.trim(),
          code: authCode.trim(),
        },
      );
      setSuccess(`Authorized. Refresh token saved (preview: ${res.refresh_token_preview})`);
      setAuthCode("");
      setAuthURL("");
      onAuthorized(res.refresh_token_preview);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Exchange failed");
    } finally {
      setExchanging(false);
    }
  };

  return (
    <div className="space-y-3">
      {/* Step 1 */}
      <div className="rounded-md border p-3 space-y-3">
        <p className="text-xs font-medium">Step 1 — Get Authorization URL</p>
        <Button
          type="button"
          variant="secondary"
          size="sm"
          onClick={handleGetAuthURL}
          disabled={gettingURL || !hasCredentials}
        >
          {gettingURL && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
          Get Authorization URL
        </Button>
        {authURL && (
          <div className="space-y-1">
            <p className="text-xs text-muted-foreground">
              Open this URL, sign in with Google, and copy the authorization code:
            </p>
            <a
              href={authURL}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 text-xs text-blue-600 hover:underline break-all"
            >
              Open Google Authorization <ExternalLink className="h-3 w-3 shrink-0" />
            </a>
          </div>
        )}
      </div>

      {/* Step 2 */}
      <div className="rounded-md border p-3 space-y-3">
        <p className="text-xs font-medium">Step 2 — Paste Authorization Code</p>
        <Input
          id="gws-auth-code"
          type="text"
          placeholder="4/0Axx..."
          value={authCode}
          onChange={(e) => setAuthCode(e.target.value)}
        />
        <Button
          type="button"
          variant="default"
          size="sm"
          onClick={handleExchange}
          disabled={exchanging || !authCode.trim() || !hasCredentials}
        >
          {exchanging && <Loader2 className="h-3 w-3 animate-spin mr-1" />}
          Authorize
        </Button>
      </div>

      {error && <p className="text-xs text-destructive">{error}</p>}
      {success && <p className="text-xs text-green-600">{success}</p>}
    </div>
  );
}
