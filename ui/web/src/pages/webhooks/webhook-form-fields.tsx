import { useTranslation } from "react-i18next";
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
import type { WebhookKind } from "@/types/webhook";

export const NONE = "__none__";

interface AgentOption {
  id: string;
  display_name?: string | null;
  agent_key: string;
}

interface ChannelOption {
  id: string;
  display_name?: string | null;
  name: string;
}

interface Props {
  isEdit: boolean;
  isStandard: boolean;
  name: string;
  setName: (value: string) => void;
  kind: WebhookKind;
  setKind: (kind: WebhookKind) => void;
  agentId: string;
  setAgentId: (id: string) => void;
  channelId: string;
  setChannelId: (id: string) => void;
  rateLimit: string;
  setRateLimit: (value: string) => void;
  ipAllowlist: string;
  setIpAllowlist: (value: string) => void;
  requireHmac: boolean;
  setRequireHmac: (value: boolean) => void;
  localhostOnly: boolean;
  setLocalhostOnly: (value: boolean) => void;
  agents: AgentOption[];
  instances: ChannelOption[];
}

export function WebhookFormFields({
  isEdit,
  isStandard,
  name,
  setName,
  kind,
  setKind,
  agentId,
  setAgentId,
  channelId,
  setChannelId,
  rateLimit,
  setRateLimit,
  ipAllowlist,
  setIpAllowlist,
  requireHmac,
  setRequireHmac,
  localhostOnly,
  setLocalhostOnly,
  agents,
  instances,
}: Props) {
  const { t } = useTranslation("webhooks");

  return (
    <div className="space-y-4">
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
              {!isStandard ? ` - ${t("form.messageStandardOnly")}` : ""}
            </SelectItem>
          </SelectContent>
        </Select>
      </div>

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
  );
}
