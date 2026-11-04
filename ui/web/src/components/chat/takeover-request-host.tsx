import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Hand } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Methods, Events } from "@/api/protocol";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useWsCall } from "@/hooks/use-ws-call";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";
import {
  dropTakeover,
  enqueueTakeover,
  formatCountdown,
  isTakeoverExpired,
  isTakeoverForUser,
  parseTakeoverRequest,
  secondsLeft,
  type TakeoverRequest,
} from "./takeover-request-state";

/**
 * Card shown when the agent asks the user to take over the browser (SSO,
 * CAPTCHA, QR, OTP). It is a non-modal card, not a dialog, so the user can work
 * in the browser while it stays visible. Nothing typed in the browser reaches
 * this component: it only reports "done" or "cancel".
 */
export function TakeoverRequestHost() {
  const { t } = useTranslation("browser-takeover");
  const userId = useAuthStore((s) => s.userId);
  const [queue, setQueue] = useState<TakeoverRequest[]>([]);
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const done = useWsCall(Methods.BROWSER_TAKEOVER_DONE);
  const cancel = useWsCall(Methods.BROWSER_TAKEOVER_CANCEL);

  const current = queue[0] ?? null;

  useWsEvent(Events.BROWSER_TAKEOVER_REQUESTED, (payload) => {
    const req = parseTakeoverRequest(payload);
    if (!req || !isTakeoverForUser(req, userId) || isTakeoverExpired(req)) return;
    setQueue((q) => enqueueTakeover(q, req));
  });

  useWsEvent(Events.BROWSER_TAKEOVER_RESOLVED, (payload) => {
    const id = (payload as { id?: unknown } | null)?.id;
    if (typeof id !== "string") return;
    setQueue((q) => dropTakeover(q, id));
  });

  // Tick once a second for the countdown and drop the request once it expires.
  useEffect(() => {
    if (!current) return;
    const timer = setInterval(() => {
      const t0 = Date.now();
      setNow(t0);
      if (isTakeoverExpired(current, t0)) setQueue((q) => dropTakeover(q, current.id));
    }, 1000);
    return () => clearInterval(timer);
  }, [current]);

  if (!current) return null;

  const answer = async (finish: boolean) => {
    const id = current.id;
    setBusy(true);
    try {
      await (finish ? done : cancel).call({ id });
    } catch {
      toast.error(t("failed"), t("expired"));
    } finally {
      setBusy(false);
      setQueue((q) => dropTakeover(q, id));
    }
  };

  return (
    <div
      role="alert"
      className="safe-bottom fixed inset-x-0 bottom-0 z-50 border-t bg-background p-4 shadow-lg sm:inset-x-auto sm:bottom-4 sm:right-4 sm:w-96 sm:rounded-lg sm:border"
    >
      <div className="flex items-center gap-2 font-medium">
        <Hand className="h-4 w-4" />
        {t("title")}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">{t("description")}</p>
      <dl className="mt-3 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
        {current.agentKey && (
          <>
            <dt className="text-muted-foreground">{t("agent")}</dt>
            <dd className="break-all">{current.agentKey}</dd>
          </>
        )}
        <dt className="text-muted-foreground">{t("host")}</dt>
        <dd className="break-all font-mono">{current.host}</dd>
        {current.reason && (
          <>
            <dt className="text-muted-foreground">{t("reason")}</dt>
            <dd className="break-words">{current.reason}</dd>
          </>
        )}
        {current.expiresAt > 0 && (
          <>
            <dt className="text-muted-foreground">{t("timeLeft")}</dt>
            <dd className="font-mono">{formatCountdown(secondsLeft(current, now))}</dd>
          </>
        )}
      </dl>
      <p className="mt-3 text-xs text-muted-foreground">{t("privacy")}</p>
      <div className="mt-3 flex justify-end gap-2">
        <Button variant="outline" disabled={busy} onClick={() => void answer(false)}>
          {t("cancel")}
        </Button>
        <Button disabled={busy} onClick={() => void answer(true)}>
          {t("done")}
        </Button>
      </div>
    </div>
  );
}
