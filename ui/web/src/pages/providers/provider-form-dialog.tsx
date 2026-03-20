import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useQueryClient } from "@tanstack/react-query";
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
import type { ProviderData, ProviderInput } from "./hooks/use-providers";
import { slugify, isValidSlug } from "@/lib/slug";
import { PROVIDER_TYPES } from "@/constants/providers";
import { OAuthSection } from "./provider-oauth-section";
import { CLISection } from "./provider-cli-section";
import { ACPSection } from "./provider-acp-section";
import { Loader2 } from "lucide-react";

interface ProviderFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: ProviderInput) => Promise<unknown>;
  existingProviders?: ProviderData[];
}

export function ProviderFormDialog({ open, onOpenChange, onSubmit, existingProviders = [] }: ProviderFormDialogProps) {
  const { t } = useTranslation("providers");
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [providerType, setProviderType] = useState("openai_compat");
  const [apiBase, setApiBase] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [enabled, setEnabled] = useState(true);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // ACP fields
  const [acpBinary, setAcpBinary] = useState("");
  const [acpArgs, setAcpArgs] = useState("");
  const [acpIdleTTL, setAcpIdleTTL] = useState("");
  const [acpPermMode, setAcpPermMode] = useState("approve-all");
  const [acpWorkDir, setAcpWorkDir] = useState("");

  const hasClaudeCLI = existingProviders.some((p) => p.provider_type === "claude_cli");

  const isOAuth = providerType === "chatgpt_oauth";
  const isCLI = providerType === "claude_cli";
  const isACP = providerType === "acp";

  useEffect(() => {
    if (open) {
      setError("");
      setName("");
      setDisplayName("");
      setProviderType("openai_compat");
      setApiBase("");
      setApiKey("");
      setEnabled(true);
      setAcpBinary("");
      setAcpArgs("");
      setAcpIdleTTL("");
      setAcpPermMode("approve-all");
      setAcpWorkDir("");
    }
  }, [open]);

  const handleSubmit = async () => {
    if (!name.trim() || !providerType) return;
    setLoading(true);
    try {
      const data: ProviderInput = {
        name: name.trim(),
        display_name: displayName.trim() || undefined,
        provider_type: providerType,
        api_base: apiBase.trim() || undefined,
        enabled,
      };

      if (isACP) {
        data.api_base = acpBinary.trim() || undefined;
        const settings: Record<string, unknown> = {};
        if (acpArgs.trim()) {
          settings.args = acpArgs.trim().split(/\s+/);
        }
        if (acpIdleTTL.trim()) settings.idle_ttl = acpIdleTTL.trim();
        if (acpPermMode) settings.perm_mode = acpPermMode;
        if (acpWorkDir.trim()) settings.work_dir = acpWorkDir.trim();
        if (Object.keys(settings).length > 0) {
          data.settings = settings;
        }
      }

      if (apiKey && apiKey !== "***") {
        data.api_key = apiKey;
      }

      await onSubmit(data);
      onOpenChange(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("form.saving"));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col">
        <DialogHeader>
          <DialogTitle>{t("form.createTitle")}</DialogTitle>
          <DialogDescription>{t("form.configure")}</DialogDescription>
        </DialogHeader>
        <div className="-mx-4 min-h-0 overflow-y-auto px-4 py-4 sm:-mx-6 sm:px-6 space-y-4">
          <ProviderTypeSelect
            value={providerType}
            hasClaudeCLI={hasClaudeCLI}
            alreadyAddedLabel={t("form.alreadyAdded")}
            providerTypeLabel={t("form.providerType")}
            onChange={(v) => {
              setProviderType(v);
              const preset = PROVIDER_TYPES.find((pt) => pt.value === v);
              setApiBase(preset?.apiBase || "");
              if (v === "chatgpt_oauth") {
                setName("openai-codex");
                setDisplayName("ChatGPT (OAuth)");
              } else {
                if (name === "openai-codex") setName("");
                if (displayName === "ChatGPT (OAuth)") setDisplayName("");
              }
            }}
          />

          {isOAuth ? (
            <>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label>{t("form.nameFixed")}</Label>
                  <Input value="openai-codex" disabled className="text-base md:text-sm" />
                </div>
                <div className="space-y-2">
                  <Label>{t("form.displayName")}</Label>
                  <Input value="ChatGPT (OAuth)" disabled className="text-base md:text-sm" />
                </div>
              </div>
              <OAuthSection onSuccess={() => { queryClient.invalidateQueries({ queryKey: ["providers"] }); onOpenChange(false); }} />
            </>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="name">{t("form.name")}</Label>
                  <Input
                    id="name"
                    value={name}
                    onChange={(e) => setName(slugify(e.target.value))}
                    placeholder={t("form.namePlaceholder")}
                    className="text-base md:text-sm"
                  />
                  <p className="text-xs text-muted-foreground">{t("form.nameHint")}</p>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="displayName">{t("form.displayName")}</Label>
                  <Input
                    id="displayName"
                    value={displayName}
                    onChange={(e) => setDisplayName(e.target.value)}
                    placeholder={t("form.displayNamePlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </div>

              {isCLI && <CLISection open={open} />}

              {isACP && (
                <ACPSection
                  binary={acpBinary}
                  onBinaryChange={setAcpBinary}
                  args={acpArgs}
                  onArgsChange={setAcpArgs}
                  idleTTL={acpIdleTTL}
                  onIdleTTLChange={setAcpIdleTTL}
                  permMode={acpPermMode}
                  onPermModeChange={setAcpPermMode}
                  workDir={acpWorkDir}
                  onWorkDirChange={setAcpWorkDir}
                />
              )}

              {!isCLI && !isACP && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="apiBase">{t("form.apiBase")}</Label>
                    <Input
                      id="apiBase"
                      value={apiBase}
                      onChange={(e) => setApiBase(e.target.value)}
                      placeholder={PROVIDER_TYPES.find((pt) => pt.value === providerType)?.placeholder || PROVIDER_TYPES.find((pt) => pt.value === providerType)?.apiBase || "https://api.example.com/v1"}
                      className="text-base md:text-sm"
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="apiKey">{t("form.apiKey")}</Label>
                    <Input
                      id="apiKey"
                      type="password"
                      value={apiKey}
                      onChange={(e) => setApiKey(e.target.value)}
                      placeholder={t("form.apiKeyPlaceholder")}
                      className="text-base md:text-sm"
                    />
                  </div>
                </>
              )}

              <div className="flex items-center justify-between">
                <Label htmlFor="enabled">{t("form.enabled")}</Label>
                <Switch id="enabled" checked={enabled} onCheckedChange={setEnabled} />
              </div>
              {error && (
                <p className="text-sm text-destructive">{error}</p>
              )}
            </>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {isOAuth ? t("form.close") : t("form.cancel")}
          </Button>
          {!isOAuth && (
            <Button
              onClick={handleSubmit}
              disabled={!name.trim() || !isValidSlug(name) || !providerType || loading}
              className="gap-1"
            >
              {loading && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {loading ? t("form.creating") : t("form.create")}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ProviderTypeSelect({ value, hasClaudeCLI, alreadyAddedLabel, providerTypeLabel, onChange }: {
  value: string;
  hasClaudeCLI: boolean;
  alreadyAddedLabel: string;
  providerTypeLabel: string;
  onChange: (value: string) => void;
}) {
  return (
    <div className="space-y-2">
      <Label>{providerTypeLabel}</Label>
      <Select value={value} onValueChange={onChange}>
        <SelectTrigger>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {PROVIDER_TYPES.map((pt) => (
            <SelectItem
              key={pt.value}
              value={pt.value}
              disabled={pt.value === "claude_cli" && hasClaudeCLI}
            >
              {pt.label}
              {pt.value === "claude_cli" && hasClaudeCLI && (
                <span className="ml-1 text-xs opacity-60">{alreadyAddedLabel}</span>
              )}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
