import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck, ShieldX, Loader2, ExternalLink } from "lucide-react";
import { useWsEvent } from "@/hooks/use-ws-event";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { MCPServerData, MCPOAuthStatus } from "./hooks/use-mcp";

// MCPOAuthDialog manages the SERVER-LEVEL (global) OAuth token only — it always
// operates on user_id="" (no per-user scope). Per-user OAuth is handled by
// MCPUserCredentialsDialog.
interface MCPOAuthDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: MCPServerData;
  onStartOAuth: (serverId: string, mcpUrl: string, userId?: string) => Promise<{ auth_url: string; state: string; client_id: string; issuer: string; completed?: boolean }>;
  onGetStatus: (serverId: string, userId?: string) => Promise<MCPOAuthStatus>;
  onRevoke: (serverId: string, userId?: string) => Promise<void>;
}

export function MCPOAuthDialog({
  open,
  onOpenChange,
  server,
  onStartOAuth,
  onGetStatus,
  onRevoke,
}: MCPOAuthDialogProps) {
  const { t } = useTranslation("mcp");
  const [status, setStatus] = useState<MCPOAuthStatus | null>(null);
  const [authorizing, setAuthorizing] = useState(false);
  const [revoking, setRevoking] = useState(false);
  const [error, setError] = useState("");
  const popupRef = useRef<Window | null>(null);

  const loadStatus = useCallback(async () => {
    try {
      // Global/server token only — no user_id.
      const s = await onGetStatus(server.id);
      setStatus(s);
    } catch {
      setStatus(null);
    }
  }, [server.id, onGetStatus]);

  useEffect(() => {
    if (open) {
      setError("");
      loadStatus();
    }
  }, [open, loadStatus]);

  // Close popup on unmount.
  useEffect(() => {
    return () => { popupRef.current?.close(); };
  }, []);

  // Primary notification path: WebSocket event from backend after token exchange.
  // Fires for both success and error when the OAuth callback is processed.
  useWsEvent("mcp.oauth_complete", (payload: unknown) => {
    const p = payload as { serverId?: string; userId?: string; status?: string; error?: string };
    if (p.serverId !== server.id) return;
    // Only react to the global (server-level) token event.
    if ((p.userId ?? "") !== "") return;
    popupRef.current?.close();
    popupRef.current = null;
    setAuthorizing(false);
    if (p.status === "success") {
      loadStatus();
    } else {
      setError(p.error ?? t("form.oauth.authFailed"));
    }
  });

  const handleAuthorize = async () => {
    if (!server.url) {
      setError("Server has no URL configured");
      return;
    }
    setAuthorizing(true);
    setError("");
    try {
      const { auth_url, completed } = await onStartOAuth(server.id, server.url);

      // client_credentials grant completes server-side with no browser redirect.
      if (completed || !auth_url) {
        await loadStatus();
        setAuthorizing(false);
        return;
      }

      const popup = window.open(auth_url, "mcp-oauth", "width=600,height=700,menubar=no,toolbar=no");
      popupRef.current = popup;
    } catch (err) {
      setError(err instanceof Error ? err.message : t("form.oauth.authFailed"));
      setAuthorizing(false);
    }
  };

  const handleRevoke = async () => {
    setRevoking(true);
    setError("");
    try {
      await onRevoke(server.id);
      await loadStatus();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Revoke failed");
    } finally {
      setRevoking(false);
    }
  };

  const hasToken = status?.has_token ?? false;
  const isExpired = status?.expired ?? false;

  return (
    <Dialog open={open} onOpenChange={(v) => {
      if (!v && authorizing) {
        popupRef.current?.close();
        popupRef.current = null;
        setAuthorizing(false);
      }
      if (!revoking) onOpenChange(v);
    }}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheck className="h-4 w-4 text-primary" />
            {t("form.oauth.title")}
          </DialogTitle>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Server info */}
          <div className="text-sm text-muted-foreground">
            <span className="font-medium text-foreground">{server.display_name || server.name}</span>
            {server.url && <span className="ml-1 font-mono text-xs truncate block">{server.url}</span>}
          </div>

          {/* Auth mode — this dialog always manages the server-level (global) token */}
          <p className="text-xs text-muted-foreground">
            {t("form.oauth.modeGlobal")}
          </p>

          {/* Token status */}
          <div className="rounded-md border p-3 space-y-1">
            {status === null ? (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading...
              </div>
            ) : hasToken ? (
              <div className="space-y-1">
                <div className="flex items-center gap-2 text-sm">
                  {isExpired
                    ? <ShieldX className="h-3.5 w-3.5 text-amber-500" />
                    : <ShieldCheck className="h-3.5 w-3.5 text-emerald-500" />}
                  <span className={isExpired ? "text-amber-600 dark:text-amber-400" : "text-emerald-600 dark:text-emerald-400"}>
                    {isExpired ? t("form.oauth.expired") : t("form.oauth.authorized")}
                  </span>
                </div>
                {status.client_id && (
                  <p className="text-xs text-muted-foreground font-mono">
                    client: {status.client_id}
                  </p>
                )}
                {status.expires_at && (
                  <p className="text-xs text-muted-foreground">
                    expires: {new Date(status.expires_at).toLocaleString()}
                  </p>
                )}
              </div>
            ) : (
              <div className="flex items-center gap-2 text-sm text-muted-foreground">
                <ShieldX className="h-3.5 w-3.5" />
                {t("form.oauth.notAuthorized")}
              </div>
            )}
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter className="flex-row gap-2 sm:justify-between">
          {hasToken && !authorizing && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleRevoke}
              disabled={revoking}
              className="text-destructive hover:text-destructive"
            >
              {revoking ? <Loader2 className="h-3.5 w-3.5 animate-spin mr-1" /> : null}
              {t("form.oauth.revoke")}
            </Button>
          )}
          <div className="flex gap-2 ml-auto">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={revoking}>
              {t("form.cancel", "Close")}
            </Button>
            <Button
              size="sm"
              onClick={handleAuthorize}
              disabled={authorizing || revoking}
              className="gap-1"
            >
              {authorizing
                ? <><Loader2 className="h-3.5 w-3.5 animate-spin" />{t("form.oauth.authorizing")}</>
                : <><ExternalLink className="h-3.5 w-3.5" />{hasToken ? t("form.oauth.reauthorize") : t("form.oauth.authorize")}</>
              }
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
