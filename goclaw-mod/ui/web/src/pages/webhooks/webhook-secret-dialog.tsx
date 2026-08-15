import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Copy, Check, ShieldAlert } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";

export interface WebhookSecretPayload {
  webhookId: string;
  secret: string;
  hmacSigningKey: string;
}

interface Props {
  payload: WebhookSecretPayload | null;
  onClose: () => void;
}

export function WebhookSecretDialog({ payload, onClose }: Props) {
  const { t } = useTranslation("webhooks");
  const [copiedKey, setCopiedKey] = useState<string | null>(null);

  const copy = async (value: string, field: string) => {
    await navigator.clipboard.writeText(value);
    setCopiedKey(field);
    setTimeout(() => setCopiedKey((k) => (k === field ? null : k)), 2000);
  };

  const Row = ({ label, value, field }: { label: string; value: string; field: string }) => (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      <div className="flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded bg-muted px-3 py-2 text-base md:text-sm font-mono break-all">
          {value}
        </code>
        <Button variant="outline" size="sm" onClick={() => copy(value, field)} className="gap-1 shrink-0">
          {copiedKey === field ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
          {copiedKey === field ? t("secret.copied") : t("secret.copy")}
        </Button>
      </div>
    </div>
  );

  return (
    <Dialog open={!!payload} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("secret.title")}</DialogTitle>
          <DialogDescription>{t("secret.description")}</DialogDescription>
        </DialogHeader>

        <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm">
          <ShieldAlert className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
          <span>{t("secret.warning")}</span>
        </div>

        {payload && (
          <div className="space-y-4">
            <Row label={t("secret.webhookId")} value={payload.webhookId} field="webhookId" />
            <Row label={t("secret.bearer")} value={payload.secret} field="secret" />
            <Row label={t("secret.hmacKey")} value={payload.hmacSigningKey} field="hmac" />
            <p className="text-xs text-muted-foreground">{t("secret.usageHint")}</p>
          </div>
        )}

        <DialogFooter>
          <Button onClick={onClose}>{t("secret.done")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
