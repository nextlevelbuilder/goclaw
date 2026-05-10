import { useTranslation } from "react-i18next";
import { Plus, Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { CliCredentialGrantEnvSection } from "./cli-credential-grant-env-section";
import type { GrantEnvState } from "./cli-credential-grant-env-section";
import type { AgentData } from "@/types/agent";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  binary: SecureCLIBinary;
  agents: AgentData[];
  agentId: string;
  setAgentId: (v: string) => void;
  denyArgs: string;
  setDenyArgs: (v: string) => void;
  denyVerbose: string;
  setDenyVerbose: (v: string) => void;
  timeout: string;
  setTimeout: (v: string) => void;
  tips: string;
  setTips: (v: string) => void;
  enabled: boolean;
  setEnabled: (v: boolean) => void;
  /** Per-grant env override state */
  envState: GrantEnvState;
  setEnvState: (next: GrantEnvState) => void;
  /** Grant ID when editing (null when creating) */
  editingGrantId: string | null;
  /** Whether the existing grant already has encrypted env */
  initialEnvSet: boolean;
  /** Key names of existing grant env (for masked display) */
  initialEnvKeys: string[];
  /** Keys rejected by last PUT (shown as errors) */
  rejectedKeys?: string[];
  isEditing: boolean;
  saving: boolean;
  onSubmit: () => void;
  onCancel: () => void;
}

/** Inline form for adding or editing a per-agent grant. */
export function CliCredentialGrantForm({
  binary, agents, agentId, setAgentId,
  denyArgs, setDenyArgs, denyVerbose, setDenyVerbose,
  timeout, setTimeout, tips, setTips,
  enabled, setEnabled,
  envState, setEnvState,
  editingGrantId, initialEnvSet, initialEnvKeys, rejectedKeys,
  isEditing, saving,
  onSubmit, onCancel,
}: Props) {
  const { t } = useTranslation("cli-credentials");
  const { t: tc } = useTranslation("common");

  return (
    <div className="space-y-3 rounded-md border p-3">
      <div className="flex items-center justify-between">
        <Label className="text-sm font-medium">
          {isEditing ? t("grants.editGrant") : t("grants.addGrant")}
        </Label>
        {isEditing && (
          <Button variant="ghost" size="sm" onClick={onCancel} className="h-6 px-2 text-xs text-muted-foreground">
            {tc("cancel")}
          </Button>
        )}
      </div>

      <div className="grid gap-3">
        <Select value={agentId} onValueChange={setAgentId} disabled={isEditing}>
          <SelectTrigger className="text-base md:text-sm">
            <SelectValue placeholder={t("grants.selectAgent")} />
          </SelectTrigger>
          <SelectContent>
            {agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                <span>{a.display_name || a.agent_key}</span>
                <span className="ml-2 text-xs text-muted-foreground">{a.agent_key}</span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>

        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">{t("grants.overrideDenyArgs")}</Label>
          <Input
            value={denyArgs}
            onChange={(e) => setDenyArgs(e.target.value)}
            placeholder={binary.deny_args?.join(", ") || t("grants.defaultPlaceholder")}
            className="text-base md:text-sm"
          />
        </div>

        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">{t("grants.overrideDenyVerbose")}</Label>
          <Input
            value={denyVerbose}
            onChange={(e) => setDenyVerbose(e.target.value)}
            placeholder={binary.deny_verbose?.join(", ") || t("grants.defaultPlaceholder")}
            className="text-base md:text-sm"
          />
        </div>

        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">{t("grants.overrideTimeout")}</Label>
          <Input
            type="number"
            min={1}
            value={timeout}
            onChange={(e) => setTimeout(e.target.value)}
            placeholder={`${binary.timeout_seconds} (${t("grants.defaultPlaceholder")})`}
            className="text-base md:text-sm"
          />
        </div>

        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">{t("grants.overrideTips")}</Label>
          <Textarea
            value={tips}
            onChange={(e) => setTips(e.target.value)}
            placeholder={binary.tips || t("grants.defaultPlaceholder")}
            rows={2}
            className="text-base md:text-sm resize-none"
          />
        </div>

        <div className="flex items-center gap-2">
          <Switch id="grant-enabled" checked={enabled} onCheckedChange={setEnabled} />
          <Label htmlFor="grant-enabled">{tc("enabled")}</Label>
        </div>

        {/* Per-grant env override — Phase 7 */}
        <CliCredentialGrantEnvSection
          binaryId={binary.id}
          grantId={editingGrantId}
          initialEnvSet={initialEnvSet}
          initialEnvKeys={initialEnvKeys}
          state={envState}
          onChange={setEnvState}
          rejectedKeys={rejectedKeys}
        />
      </div>

      <Button size="sm" onClick={onSubmit} disabled={saving || !agentId} className="gap-1">
        {isEditing ? (
          <><Pencil className="h-3.5 w-3.5" /> {t("grants.update")}</>
        ) : (
          <><Plus className="h-3.5 w-3.5" /> {t("grants.grant")}</>
        )}
      </Button>
    </div>
  );
}
