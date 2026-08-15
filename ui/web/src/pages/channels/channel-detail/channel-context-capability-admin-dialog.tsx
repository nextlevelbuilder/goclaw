import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, Loader2, ShieldCheck, ShieldX, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import type { ChannelCapability, ChannelContextData } from "@/types/channel";
import type { ChannelCredentialPayload } from "../hooks/use-channel-detail";

export interface ChannelContextCapabilityTarget {
  context: ChannelContextData;
  capability: ChannelCapability;
}

interface ChannelContextCapabilityAdminDialogProps {
  target: ChannelContextCapabilityTarget | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onGrant: (target: ChannelContextCapabilityTarget) => Promise<void>;
  onRevoke: (target: ChannelContextCapabilityTarget) => Promise<void>;
  onSaveCredentials: (target: ChannelContextCapabilityTarget, payload: ChannelCredentialPayload) => Promise<void>;
  onDeleteCredentials: (target: ChannelContextCapabilityTarget) => Promise<void>;
}

function parseEnvJSON(value: string): Record<string, string> | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  const parsed = JSON.parse(trimmed) as Record<string, unknown>;
  const env: Record<string, string> = {};
  for (const [key, raw] of Object.entries(parsed)) {
    if (typeof raw === "string") {
      env[key] = raw;
    }
  }
  return env;
}

export function ChannelContextCapabilityAdminDialog({
  target,
  open,
  onOpenChange,
  onGrant,
  onRevoke,
  onSaveCredentials,
  onDeleteCredentials,
}: ChannelContextCapabilityAdminDialogProps) {
  const { t } = useTranslation("channels");
  const [apiKey, setApiKey] = useState("");
  const [envText, setEnvText] = useState("");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState<"grant" | "revoke" | "credentials" | "deleteCredentials" | null>(null);

  useEffect(() => {
    if (open) {
      setApiKey("");
      setEnvText("");
      setError("");
      setSaving(null);
    }
  }, [open, target]);

  if (!target) return null;

  const { capability } = target;
  const isMCP = capability.type === "mcp_server";

  const run = async (
    action: "grant" | "revoke" | "credentials" | "deleteCredentials",
    task: () => Promise<void>,
  ) => {
    setSaving(action);
    setError("");
    try {
      await task();
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("detail.contexts.actionFailed"));
    } finally {
      setSaving(null);
    }
  };

  const saveCredentials = async () => {
    let env: Record<string, string> | undefined;
    try {
      env = parseEnvJSON(envText);
    } catch {
      setError(t("detail.contexts.invalidEnv"));
      return;
    }
    if (!isMCP && !env) {
      setError(t("detail.contexts.envRequired"));
      return;
    }
    if (isMCP && !env && !apiKey.trim()) {
      setError(t("detail.contexts.credentialRequired"));
      return;
    }
    await run("credentials", () => onSaveCredentials(target, { apiKey: apiKey.trim() || undefined, env }));
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!saving) onOpenChange(next); }}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("detail.contexts.manageCapability")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          <div className="rounded-md border bg-muted/30 px-3 py-2">
            <div className="text-sm font-medium">{capability.display_name || capability.name}</div>
            <div className="font-mono text-xs text-muted-foreground">{capability.name}</div>
          </div>

          <div className="grid grid-cols-2 gap-2">
            <Button
              variant="outline"
              onClick={() => run("grant", () => onGrant(target))}
              disabled={!!saving}
            >
              {saving === "grant" ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldCheck className="h-4 w-4" />}
              {t("detail.contexts.grant")}
            </Button>
            <Button
              variant="outline"
              onClick={() => run("revoke", () => onRevoke(target))}
              disabled={!!saving || !capability.context_grant_configured}
            >
              {saving === "revoke" ? <Loader2 className="h-4 w-4 animate-spin" /> : <ShieldX className="h-4 w-4" />}
              {t("detail.contexts.revoke")}
            </Button>
          </div>

          <div className="space-y-2">
            <div className="text-sm font-medium">{t("detail.contexts.scopedCredential")}</div>
            {isMCP && (
              <Input
                type="password"
                value={apiKey}
                onChange={(event) => setApiKey(event.target.value)}
                placeholder={t("detail.contexts.apiKeyPlaceholder")}
              />
            )}
            <Textarea
              value={envText}
              onChange={(event) => setEnvText(event.target.value)}
              placeholder={t("detail.contexts.envPlaceholder")}
              size="lg"
            />
            <p className="text-xs text-muted-foreground">{t("detail.contexts.secretHint")}</p>
          </div>

          {error && <p className="text-sm text-destructive">{error}</p>}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <Button
            variant="destructive"
            onClick={() => run("deleteCredentials", () => onDeleteCredentials(target))}
            disabled={!!saving || !capability.context_credentials_configured}
          >
            {saving === "deleteCredentials" ? <Loader2 className="h-4 w-4 animate-spin" /> : <Trash2 className="h-4 w-4" />}
            {t("detail.contexts.deleteCredential")}
          </Button>
          <Button onClick={saveCredentials} disabled={!!saving}>
            {saving === "credentials" ? <Loader2 className="h-4 w-4 animate-spin" /> : <KeyRound className="h-4 w-4" />}
            {t("detail.contexts.saveCredential")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
