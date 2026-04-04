import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Save, Settings, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { ConfigGroupHeader } from "@/components/shared/config-group-header";
import { PROVIDER_TYPES } from "@/constants/providers";
import { CLISection } from "../provider-cli-section";
import { OAuthSection } from "../provider-oauth-section";
import type { ProviderData, ProviderInput } from "@/types/provider";

interface ProviderAdvancedDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  provider: ProviderData;
  onUpdate: (id: string, data: ProviderInput) => Promise<void>;
}

function deriveState(provider: ProviderData) {
  const s = provider.settings as Record<string, unknown> | undefined;
  return {
    apiBase: provider.api_base || "",
    acpBinary: provider.provider_type === "acp" ? (provider.api_base || "") : "",
    acpArgs: Array.isArray(s?.args) ? (s.args as string[]).join(" ") : "",
    acpIdleTTL: (s?.idle_ttl as string) || "",
    acpPermMode: (s?.perm_mode as string) || "approve-all",
    acpWorkDir: (s?.work_dir as string) || "",
  };
}

export function ProviderAdvancedDialog({
  open,
  onOpenChange,
  provider,
  onUpdate,
}: ProviderAdvancedDialogProps) {
  const { t } = useTranslation("providers");

  const isACP = provider.provider_type === "acp";
  const isCLI = provider.provider_type === "claude_cli";
  const isOAuth = provider.provider_type === "chatgpt_oauth";
  const isStandard = !isACP && !isCLI && !isOAuth;

  const typeInfo = PROVIDER_TYPES.find((pt) => pt.value === provider.provider_type);

  const init = deriveState(provider);
  const [apiBase, setApiBase] = useState(init.apiBase);
  const [acpBinary, setAcpBinary] = useState(init.acpBinary);
  const [acpArgs, setAcpArgs] = useState(init.acpArgs);
  const [acpIdleTTL, setAcpIdleTTL] = useState(init.acpIdleTTL);
  const [acpPermMode, setAcpPermMode] = useState(init.acpPermMode);
  const [acpWorkDir, setAcpWorkDir] = useState(init.acpWorkDir);

  // Re-sync when dialog opens
  useEffect(() => {
    if (!open) return;
    const s = deriveState(provider);
    setApiBase(s.apiBase);
    setAcpBinary(s.acpBinary);
    setAcpArgs(s.acpArgs);
    setAcpIdleTTL(s.acpIdleTTL);
    setAcpPermMode(s.acpPermMode);
    setAcpWorkDir(s.acpWorkDir);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    setSaving(true);
    try {
      const data: ProviderInput = {
        name: provider.name,
        provider_type: provider.provider_type,
      };

      if (isACP) {
        data.api_base = acpBinary.trim() || undefined;
        const settings: Record<string, unknown> = {};
        if (acpArgs.trim()) settings.args = acpArgs.trim().split(/\s+/);
        if (acpIdleTTL.trim()) settings.idle_ttl = acpIdleTTL.trim();
        if (acpPermMode) settings.perm_mode = acpPermMode;
        if (acpWorkDir.trim()) settings.work_dir = acpWorkDir.trim();
        if (Object.keys(settings).length > 0) data.settings = settings;
      } else if (isStandard) {
        data.api_base = apiBase.trim() || undefined;
      }

      await onUpdate(provider.id, data);
      onOpenChange(false);
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] w-[95vw] flex flex-col sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Settings className="h-4 w-4" />
            {t("detail.advanced")}
          </DialogTitle>
        </DialogHeader>

        {/* Scrollable body */}
        <div className="overflow-y-auto min-h-0 -mx-4 px-4 sm:-mx-6 sm:px-6 space-y-4">
          {/* Identity */}
          <ConfigGroupHeader
            title={t("detail.identity")}
            description={t("detail.identityDesc")}
          />
          <div className="space-y-2">
            <Label>{isOAuth ? t("form.oauthAlias") : t("form.name")}</Label>
            <Input
              value={provider.name}
              disabled
              className="text-base md:text-sm font-mono"
            />
            <p className="text-xs text-muted-foreground">
              {isOAuth ? t("detail.oauthAliasReadonly") : t("detail.nameReadonly")}
            </p>
          </div>

          {/* Connection — standard providers only */}
          {isStandard && (
            <>
              <ConfigGroupHeader
                title={t("detail.connection")}
                description={t("detail.connectionDesc")}
              />
              <div className="space-y-2">
                <Label htmlFor="apiBase">{t("form.apiBase")}</Label>
                <Input
                  id="apiBase"
                  value={apiBase}
                  onChange={(e) => setApiBase(e.target.value)}
                  placeholder={typeInfo?.placeholder || typeInfo?.apiBase || "https://api.example.com/v1"}
                  className="text-base md:text-sm"
                />
              </div>
            </>
          )}

          {/* ACP Configuration */}
          {isACP && (
            <>
              <ConfigGroupHeader
                title={t("detail.acpConfig")}
                description={t("detail.acpConfigDesc")}
              />
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="acpBinary">{t("acp.binary")}</Label>
                  <Input
                    id="acpBinary"
                    value={acpBinary}
                    onChange={(e) => setAcpBinary(e.target.value)}
                    placeholder={t("acp.binaryPlaceholder")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground">{t("acp.binaryHint")}</p>
                </div>

                <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="acpArgs">{t("acp.args")}</Label>
                  <Input
                    id="acpArgs"
                    value={acpArgs}
                    onChange={(e) => setAcpArgs(e.target.value)}
                    placeholder={t("acp.argsPlaceholder")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground">{t("acp.argsHint")}</p>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="acpIdleTTL">{t("acp.idleTTL")}</Label>
                  <Input
                    id="acpIdleTTL"
                    value={acpIdleTTL}
                    onChange={(e) => setAcpIdleTTL(e.target.value)}
                    placeholder={t("acp.idleTTLPlaceholder")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground">{t("acp.idleTTLHint")}</p>
                </div>

                <div className="space-y-2">
                  <Label>{t("acp.permMode")}</Label>
                  <Select value={acpPermMode} onValueChange={setAcpPermMode}>
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="approve-all">{t("acp.permModeApproveAll")}</SelectItem>
                      <SelectItem value="approve-reads">{t("acp.permModeApproveReads")}</SelectItem>
                      <SelectItem value="deny-all">{t("acp.permModeDenyAll")}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2 sm:col-span-2">
                  <Label htmlFor="acpWorkDir">{t("acp.workDir")}</Label>
                  <Input
                    id="acpWorkDir"
                    value={acpWorkDir}
                    onChange={(e) => setAcpWorkDir(e.target.value)}
                    placeholder={t("acp.workDirPlaceholder")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground">{t("acp.workDirHint")}</p>
                </div>
              </div>
            </>
          )}

          {/* Claude CLI */}
          {isCLI && (
            <>
              <ConfigGroupHeader
                title={t("detail.cliConfig")}
                description={t("detail.cliConfigDesc")}
              />
              <CLISection open={open} />
            </>
          )}

          {/* OAuth */}
          {isOAuth && (
            <>
              <ConfigGroupHeader
                title={t("detail.oauthConfig")}
                description={t("detail.oauthConfigDesc")}
              />
              <OAuthSection
                providerName={provider.name}
                displayName={provider.display_name}
                apiBase={provider.api_base}
                authenticatedActionLabel={t("form.close")}
                onSuccess={() => onOpenChange(false)}
              />
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 pt-4 border-t shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("form.cancel")}
          </Button>
          {!isOAuth && (
            <Button onClick={handleSave} disabled={saving}>
              {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
              {saving ? t("form.saving") : t("form.save")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
