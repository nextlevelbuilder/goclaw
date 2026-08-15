import type { UseFormReturn } from "react-hook-form";
import { useTranslation } from "react-i18next";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { KeyValueEditor } from "@/components/shared/key-value-editor";
import type { MCPFormData } from "@/schemas/mcp.schema";

/** Env var keys whose values should be masked in the form. */
const SENSITIVE_ENV_RE = /^.*(key|secret|token|password|credential).*$/i;
export const isSensitiveEnv = (key: string) => SENSITIVE_ENV_RE.test(key.trim());

interface McpSettingsFieldsProps {
  form: UseFormReturn<MCPFormData>;
  /** Pass server id when editing — used to show "existing secret" placeholder. */
  isEditing?: boolean;
}

/** Renders env vars, OAuth config, tool prefix, timeout, enabled, requireUserCredentials fields. */
export function McpSettingsFields({ form, isEditing }: McpSettingsFieldsProps) {
  const { t } = useTranslation("mcp");
  const { watch, setValue } = form;
  const transport = watch("transport");
  const env = watch("env") as Record<string, string>;
  const toolPrefix = watch("toolPrefix");
  const timeout = watch("timeout");
  const name = watch("name");
  const enabled = watch("enabled");
  const requireUserCreds = watch("requireUserCreds");
  const toolHintsGlobal = watch("toolHintsGlobal");
  const toolHintsTools = watch("toolHintsTools") as Record<string, string>;
  const oauthEnabled = watch("oauthEnabled");
  const oauthUseDcr = watch("oauthUseDcr");
  const oauthGrantType = watch("oauthGrantType");

  const isStdio = transport === "stdio";

  return (
    <>
      <div className="grid gap-1.5">
        <Label>{t("form.env")}</Label>
        <KeyValueEditor
          value={env}
          onChange={(v) => setValue("env", v, { shouldDirty: true, shouldValidate: true })}
          keyPlaceholder={t("form.envKeyPlaceholder")}
          valuePlaceholder={t("form.envValuePlaceholder")}
          addLabel={t("form.addVariable")}
          maskValue={isSensitiveEnv}
        />
      </div>

      {/* OAuth 2.1 configuration — only for network transports (SSE / Streamable HTTP) */}
      {!isStdio && (
        <div className="rounded-md border border-dashed border-border p-3 space-y-3">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="mcp-oauth-enable"
                checked={oauthEnabled}
                onCheckedChange={(v) => setValue("oauthEnabled", v)}
              />
              <Label htmlFor="mcp-oauth-enable" className="font-medium">
                {t("form.oauth.title")}
              </Label>
            </div>
            {!oauthEnabled && (
              <p className="text-xs text-muted-foreground pl-9">{t("form.oauth.titleHint")}</p>
            )}
          </div>

          {oauthEnabled && (
            <div className="pl-0 space-y-3">
              {/* DCR toggle */}
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <Switch
                    id="mcp-oauth-dcr"
                    checked={oauthUseDcr}
                    onCheckedChange={(v) => setValue("oauthUseDcr", v)}
                  />
                  <Label htmlFor="mcp-oauth-dcr">{t("form.oauth.useDcr")}</Label>
                </div>
                <p className="text-xs text-muted-foreground pl-9">{t("form.oauth.useDcrHint")}</p>
              </div>

              {/* Manual config — when DCR disabled */}
              {!oauthUseDcr && (
                <>
                  {/* Grant type */}
                  <div className="grid gap-1.5">
                    <Label>{t("form.oauth.grantType")}</Label>
                    <Select
                      value={oauthGrantType}
                      onValueChange={(v) => setValue("oauthGrantType", v as MCPFormData["oauthGrantType"])}
                    >
                      <SelectTrigger className="text-base md:text-sm">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="pkce">{t("form.oauth.pkce")}</SelectItem>
                        <SelectItem value="authorization_code">{t("form.oauth.authCode")}</SelectItem>
                        <SelectItem value="client_credentials">{t("form.oauth.clientCreds")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {/* Authorization URL — for auth code flows (pkce and authorization_code) */}
                  {oauthGrantType !== "client_credentials" && (
                    <div className="grid gap-1.5">
                      <Label>{t("form.oauth.authEndpoint")}</Label>
                      <Input
                        value={watch("oauthAuthEndpoint")}
                        onChange={(e) => setValue("oauthAuthEndpoint", e.target.value)}
                        placeholder={t("form.oauth.authEndpointPlaceholder")}
                        className="text-base md:text-sm"
                      />
                    </div>
                  )}

                  {/* Token URL */}
                  <div className="grid gap-1.5">
                    <Label>{t("form.oauth.tokenEndpoint")}</Label>
                    <Input
                      value={watch("oauthTokenEndpoint")}
                      onChange={(e) => setValue("oauthTokenEndpoint", e.target.value)}
                      placeholder={t("form.oauth.tokenEndpointPlaceholder")}
                      className="text-base md:text-sm"
                    />
                  </div>

                  {/* Client ID */}
                  <div className="grid gap-1.5">
                    <Label>{t("form.oauth.clientId")}</Label>
                    <Input
                      autoComplete="off"
                      value={watch("oauthClientId")}
                      onChange={(e) => setValue("oauthClientId", e.target.value)}
                      placeholder="client_id"
                      className="text-base md:text-sm font-mono"
                    />
                  </div>

                  {/* Client Secret — not needed for PKCE (public client) */}
                  {oauthGrantType !== "pkce" && (
                    <div className="grid gap-1.5">
                      <Label>{t("form.oauth.clientSecret")}</Label>
                      <Input
                        type="password"
                        autoComplete="new-password"
                        value={watch("oauthClientSecret")}
                        onChange={(e) => setValue("oauthClientSecret", e.target.value)}
                        placeholder={isEditing ? t("form.oauth.clientSecretPlaceholder") : "client_secret"}
                        className="text-base md:text-sm"
                      />
                    </div>
                  )}

                  {/* Scopes */}
                  <div className="grid gap-1.5">
                    <Label>{t("form.oauth.scope")}</Label>
                    <Input
                      value={watch("oauthScope")}
                      onChange={(e) => setValue("oauthScope", e.target.value)}
                      placeholder={t("form.oauth.scopePlaceholder")}
                      className="text-base md:text-sm"
                    />
                  </div>
                </>
              )}

              {/* Auth mode indicator */}
              <p className="text-xs text-muted-foreground">
                {requireUserCreds ? t("form.oauth.modePerUser") : t("form.oauth.modeGlobal")}
              </p>
            </div>
          )}
        </div>
      )}

      {/* Admin-authored hints appended to MCP tool descriptions. */}
      <div className="grid gap-3 rounded-md border border-dashed border-border p-3">
        <div className="grid gap-1">
          <Label className="text-sm font-medium">{t("form.toolHints")}</Label>
          <p className="text-xs text-muted-foreground">{t("form.toolHintsHint")}</p>
        </div>

        <div className="grid gap-1.5">
          <Label htmlFor="mcp-tool-hints-global" className="text-xs text-muted-foreground">
            {t("form.toolHintsGlobal")}
          </Label>
          <Textarea
            id="mcp-tool-hints-global"
            value={toolHintsGlobal}
            onChange={(e) => setValue("toolHintsGlobal", e.target.value)}
            placeholder={t("form.toolHintsGlobalPlaceholder")}
            rows={3}
            className="text-base md:text-sm resize-y"
          />
          <p className="text-xs text-muted-foreground">{t("form.toolHintsGlobalHint")}</p>
        </div>

        <div className="grid gap-1.5">
          <Label className="text-xs text-muted-foreground">{t("form.toolHintsPerTool")}</Label>
          <KeyValueEditor
            value={toolHintsTools}
            onChange={(v) => setValue("toolHintsTools", v)}
            keyPlaceholder={t("form.toolHintsToolNamePlaceholder")}
            valuePlaceholder={t("form.toolHintsToolValuePlaceholder")}
            addLabel={t("form.toolHintsAdd")}
            valueAs="textarea"
          />
        </div>
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
          onChange={(e) => setValue("timeout", parseInt(e.target.value, 10) || 60, { shouldValidate: true })}
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
    </>
  );
}
