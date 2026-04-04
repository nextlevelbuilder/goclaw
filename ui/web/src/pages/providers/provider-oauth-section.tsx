import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Loader2, ExternalLink, CheckCircle, ClipboardPaste } from "lucide-react";
import { useHttp } from "@/hooks/use-ws";
import { isValidSlug } from "@/lib/slug";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";

interface OAuthStatus {
  authenticated: boolean;
  provider_name?: string;
  error?: string;
}

interface StartResponse {
  auth_url?: string;
  status?: string;
}

interface OAuthSectionProps {
  onSuccess: () => void;
  authenticatedActionLabel?: string;
  providerName?: string;
  displayName?: string;
  apiBase?: string;
}

export function OAuthSection({
  onSuccess,
  authenticatedActionLabel,
  providerName,
  displayName,
  apiBase,
}: OAuthSectionProps) {
  const { t } = useTranslation("providers");
  const queryClient = useQueryClient();
  const http = useHttp();
  const resolvedProviderName = providerName?.trim() ?? "";
  const hasValidProvider = resolvedProviderName.length > 0 && isValidSlug(resolvedProviderName);
  const [status, setStatus] = useState<OAuthStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [waitingCallback, setWaitingCallback] = useState(false);
  const [pasteUrl, setPasteUrl] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [justAuthenticated, setJustAuthenticated] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const actionLabel = authenticatedActionLabel || t("oauth.done");
  const renderUsageHint = (provider: string) => (
    <div className="rounded-md border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
      <div className="flex flex-wrap gap-1.5">
        <Badge variant="outline" className="bg-background/80">{t("oauth.poolBadge")}</Badge>
        <Badge variant="outline" className="bg-background/80">{t("oauth.roundRobinBadge")}</Badge>
      </div>
      <p className="mt-2">{t("oauth.multiAccountHint")}</p>
      <p className="mt-1">
        {t("oauth.modelPrefixHint")} <code className="rounded bg-muted px-1 font-mono">{provider}/</code>{" "}
        {t("oauth.modelPrefixExample", {
          example: `${provider}/gpt-5.4`,
        })}
      </p>
    </div>
  );

  const stopPolling = () => {
    if (pollRef.current) { clearInterval(pollRef.current); pollRef.current = null; }
    if (timeoutRef.current) { clearTimeout(timeoutRef.current); timeoutRef.current = null; }
  };

  const fetchStatus = useCallback(async () => {
    if (!hasValidProvider) {
      setStatus(null);
      setLoading(false);
      return null;
    }
    try {
      const res = await http.get<OAuthStatus>(`/v1/auth/chatgpt/${encodeURIComponent(resolvedProviderName)}/status`);
      setStatus(res);
      return res;
    } catch {
      setStatus(null);
      return null;
    } finally {
      setLoading(false);
    }
  }, [hasValidProvider, http, resolvedProviderName]);

  useEffect(() => {
    fetchStatus();
    return stopPolling;
  }, [fetchStatus]);

  const showSuccess = () => {
    setWaitingCallback(false);
    setJustAuthenticated(true);
    queryClient.invalidateQueries({ queryKey: ["providers"] });
  };

  const handleStart = async () => {
    if (!hasValidProvider) return;
    setStarting(true);
    try {
      const res = await http.post<StartResponse>(`/v1/auth/chatgpt/${encodeURIComponent(resolvedProviderName)}/start`, {
        display_name: displayName?.trim() || undefined,
        api_base: apiBase?.trim() || undefined,
      });
      if (res.status === "already_authenticated") {
        await fetchStatus();
        showSuccess();
        return;
      }
      if (res.auth_url) {
        window.open(res.auth_url, "_blank", "noopener,noreferrer");
        setWaitingCallback(true);
        setPasteUrl("");
        pollRef.current = setInterval(async () => {
          const s = await fetchStatus();
          if (s?.authenticated) {
            stopPolling();
            showSuccess();
          }
        }, 2000);
        timeoutRef.current = setTimeout(() => {
          stopPolling();
          setWaitingCallback(false);
        }, 6 * 60 * 1000);
      }
    } catch (err) {
      toast.error(i18next.t("providers:oauth.oauthFailed"), err instanceof Error ? err.message : "");
    } finally {
      setStarting(false);
    }
  };

  const handlePasteSubmit = async () => {
    const url = pasteUrl.trim();
    if (!url || !hasValidProvider) return;
    setSubmitting(true);
    try {
      await http.post(`/v1/auth/chatgpt/${encodeURIComponent(resolvedProviderName)}/callback`, { redirect_url: url });
      stopPolling();
      setPasteUrl("");
      await fetchStatus();
      showSuccess();
    } catch (err) {
      toast.error(i18next.t("providers:oauth.exchangeFailed"), err instanceof Error ? err.message : "");
    } finally {
      setSubmitting(false);
    }
  };

  const handleLogout = async () => {
    if (!hasValidProvider) return;
    try {
      await http.post(`/v1/auth/chatgpt/${encodeURIComponent(resolvedProviderName)}/logout`);
      setStatus({ authenticated: false });
      queryClient.invalidateQueries({ queryKey: ["providers"] });
      toast.success(i18next.t("providers:oauth.loggedOut"), i18next.t("providers:oauth.loggedOutDesc"));
    } catch (err) {
      toast.error(i18next.t("providers:oauth.logoutFailed"), err instanceof Error ? err.message : "");
    }
  };

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> {t("oauth.checkingStatus")}
      </div>
    );
  }

  // Just authenticated — show success with countdown
  if (justAuthenticated) {
    const activeProvider = status?.provider_name || resolvedProviderName;
    return (
      <div className="space-y-3 py-2">
        <div className="flex items-center gap-2 rounded-md border border-green-500/30 bg-green-500/5 px-4 py-3 text-sm text-green-700 dark:text-green-400">
          <CheckCircle className="h-5 w-5 shrink-0" />
          <div>
            <p className="font-medium">{t("oauth.authSuccessful")}</p>
            <p className="mt-0.5 text-xs opacity-80">
              {t("oauth.activeProvider")} <code className="rounded bg-muted px-1 font-mono text-xs">{activeProvider}</code>.{" "}
              {t("oauth.authSuccessfulDesc")}
            </p>
          </div>
        </div>
        {renderUsageHint(activeProvider)}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={onSuccess}>
            {actionLabel}
          </Button>
        </div>
      </div>
    );
  }

  // Already authenticated (opened dialog when already authed)
  if (status?.authenticated) {
    const activeProvider = status.provider_name || resolvedProviderName;
    return (
      <div className="space-y-3">
        <div className="flex items-center gap-2 rounded-md border border-green-500/30 bg-green-500/5 px-3 py-2 text-sm text-green-700 dark:text-green-400">
          <CheckCircle className="h-4 w-4 shrink-0" />
          <span>
            {t("oauth.authenticated")} <code className="rounded bg-muted px-1 font-mono text-xs">{activeProvider}</code> {t("oauth.active")}
          </span>
        </div>
        {renderUsageHint(activeProvider)}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={onSuccess}>
            {actionLabel}
          </Button>
          <Button variant="outline" size="sm" onClick={handleLogout} className="gap-1.5">
            {t("oauth.removeToken")}
          </Button>
        </div>
      </div>
    );
  }

  if (!hasValidProvider) {
    return (
      <div className="rounded-md border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
        <p className="text-sm text-foreground">{t("oauth.signInDesc")}</p>
        <p className="mt-1">{t("oauth.aliasRequired")}</p>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="rounded-md border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
        <p className="text-sm text-foreground">{t("oauth.signInDesc")}</p>
        <div className="mt-2 flex flex-wrap gap-1.5">
          <Badge variant="outline" className="bg-background/80">{t("oauth.poolBadge")}</Badge>
          <Badge variant="outline" className="bg-background/80">{t("oauth.roundRobinBadge")}</Badge>
        </div>
        <p className="mt-2">{t("oauth.multiAccountHint")}</p>
        <p className="mt-1">
          {t("oauth.modelPrefixHint")} <code className="rounded bg-muted px-1 font-mono">{resolvedProviderName}/</code>{" "}
          {t("oauth.modelPrefixExample", {
            example: `${resolvedProviderName}/gpt-5.4`,
          })}
        </p>
      </div>
      {waitingCallback ? (
        <div className="space-y-3">
          <div className="flex items-center gap-2 rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-700 dark:text-blue-400">
            <Loader2 className="h-4 w-4 shrink-0 animate-spin" />
            <span>{t("oauth.waiting")}</span>
          </div>
          <div className="rounded-md border border-amber-500/30 bg-amber-500/5 p-3 space-y-2">
            <p className="text-xs text-amber-700 dark:text-amber-400">
              <strong>{t("oauth.remoteVps")}</strong>{" "}{t("oauth.remoteVpsHint")}{" "}
              <code className="text-xs">localhost:1455</code>{" "}{t("oauth.remoteVpsError")}
            </p>
            <div className="flex gap-2">
              <Input
                placeholder={t("oauth.pasteUrlPlaceholder")}
                value={pasteUrl}
                onChange={(e) => setPasteUrl(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handlePasteSubmit()}
                className="text-xs font-mono h-8"
              />
              <Button
                size="sm"
                onClick={handlePasteSubmit}
                disabled={submitting || !pasteUrl.trim()}
                className="gap-1.5 shrink-0 h-8"
              >
                {submitting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ClipboardPaste className="h-3.5 w-3.5" />}
                {t("oauth.submit")}
              </Button>
            </div>
          </div>
        </div>
      ) : (
        <Button size="sm" onClick={handleStart} disabled={starting} className="gap-1.5">
          {starting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />}
          {starting ? t("oauth.starting") : t("oauth.signInWithChatGPT")}
        </Button>
      )}
    </div>
  );
}
