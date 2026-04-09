import { useCallback, useEffect, useRef, useState } from "react";
import { ExternalLink, Loader2, CheckCircle, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { isValidSlug } from "@/lib/slug";

interface GitHubCopilotOAuthStatus {
  authenticated: boolean;
  pending?: boolean;
  provider_name?: string;
  verification_uri?: string;
  user_code?: string;
  error?: string;
}

interface GitHubCopilotOAuthStartResponse {
  status?: string;
  provider_name?: string;
  verification_uri?: string;
  user_code?: string;
  interval?: number;
  expires_in?: number;
}

interface GitHubCopilotOAuthSectionProps {
  onSuccess: () => void;
  authenticatedActionLabel?: string;
  providerName?: string;
  displayName?: string;
}

export function GitHubCopilotOAuthSection({
  onSuccess,
  authenticatedActionLabel = "Close",
  providerName,
  displayName,
}: GitHubCopilotOAuthSectionProps) {
  const http = useHttp();
  const resolvedProviderName = providerName?.trim() ?? "";
  const hasValidProvider = resolvedProviderName.length > 0 && isValidSlug(resolvedProviderName);
  const [status, setStatus] = useState<GitHubCopilotOAuthStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [enterpriseDomain, setEnterpriseDomain] = useState("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const stopPolling = () => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  };

  const fetchStatus = useCallback(async () => {
    if (!hasValidProvider) {
      setStatus(null);
      setLoading(false);
      return null;
    }
    try {
      const res = await http.get<GitHubCopilotOAuthStatus>(`/v1/auth/copilot/${encodeURIComponent(resolvedProviderName)}/status`);
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

  useEffect(() => {
    if (!status?.pending || status.authenticated) {
      stopPolling();
      return;
    }
    if (pollRef.current) return;
    pollRef.current = setInterval(async () => {
      const next = await fetchStatus();
      if (next?.authenticated) {
        stopPolling();
      }
    }, 2000);
    return stopPolling;
  }, [fetchStatus, status?.authenticated, status?.pending]);

  const handleStart = async () => {
    if (!hasValidProvider) return;
    setStarting(true);
    try {
      const res = await http.post<GitHubCopilotOAuthStartResponse>(
        `/v1/auth/copilot/${encodeURIComponent(resolvedProviderName)}/start`,
        {
          display_name: displayName?.trim() || undefined,
          enterprise_domain: enterpriseDomain.trim() || undefined,
        },
      );
      if (res.status === "already_authenticated") {
        await fetchStatus();
        onSuccess();
        return;
      }
      if (res.verification_uri || res.user_code) {
        setStatus({
          authenticated: false,
          pending: true,
          provider_name: res.provider_name,
          verification_uri: res.verification_uri,
          user_code: res.user_code,
        });
      }
    } catch (err) {
      toast.error("GitHub Copilot sign-in failed", err instanceof Error ? err.message : "");
    } finally {
      setStarting(false);
    }
  };

  const handleLogout = async () => {
    if (!hasValidProvider) return;
    try {
      await http.post(`/v1/auth/copilot/${encodeURIComponent(resolvedProviderName)}/logout`);
      stopPolling();
      setStatus({ authenticated: false });
      toast.success("GitHub Copilot disconnected");
    } catch (err) {
      toast.error("Failed to disconnect GitHub Copilot", err instanceof Error ? err.message : "");
    }
  };

  const copyCode = async () => {
    if (!status?.user_code) return;
    await navigator.clipboard.writeText(status.user_code);
    toast.success("Code copied");
  };

  if (loading) {
    return (
      <div className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> Checking GitHub Copilot status…
      </div>
    );
  }

  if (status?.authenticated) {
    return (
      <div className="space-y-3 py-2">
        <div className="flex items-center gap-2 rounded-md border border-green-500/30 bg-green-500/5 px-4 py-3 text-sm text-green-700 dark:text-green-400">
          <CheckCircle className="h-5 w-5 shrink-0" />
          <div>
            <p className="font-medium">GitHub Copilot connected</p>
            <p className="mt-0.5 text-xs opacity-80">Provider <code className="rounded bg-muted px-1 font-mono text-xs">{status.provider_name || resolvedProviderName}</code> is ready to use.</p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={onSuccess}>{authenticatedActionLabel}</Button>
          <Button variant="outline" size="sm" onClick={handleLogout}>Remove token</Button>
        </div>
      </div>
    );
  }

  if (!hasValidProvider) {
    return (
      <div className="rounded-md border bg-muted/50 px-3 py-2 text-xs text-muted-foreground">
        Enter a provider alias first. GitHub Copilot OAuth providers are created after sign-in completes.
      </div>
    );
  }

  if (status?.pending) {
    return (
      <div className="space-y-3">
        <div className="flex items-center gap-2 rounded-md border border-blue-500/30 bg-blue-500/5 px-3 py-2 text-sm text-blue-700 dark:text-blue-400">
          <Loader2 className="h-4 w-4 animate-spin" /> Waiting for GitHub approval…
        </div>
        <div className="rounded-md border bg-muted/40 p-3 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium">1. Open the GitHub device verification page</p>
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" asChild>
                <a href={status.verification_uri || "https://github.com/login/device"} target="_blank" rel="noreferrer">
                  <ExternalLink className="mr-1.5 h-3.5 w-3.5" /> Open verification page
                </a>
              </Button>
            </div>
          </div>
          <div className="space-y-1">
            <p className="text-sm font-medium">2. Enter this code</p>
            <div className="flex items-center gap-2">
              <Input readOnly value={status.user_code || ""} className="font-mono text-base tracking-[0.2em]" />
              <Button size="icon" variant="outline" onClick={copyCode} aria-label="Copy code">
                <Copy className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="rounded-md border bg-muted/50 px-3 py-2 text-xs text-muted-foreground space-y-2">
        <p className="text-sm text-foreground">Sign in with your GitHub account to mint a short-lived Copilot session token.</p>
        <div className="space-y-2">
          <label className="block text-xs font-medium text-foreground">GitHub Enterprise URL/domain (optional)</label>
          <Input
            placeholder="github.example.com"
            value={enterpriseDomain}
            onChange={(e) => setEnterpriseDomain(e.target.value)}
            className="text-base md:text-sm"
          />
        </div>
      </div>
      <Button size="sm" onClick={handleStart} disabled={starting} className="gap-1.5">
        {starting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <ExternalLink className="h-3.5 w-3.5" />}
        {starting ? "Starting device flow…" : "Sign in with GitHub Copilot"}
      </Button>
    </div>
  );
}