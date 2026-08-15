import { useEffect } from "react";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import { slugify } from "@/lib/slug";
import { DEFAULT_CODEX_OAUTH_ALIAS, PROVIDER_TYPES, suggestUniqueProviderAlias } from "@/constants/providers";
import { OAuthSection } from "./provider-oauth-section";
import { CLISection } from "./provider-cli-section";
import { ACPSection } from "./provider-acp-section";
import { ProviderStandardFormFields } from "./provider-standard-form-fields";
import { Loader2 } from "lucide-react";
import { providerCreateSchema, type ProviderCreateFormData } from "@/schemas/provider.schema";

interface ProviderFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: ProviderInput) => Promise<unknown>;
  existingProviders?: ProviderData[];
}

export function ProviderFormDialog({ open, onOpenChange, onSubmit, existingProviders = [] }: ProviderFormDialogProps) {
  const { t } = useTranslation("providers");
  const queryClient = useQueryClient();

  const form = useForm<ProviderCreateFormData>({
    resolver: zodResolver(providerCreateSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      displayName: "",
      providerType: "openai_compat",
      apiBase: "",
      apiKey: "",
      enabled: true,
      acpBinary: "",
      acpArgs: "",
      acpIdleTTL: "",
      acpPermMode: "approve-all",
      acpWorkDir: "",
    },
  });

  const { register, control, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = form;

  const providerType = watch("providerType");
  const name = watch("name");

  const hasClaudeCLI = existingProviders.some((p) => p.provider_type === "claude_cli");
  const isOAuth = providerType === "chatgpt_oauth";
  const isCLI = providerType === "claude_cli";
  const isACP = providerType === "acp";

  // Reset form when dialog opens
  useEffect(() => {
    if (open) {
      reset({
        name: "",
        displayName: "",
        providerType: "openai_compat",
        apiBase: "",
        apiKey: "",
        enabled: true,
        acpBinary: "",
        acpArgs: "",
        acpIdleTTL: "",
        acpPermMode: "approve-all",
        acpWorkDir: "",
      });
    }
  }, [open, reset]);

  const onFormSubmit = async (data: ProviderCreateFormData) => {
    const payload: ProviderInput = {
      name: data.name,
      display_name: data.displayName || undefined,
      provider_type: data.providerType,
      api_base: data.apiBase || undefined,
      enabled: data.enabled,
    };

    if (isACP) {
      payload.api_base = data.acpBinary || undefined;
      const settings: Record<string, unknown> = {};
      if (data.acpArgs?.trim()) settings.args = data.acpArgs.trim().split(/\s+/);
      if (data.acpIdleTTL?.trim()) settings.idle_ttl = data.acpIdleTTL.trim();
      if (data.acpPermMode) settings.perm_mode = data.acpPermMode;
      if (data.acpWorkDir?.trim()) settings.work_dir = data.acpWorkDir.trim();
      if (Object.keys(settings).length > 0) payload.settings = settings;
    }

    if (data.apiKey && data.apiKey !== "***") {
      payload.api_key = data.apiKey;
    }

    await onSubmit(payload);
    onOpenChange(false);
  };

  const handleProviderTypeChange = (v: string) => {
    setValue("providerType", v, { shouldValidate: true });
    const preset = PROVIDER_TYPES.find((pt) => pt.value === v);
    setValue("apiBase", preset?.apiBase || "");
    if (v === "chatgpt_oauth") {
      if (!name || providerType !== "chatgpt_oauth") {
        setValue("name", suggestUniqueProviderAlias(existingProviders));
      }
    } else {
      if (name === DEFAULT_CODEX_OAUTH_ALIAS) setValue("name", "");
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col">
        <DialogHeader>
          <DialogTitle>{t("form.createTitle")}</DialogTitle>
          <DialogDescription>{isOAuth ? t("form.configureOauth") : t("form.configure")}</DialogDescription>
        </DialogHeader>
        <div className="-mx-4 min-h-0 overflow-y-auto px-4 py-4 sm:-mx-6 sm:px-6 space-y-4">
          <ProviderTypeSelect
            value={providerType}
            hasClaudeCLI={hasClaudeCLI}
            alreadyAddedLabel={t("form.alreadyAdded")}
            providerTypeLabel={t("form.providerType")}
            onChange={handleProviderTypeChange}
          />

          {isOAuth ? (
            <>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="oauth-name">{t("form.oauthAlias")}</Label>
                  <Input
                    id="oauth-name"
                    {...register("name")}
                    onChange={(e) => setValue("name", slugify(e.target.value), { shouldValidate: true })}
                    placeholder={t("form.oauthAliasPlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="oauth-display-name">{t("form.displayName")}</Label>
                  <Input
                    id="oauth-display-name"
                    {...register("displayName")}
                    placeholder={t("form.oauthDisplayNamePlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </div>
              <OAuthSection
                providerName={name}
                displayName={watch("displayName") || ""}
                apiBase={watch("apiBase") || ""}
                authenticatedActionLabel={t("form.close")}
                onSuccess={() => { queryClient.invalidateQueries({ queryKey: ["providers"] }); onOpenChange(false); }}
              />
            </>
          ) : (
            <>
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="name">{t("form.name")}</Label>
                  <Input
                    id="name"
                    {...register("name")}
                    onChange={(e) => setValue("name", slugify(e.target.value), { shouldValidate: true })}
                    placeholder={t("form.namePlaceholder")}
                    className="text-base md:text-sm"
                  />
                  {errors.name ? (
                    <p className="text-xs text-destructive">{errors.name.message}</p>
                  ) : (
                    <p className="text-xs text-muted-foreground">{t("form.nameHint")}</p>
                  )}
                </div>
                <div className="space-y-2">
                  <Label htmlFor="displayName">{t("form.displayName")}</Label>
                  <Input
                    id="displayName"
                    {...register("displayName")}
                    placeholder={t("form.displayNamePlaceholder")}
                    className="text-base md:text-sm"
                  />
                </div>
              </div>

              {isCLI && <CLISection open={open} />}

              {isACP && (
                <ACPSection
                  binary={watch("acpBinary") || ""}
                  onBinaryChange={(v) => setValue("acpBinary", v)}
                  args={watch("acpArgs") || ""}
                  onArgsChange={(v) => setValue("acpArgs", v)}
                  idleTTL={watch("acpIdleTTL") || ""}
                  onIdleTTLChange={(v) => setValue("acpIdleTTL", v)}
                  permMode={watch("acpPermMode") || "approve-all"}
                  onPermModeChange={(v) => setValue("acpPermMode", v)}
                  workDir={watch("acpWorkDir") || ""}
                  onWorkDirChange={(v) => setValue("acpWorkDir", v)}
                />
              )}

              {!isCLI && !isACP && (
                <ProviderStandardFormFields
                  register={register}
                  errors={errors}
                  providerType={providerType}
                  control={control}
                />
              )}

              {(isCLI || isACP) && (
                <>
                  <div className="flex items-center justify-between">
                    <Label htmlFor="enabled">{t("form.enabled")}</Label>
                    <Controller
                      control={control}
                      name="enabled"
                      render={({ field }) => (
                        <Switch id="enabled" checked={field.value} onCheckedChange={field.onChange} />
                      )}
                    />
                  </div>
                  {errors.root && (
                    <p className="text-sm text-destructive">{errors.root.message}</p>
                  )}
                </>
              )}
            </>

          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {isOAuth ? t("form.close") : t("form.cancel")}
          </Button>
          {!isOAuth && (
            <Button
              onClick={handleSubmit(onFormSubmit, (errs) => {
                // surface first field error as root error for display
                const first = Object.values(errs)[0];
                if (first?.message) form.setError("root", { message: first.message });
              })}
              disabled={!name || !!errors.name || !providerType || isSubmitting}
              className="gap-1"
            >
              {isSubmitting && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {isSubmitting ? t("form.creating") : t("form.create")}
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
