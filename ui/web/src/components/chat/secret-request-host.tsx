import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Lock } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Methods, Events } from "@/api/protocol";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useWsCall } from "@/hooks/use-ws-call";
import { useAuthStore } from "@/stores/use-auth-store";
import { toast } from "@/stores/use-toast-store";
import type { SecretRequest } from "@/types/secret-vault";
import {
  dropRequest,
  enqueueRequest,
  isExpired,
  isForUser,
  parseSecretRequest,
} from "./secret-request-state";

/**
 * Secure Form / approval dialog for the secret vault. It is deliberately
 * outside the chat message list and store: the typed value lives only in this
 * component's state, goes straight to the gateway over WebSocket and is wiped
 * the moment it is submitted or the dialog closes.
 */
export function SecretRequestHost() {
  const { t } = useTranslation("secret-vault");
  const userId = useAuthStore((s) => s.userId);
  const [queue, setQueue] = useState<SecretRequest[]>([]);
  const [username, setUsername] = useState("");
  const [secret, setSecret] = useState("");
  const [scope, setScope] = useState<"save" | "session">("save");
  const [busy, setBusy] = useState(false);

  const submit = useWsCall(Methods.SECRET_VAULT_FORM_SUBMIT);
  const cancel = useWsCall(Methods.SECRET_VAULT_FORM_CANCEL);
  const respond = useWsCall(Methods.SECRET_VAULT_APPROVAL_RESPOND);

  const current = queue[0] ?? null;

  const wipe = useCallback(() => {
    setUsername("");
    setSecret("");
    setScope("save");
  }, []);

  useWsEvent(Events.SECRET_VAULT_FORM_REQUESTED, (payload) => {
    const req = parseSecretRequest(payload);
    if (!req || !isForUser(req, userId) || isExpired(req)) return;
    setQueue((q) => enqueueRequest(q, req));
  });

  useWsEvent(Events.SECRET_VAULT_FORM_RESOLVED, (payload) => {
    const id = (payload as { id?: unknown } | null)?.id;
    if (typeof id !== "string") return;
    setQueue((q) => dropRequest(q, id));
  });

  // Reset the inputs whenever a different request becomes current.
  const currentId = current?.id;
  useEffect(() => {
    wipe();
  }, [currentId, wipe]);

  // Drop the request once it expires.
  useEffect(() => {
    if (!current || current.expiresAt <= 0) return;
    const ms = current.expiresAt - Date.now();
    const timer = setTimeout(() => setQueue((q) => dropRequest(q, current.id)), Math.max(ms, 0));
    return () => clearTimeout(timer);
  }, [current]);

  if (!current) return null;

  const isApproval = current.kind === "approval";
  const isLogin = current.kind === "login";
  const canSubmit = secret !== "" && (!isLogin || username !== "");

  const finish = (id: string) => {
    wipe();
    setQueue((q) => dropRequest(q, id));
  };

  const handleSubmit = async () => {
    if (!canSubmit || busy) return;
    const id = current.id;
    const params = {
      id,
      sessionKey: current.sessionKey,
      scope,
      username: isLogin ? username : undefined,
      secret,
    };
    // Clear the inputs before the round trip: the value must not outlive the call.
    wipe();
    setBusy(true);
    try {
      await submit.call(params);
      finish(id);
    } catch {
      toast.error(t("request.failed"), t("request.expired"));
      finish(id);
    } finally {
      setBusy(false);
    }
  };

  const handleCancel = async () => {
    const id = current.id;
    finish(id);
    try {
      await cancel.call({ id });
    } catch {
      /* already resolved or expired */
    }
  };

  const handleApproval = async (approve: boolean) => {
    const id = current.id;
    setBusy(true);
    try {
      await respond.call({ id, approve });
    } catch {
      toast.error(t("request.failed"), t("request.expired"));
    } finally {
      setBusy(false);
      finish(id);
    }
  };

  const target = current.host || current.domains?.join(", ") || "";

  return (
    <Dialog open onOpenChange={(open) => !open && void (isApproval ? handleApproval(false) : handleCancel())}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Lock className="h-4 w-4" />
            {isApproval ? t("request.approvalTitle") : t("request.formTitle")}
          </DialogTitle>
          <DialogDescription>
            {isApproval ? t("request.approvalDescription") : t("request.formDescription")}
          </DialogDescription>
        </DialogHeader>

        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
          <dt className="text-muted-foreground">{t("request.secretName")}</dt>
          <dd className="font-mono break-all">{current.name}</dd>
          {current.agentKey && (
            <>
              <dt className="text-muted-foreground">{t("request.agent")}</dt>
              <dd className="break-all">{current.agentKey}</dd>
            </>
          )}
          {target && (
            <>
              <dt className="text-muted-foreground">{t("request.host")}</dt>
              <dd className="font-mono break-all">{target}</dd>
            </>
          )}
          {current.action && (
            <>
              <dt className="text-muted-foreground">{t("request.action")}</dt>
              <dd className="break-all">{current.action}</dd>
            </>
          )}
          {current.reason && (
            <>
              <dt className="text-muted-foreground">{t("request.reason")}</dt>
              <dd className="break-words">{current.reason}</dd>
            </>
          )}
        </dl>

        {isApproval ? (
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => void handleApproval(false)}>
              {t("request.deny")}
            </Button>
            <Button disabled={busy} onClick={() => void handleApproval(true)}>
              {t("request.approve")}
            </Button>
          </DialogFooter>
        ) : (
          <form
            autoComplete="off"
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              void handleSubmit();
            }}
          >
            {isLogin && (
              <div className="space-y-1.5">
                <Label htmlFor="sr-username">{t("form.username")}</Label>
                <Input
                  id="sr-username"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoComplete="off"
                  className="text-base md:text-sm"
                />
              </div>
            )}
            <div className="space-y-1.5">
              <Label htmlFor="sr-secret">{isLogin ? t("form.password") : t("form.secret")}</Label>
              <Input
                id="sr-secret"
                type="password"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                autoComplete="new-password"
                spellCheck={false}
                className="text-base md:text-sm"
              />
            </div>

            <fieldset className="space-y-1.5">
              <legend className="text-sm font-medium">{t("request.scopeLabel")}</legend>
              {(["save", "session"] as const).map((s) => (
                <label key={s} className="flex cursor-pointer items-start gap-2 text-sm">
                  <input
                    type="radio"
                    name="sr-scope"
                    className="mt-1"
                    checked={scope === s}
                    onChange={() => setScope(s)}
                  />
                  <span>
                    {s === "save" ? t("request.scopeSave") : t("request.scopeSession")}
                    <span className="block text-xs text-muted-foreground">
                      {s === "save" ? t("request.scopeSaveHint") : t("request.scopeSessionHint")}
                    </span>
                  </span>
                </label>
              ))}
            </fieldset>

            <p className="text-xs text-muted-foreground">{t("request.notSent")}</p>

            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={() => void handleCancel()}>
                {t("request.cancel")}
              </Button>
              <Button type="submit" disabled={!canSubmit || busy}>
                {t("request.submit")}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
