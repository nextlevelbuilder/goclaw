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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useAuthStore } from "@/stores/use-auth-store";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useChannelInstances } from "@/pages/channels/hooks/use-channel-instances";
import type { WebhookData, WebhookKind, WebhookCreateInput, WebhookUpdateInput } from "@/types/webhook";

const NONE = "__none__";

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

        <div className="space-y-4">
          {/* Name */}
          <div className="space-y-1.5">
            <Label htmlFor="wh-name">{t("form.name")}</Label>
            <Input
              id="wh-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("form.namePlaceholder")}
              maxLength={100}
              className="text-base md:text-sm"
            />
          </div>

          {/* Kind (immutable on edit) */}
          <div className="space-y-1.5">
            <Label htmlFor="wh-kind">{t("form.kind")}</Label>
            <Select value={kind} onValueChange={(v) => setKind(v as WebhookKind)} disabled={isEdit}>
              <SelectTrigger id="wh-kind" className="text-base md:text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="llm">{t("kind.llm")}</SelectItem>
                <SelectItem value="message" disabled={!isStandard}>
                  {t("kind.message")}
                  {!isStandard ? ` — ${t("form.messageStandardOnly")}` : ""}
                </SelectItem>
              </SelectContent>
            </Select>
          </div>

          {/* Agent (llm) — immutable on edit */}
          {kind === "llm" && (
            <div className="space-y-1.5">
              <Label htmlFor="wh-agent">{t("form.agent")}</Label>
              <Select value={agentId} onValueChange={setAgentId} disabled={isEdit}>
                <SelectTrigger id="wh-agent" className="text-base md:text-sm">
                  <SelectValue placeholder={t("form.agentPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t("form.none")}</SelectItem>
                  {agents.map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.display_name || a.agent_key}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">{t("form.agentHint")}</p>
            </div>
          )}

          {/* Channel (message) */}
          {kind === "message" && (
            <div className="space-y-1.5">
              <Label htmlFor="wh-channel">{t("form.channel")}</Label>
              <Select value={channelId} onValueChange={setChannelId}>
                <SelectTrigger id="wh-channel" className="text-base md:text-sm">
                  <SelectValue placeholder={t("form.channelPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>{t("form.channelAny")}</SelectItem>
                  {instances.map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      {c.display_name || c.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {/* Rate limit */}
          <div className="space-y-1.5">
            <Label htmlFor="wh-rate">{t("form.rateLimit")}</Label>
            <Input
              id="wh-rate"
              type="number"
              min={0}
              value={rateLimit}
              onChange={(e) => setRateLimit(e.target.value)}
              className="text-base md:text-sm"
            />
          </div>

          {/* IP allowlist */}
          <div className="space-y-1.5">
            <Label htmlFor="wh-ips">{t("form.ipAllowlist")}</Label>
            <Input
              id="wh-ips"
              value={ipAllowlist}
              onChange={(e) => setIpAllowlist(e.target.value)}
              placeholder={t("form.ipAllowlistPlaceholder")}
              className="text-base md:text-sm"
            />
            <p className="text-xs text-muted-foreground">{t("form.ipAllowlistHint")}</p>
          </div>

          {/* Toggles */}
          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="wh-hmac">{t("form.requireHmac")}</Label>
              <p className="text-xs text-muted-foreground">{t("form.requireHmacHint")}</p>
            </div>
            <Switch id="wh-hmac" checked={requireHmac} onCheckedChange={setRequireHmac} />
          </div>

          <div className="flex items-center justify-between">
            <div>
              <Label htmlFor="wh-localhost">{t("form.localhostOnly")}</Label>
              <p className="text-xs text-muted-foreground">
                {isStandard ? t("form.localhostOnlyHint") : t("form.localhostOnlyLite")}
              </p>
            </div>
            <Switch
              id="wh-localhost"
              checked={localhostOnly}
              onCheckedChange={setLocalhostOnly}
              disabled={!isStandard}
            />
          </div>
        </div>

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
