import { useState, useEffect } from "react";
import { useHttp } from "@/hooks/use-ws";
import { BrowserViewer } from "@/pages/browser/browser-viewer";

interface BrowserLiveModalProps {
  open: boolean;
  onClose: () => void;
  targetId: string;
  tabTitle?: string;
  tabUrl?: string;
}

export function BrowserLiveModal({
  open,
  onClose,
  targetId,
  tabTitle,
  tabUrl,
}: BrowserLiveModalProps) {
  const http = useHttp();
  const [token, setToken] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open || !targetId) {
      setToken(null);
      setError(null);
      return;
    }

    let cancelled = false;
    http
      .post<{ token: string }>("/browser/live", {
        targetId,
        mode: "takeover",
      })
      .then((res) => {
        if (!cancelled) setToken(res.token);
      })
      .catch((err) => {
        if (!cancelled) setError(err.message ?? "Failed to create live session");
      });

    return () => {
      cancelled = true;
    };
  }, [open, targetId, http]);

  if (!open) return null;

  return (
    <div className="fixed inset-0 z-50 flex flex-col bg-background">
      {error ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3">
          <p className="text-sm text-destructive">{error}</p>
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border px-3 py-1.5 text-sm hover:bg-accent"
          >
            Close
          </button>
        </div>
      ) : token ? (
        <BrowserViewer
          token={token}
          initialMode="takeover"
          onClose={onClose}
          tabTitle={tabTitle}
          tabUrl={tabUrl}
          className="h-full"
        />
      ) : (
        <div className="flex flex-1 items-center justify-center">
          <div className="flex items-center gap-2 text-muted-foreground">
            <div className="h-5 w-5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
            <span className="text-sm">Creating live session...</span>
          </div>
        </div>
      )}
    </div>
  );
}
