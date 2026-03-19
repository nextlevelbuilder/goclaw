import { useState } from "react";
import { useTranslation } from "react-i18next";
import { AlertCircle } from "lucide-react";

interface TokenFormProps {
  onSubmit: (userId: string, token: string) => void;
}

export function TokenForm({ onSubmit }: TokenFormProps) {
  const { t } = useTranslation("login");
  const [userId, setUserId] = useState("");
  const [token, setToken] = useState("");
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!token.trim() || !userId.trim()) return;

    setConnecting(true);
    setError(null);

    try {
      // Verify connectivity and credentials before navigating
      const res = await fetch("/v1/agents", {
        headers: {
          Authorization: `Bearer ${token.trim()}`,
          "X-GoClaw-User-Id": userId.trim(),
        },
      });

      if (res.status === 401) {
        setError(t("token.errorInvalidCredentials"));
        return;
      }

      if (!res.ok) {
        setError(t("token.errorServer", { status: res.status }));
        return;
      }

      onSubmit(userId.trim(), token.trim());
    } catch {
      setError(t("token.errorCannotConnect"));
    } finally {
      setConnecting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <label htmlFor="userId" className="text-sm font-medium">
          {t("token.userId")}
        </label>
        <input
          id="userId"
          type="text"
          value={userId}
          onChange={(e) => { setUserId(e.target.value); setError(null); }}
          placeholder={t("token.userIdPlaceholder")}
          className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base md:text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          autoFocus
          disabled={connecting}
        />
      </div>

      <div className="space-y-2">
        <label htmlFor="token" className="text-sm font-medium">
          {t("token.gatewayToken")}
        </label>
        <input
          id="token"
          type="password"
          value={token}
          onChange={(e) => { setToken(e.target.value); setError(null); }}
          placeholder={t("token.tokenPlaceholder")}
          className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base md:text-sm shadow-sm transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          disabled={connecting}
        />
      </div>

      {error && (
        <div className="flex items-start gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{error}</span>
        </div>
      )}

      <button
        type="submit"
        disabled={!token.trim() || !userId.trim() || connecting}
        className="inline-flex h-9 w-full items-center justify-center rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-50"
      >
        {connecting ? t("token.connecting") : t("token.connect")}
      </button>
    </form>
  );
}
