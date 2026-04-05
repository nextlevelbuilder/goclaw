import { useParams } from "react-router";
import { useEffect, useState } from "react";
import { BrowserViewer } from "./browser-viewer";

type ShareState = "loading" | "valid" | "expired" | "not_found" | "error";

export function BrowserSharePage() {
  const { token } = useParams<{ token: string }>();
  const [state, setState] = useState<ShareState>("loading");
  const [mode, setMode] = useState<"view" | "takeover">("view");

  useEffect(() => {
    if (!token) {
      setState("not_found");
      return;
    }

    // Fetch session info (mode, expiresAt) from the public JSON endpoint.
    fetch(`/browser/live/${token}/info`)
      .then((res) => {
        if (res.ok) {
          return res.json().then((data: { mode?: string }) => {
            if (data.mode === "takeover") {
              setMode("takeover");
            }
            setState("valid");
          });
        } else if (res.status === 410) {
          setState("expired");
        } else {
          setState("not_found");
        }
      })
      .catch(() => {
        setState("error");
      });
  }, [token]);

  if (state === "loading") {
    return (
      <div className="flex h-dvh items-center justify-center bg-neutral-900">
        <div className="flex flex-col items-center gap-2 text-white">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-white border-t-transparent" />
          <span className="text-sm">Validating session...</span>
        </div>
      </div>
    );
  }

  if (state === "expired") {
    return (
      <div className="flex h-dvh items-center justify-center bg-neutral-900">
        <div className="flex flex-col items-center gap-3 text-white">
          <span className="text-4xl">&#9203;</span>
          <h1 className="text-lg font-semibold">Session Expired</h1>
          <p className="text-sm text-neutral-400">
            This live view session has expired. Request a new share link.
          </p>
        </div>
      </div>
    );
  }

  if (state === "not_found" || state === "error") {
    return (
      <div className="flex h-dvh items-center justify-center bg-neutral-900">
        <div className="flex flex-col items-center gap-3 text-white">
          <span className="text-4xl">&#128683;</span>
          <h1 className="text-lg font-semibold">Session Not Found</h1>
          <p className="text-sm text-neutral-400">
            This live view session does not exist or the link is invalid.
          </p>
        </div>
      </div>
    );
  }

  return (
    <BrowserViewer
      token={token!}
      initialMode={mode}
      className="h-dvh w-full"
      showHeader={true}
    />
  );
}
