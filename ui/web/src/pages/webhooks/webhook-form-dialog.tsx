import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Webhook as WebhookIcon } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { useAuthStore } from "@/stores/use-auth-store";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import { NONE, WebhookFormFields } from "./webhook-form-fields";
import type { WebhookData, WebhookKind, WebhookCreateInput, WebhookUpdateInput } from "@/types/webhook";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  editing: WebhookData | null;
  onCreate: (input: WebhookCreateInput) => Promise<void>;
  onUpdate: (id: string, input: WebhookUpdateInput) => Promise<void>;
}

export function WebhookFormDialog({ open, onOpenChange, editing, onCreate, onUpdate }: Props) {
  const { t } = useTranslation("webhooks");
  const edition = useAuthStore((s) => s.edition);
  const isStandard = edition === "standard";
  const { agents } = useAgents();
  const { instances } = useChannelInstances({ limit: 200 });

  const isEdit = !!editing;

  const [name, setName] = useState("");
  const [kind, setKind] = useState<WebhookKind>("llm");
  const [agentId, setAgentId] = useState<string>(NONE);
  const [channelId, setChannelId] = useState<string>(NONE);
  const [rateLimit, setRateLimit] = useState<string>("60");
  const [ipAllowlist, setIpAllowlist] = useState<string>("");
  const [requireHmac, setRequireHmac] = useState(false);
  const [localhostOnly, setLocalhostOnly] = useState(!isStandard);
  const [saving, setSaving] = useState(false);

  // Hydrate from the editing target (or reset to defaults on open).
  useEffect(() => {
    if (!open) return;
    if (editing) {
      setName(editing.name);
      setKind(editing.kind);
      setAgentId(editing.agent_id ?? NONE);
      setChannelId(editing.channel_id ?? NONE);
      setRateLimit(String(editing.rate_limit_per_min ?? 0));
      setIpAllowlist((editing.ip_allowlist ?? []).join(", "));
      setRequireHmac(editing.require_hmac);
      setLocalhostOnly(editing.localhost_only);
    } else {
      setName("");
      setKind("llm");
      setAgentId(NONE);
      setChannelId(NONE);
      setRateLimit("60");
      setIpAllowlist("");
      setRequireHmac(false);
      setLocalhostOnly(!isStandard);
    }
  }, [open, editing, isStandard]);

  const parseIpList = (s: string): string[] =>
    s.split(",").map((x) => x.trim()).filter(Boolean);

  const handleSubmit = async () => {
    if (!name.trim()) return;
    setSaving(true);
    try {
      const rate = parseInt(rateLimit, 10);
      if (isEdit && editing) {
        const update: WebhookUpdateInput = {
          name: name.trim(),
          channel_id: channelId === NONE ? undefined : channelId,
          rate_limit_per_min: Number.isFinite(rate) ? rate : 0,
          ip_allowlist: parseIpList(ipAllowlist),
          require_hmac: requireHmac,
          localhost_only: localhostOnly,
        };
        await onUpdate(editing.id, update);
      } else {
        const create: WebhookCreateInput = {
          name: name.trim(),
          kind,
          agent_id: kind === "llm" && agentId !== NONE ? agentId : undefined,
          channel_id: kind === "message" && channelId !== NONE ? channelId : undefined,
          rate_limit_per_min: Number.isFinite(rate) ? rate : undefined,
          ip_allowlist: parseIpList(ipAllowlist),
          require_hmac: requireHmac,
          localhost_only: localhostOnly,
        };
        await onCreate(create);
      }
      onOpenChange(false);
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <WebhookIcon className="h-5 w-5" />
            {isEdit ? t("form.editTitle") : t("form.createTitle")}
          </DialogTitle>
          <DialogDescription>{t("form.description")}</DialogDescription>
        </DialogHeader>

        <WebhookFormFields
          isEdit={isEdit}
          isStandard={isStandard}
          name={name}
          setName={setName}
          kind={kind}
          setKind={setKind}
          agentId={agentId}
          setAgentId={setAgentId}
          channelId={channelId}
          setChannelId={setChannelId}
          rateLimit={rateLimit}
          setRateLimit={setRateLimit}
          ipAllowlist={ipAllowlist}
          setIpAllowlist={setIpAllowlist}
          requireHmac={requireHmac}
          setRequireHmac={setRequireHmac}
          localhostOnly={localhostOnly}
          setLocalhostOnly={setLocalhostOnly}
          agents={agents}
          instances={instances}
        />

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            {t("form.cancel")}
          </Button>
          <Button type="button" onClick={handleSubmit} disabled={saving || !name.trim()}>
            {saving ? t("form.saving") : isEdit ? t("form.save") : t("form.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
