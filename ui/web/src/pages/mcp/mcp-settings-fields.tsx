import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { KeyValueEditor } from "@/components/shared/key-value-editor";
import type { MCPFormData } from "@/schemas/mcp.schema";

/** Env var keys whose values should be masked in the form. */
const SENSITIVE_ENV_RE = /^.*(key|secret|token|password|credential).*$/i;
export const isSensitiveEnv = (key: string) => SENSITIVE_ENV_RE.test(key.trim());

interface McpSettingsFieldsProps {
  form: UseFormReturn<MCPFormData>;
}

/** Renders env vars, tool prefix, timeout, enabled, and requireUserCredentials fields. */
export function McpSettingsFields({ form }: McpSettingsFieldsProps) {
  const { t } = useTranslation("mcp");
  const { watch, setValue } = form;
  const env = watch("env") as Record<string, string>;
  const toolPrefix = watch("toolPrefix");
  const timeout = watch("timeout");
  const name = watch("name");
  const enabled = watch("enabled");
  const requireUserCreds = watch("requireUserCreds");
  const healthFailThreshold = watch("healthFailThreshold") ?? 0;
  const healthCheckInterval = watch("healthCheckInterval") ?? 0;
  const maxReconnectAttempts = watch("maxReconnectAttempts") ?? 0;
  const reconnectCooldown = watch("reconnectCooldown") ?? 0;

  return (
    <>
      <div className="grid gap-1.5">
        <Label>{t("form.env")}</Label>
        <KeyValueEditor
          value={env}
          onChange={(v) => setValue("env", v)}
          keyPlaceholder={t("form.envKeyPlaceholder")}
          valuePlaceholder={t("form.envValuePlaceholder")}
          addLabel={t("form.addVariable")}
          maskValue={isSensitiveEnv}
        />
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="mcp-prefix">{t("form.toolPrefix")}</Label>
        <div className="flex">
          <span className="inline-flex items-center px-2.5 rounded-l-md border border-r-0 border-input bg-muted text-muted-foreground text-sm font-mono">
            mcp_
          </span>
          <Input
            id="mcp-prefix"
            value={toolPrefix}
            onChange={(e) => setValue("toolPrefix", e.target.value.replace(/[^a-z0-9_]/g, ""))}
            placeholder={name.replace(/-/g, "_") || "auto"}
            className="rounded-l-none font-mono"
          />
        </div>
        <p className="text-xs text-muted-foreground">
          {t("form.toolPrefixHint")} Tools:{" "}
          <code className="text-2xs">mcp_&#123;prefix&#125;__&#123;tool&#125;</code>
        </p>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="mcp-timeout">{t("form.timeout")}</Label>
        <Input
          id="mcp-timeout"
          type="number"
          value={timeout}
          onChange={(e) => setValue("timeout", Number(e.target.value))}
          min={1}
        />
      </div>

      <div className="flex items-center gap-2">
        <Switch
          id="mcp-enabled"
          checked={enabled}
          onCheckedChange={(v) => setValue("enabled", v)}
        />
        <Label htmlFor="mcp-enabled">{t("form.enabled")}</Label>
      </div>

      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <Switch
            id="mcp-require-creds"
            checked={requireUserCreds}
            onCheckedChange={(v) => setValue("requireUserCreds", v)}
          />
          <Label htmlFor="mcp-require-creds">{t("form.requireUserCredentials")}</Label>
        </div>
        <p className="text-xs text-muted-foreground pl-9">{t("form.requireUserCredentialsHint")}</p>
      </div>

      <div className="space-y-3 pt-2">
        <Label className="text-sm font-medium">{t("form.healthTitle")}</Label>
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1">
            <Label htmlFor="mcp-hft" className="text-xs">{t("form.healthFailThreshold")}</Label>
            <Input
              id="mcp-hft"
              type="number"
              min={0}
              placeholder="0"
              value={healthFailThreshold || ""}
              onChange={(e) => setValue("healthFailThreshold", Number(e.target.value) || 0)}
              className="text-base md:text-sm"
            />
            <p className="text-[10px] text-muted-foreground">{t("form.healthFailThresholdHint")}</p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="mcp-hci" className="text-xs">{t("form.healthCheckInterval")}</Label>
            <Input
              id="mcp-hci"
              type="number"
              min={0}
              placeholder="0"
              value={healthCheckInterval || ""}
              onChange={(e) => setValue("healthCheckInterval", Number(e.target.value) || 0)}
              className="text-base md:text-sm"
            />
            <p className="text-[10px] text-muted-foreground">{t("form.healthCheckIntervalHint")}</p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="mcp-mra" className="text-xs">{t("form.maxReconnectAttempts")}</Label>
            <Input
              id="mcp-mra"
              type="number"
              min={0}
              placeholder="0"
              value={maxReconnectAttempts || ""}
              onChange={(e) => setValue("maxReconnectAttempts", Number(e.target.value) || 0)}
              className="text-base md:text-sm"
            />
            <p className="text-[10px] text-muted-foreground">{t("form.maxReconnectAttemptsHint")}</p>
          </div>
          <div className="space-y-1">
            <Label htmlFor="mcp-rc" className="text-xs">{t("form.reconnectCooldown")}</Label>
            <Input
              id="mcp-rc"
              type="number"
              min={0}
              placeholder="0"
              value={reconnectCooldown || ""}
              onChange={(e) => setValue("reconnectCooldown", Number(e.target.value) || 0)}
              className="text-base md:text-sm"
            />
            <p className="text-[10px] text-muted-foreground">{t("form.reconnectCooldownHint")}</p>
          </div>
        </div>
      </div>
    </>
  );
}
