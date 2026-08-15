import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { Shield, Clock, Building2, KeyRound } from "lucide-react";
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
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useTenants } from "@/hooks/use-tenants";
import type { ApiKeyCreateInput } from "@/types/api-key";
import { apiKeyCreateSchema, type ApiKeyCreateFormData } from "@/schemas/api-key.schema";

const ALL_SCOPES = [
  "operator.admin",
  "operator.read",
  "operator.write",
  "operator.approvals",
  "operator.pairing",
  "operator.provision",
] as const;

const EXPIRY_OPTIONS = [
  { value: "never", seconds: 0 },
  { value: "7d", seconds: 7 * 86400 },
  { value: "30d", seconds: 30 * 86400 },
  { value: "90d", seconds: 90 * 86400 },
] as const;

// Sentinel for "system (all tenants)" — no tenant_id sent
const SYSTEM_TENANT = "__system__";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: ApiKeyCreateInput) => Promise<void>;
}

export function ApiKeyCreateDialog({ open, onOpenChange, onCreate }: Props) {
  const { t } = useTranslation("api-keys");
  const { isOwner, currentTenantId, currentTenantName } = useTenants();

  const defaultTenant = currentTenantId || SYSTEM_TENANT;

  const [saving, setSaving] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<ApiKeyCreateFormData>({
    resolver: zodResolver(apiKeyCreateSchema),
    defaultValues: {
      name: "",
      scopes: [],
      expiry: "never",
      tenantValue: defaultTenant,
    },
  });

  const scopes = watch("scopes");
  const expiry = watch("expiry");
  const tenantValue = watch("tenantValue");

  const toggleScope = (scope: string) => {
    const next = scopes.includes(scope)
      ? scopes.filter((s) => s !== scope)
      : [...scopes, scope];
    setValue("scopes", next, { shouldValidate: true });
  };

  const onValid = async (data: ApiKeyCreateFormData) => {
    setSaving(true);
    try {
      const expiryOption = EXPIRY_OPTIONS.find((o) => o.value === data.expiry);
      const input: ApiKeyCreateInput = {
        name: data.name.trim(),
        scopes: data.scopes,
        expires_in: expiryOption && expiryOption.seconds > 0 ? expiryOption.seconds : undefined,
      };
      if (isOwner && data.tenantValue !== SYSTEM_TENANT) {
        input.tenant_id = data.tenantValue;
      }
      await onCreate(input);
      reset({ name: "", scopes: [], expiry: "never", tenantValue: defaultTenant });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRound className="h-5 w-5" />
            {t("form.title")}
          </DialogTitle>
          <DialogDescription>{t("description")}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onValid)} className="space-y-5">
          {/* Name + Tenant row */}
          <div className={`grid gap-4 ${isOwner ? "grid-cols-1 sm:grid-cols-2" : "grid-cols-1"}`}>
            <div className="space-y-1.5">
              <Label htmlFor="key-name">{t("form.name")}</Label>
              <Input
                id="key-name"
                {...register("name")}
                placeholder={t("form.namePlaceholder")}
                className="text-base md:text-sm"
              />
              {errors.name && (
                <p className="text-xs text-destructive">{errors.name.message}</p>
              )}
            </div>

            {isOwner && (
              <div className="space-y-1.5">
                <Label htmlFor="key-tenant" className="flex items-center gap-1.5">
                  <Building2 className="h-3.5 w-3.5" />
                  Tenant
                </Label>
                <Select
                  value={tenantValue}
                  onValueChange={(v) => setValue("tenantValue", v, { shouldValidate: true })}
                >
                  <SelectTrigger id="key-tenant" className="text-base md:text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={SYSTEM_TENANT}>
                      {t("form.tenantSystem")}
                    </SelectItem>
                    {currentTenantId && (
                      <SelectItem value={currentTenantId}>
                        {currentTenantName || currentTenantId}
                      </SelectItem>
                    )}
                  </SelectContent>
                </Select>
              </div>
            )}
          </div>

          {/* Scopes */}
          <div className="space-y-2">
            <Label className="flex items-center gap-1.5">
              <Shield className="h-3.5 w-3.5" />
              {t("form.scopes")}
            </Label>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-1.5">
              {ALL_SCOPES.map((scope) => {
                const shortName = scope.replace("operator.", "");
                const isSelected = scopes.includes(scope);
                return (
                  <button
                    key={scope}
                    type="button"
                    onClick={() => toggleScope(scope)}
                    className={`flex items-center gap-2.5 rounded-md border px-3 py-2 text-left text-sm transition-colors ${
                      isSelected
                        ? "border-primary bg-primary/5 text-foreground"
                        : "border-border hover:bg-accent text-muted-foreground"
                    }`}
                  >
                    <div className={`h-4 w-4 rounded border flex items-center justify-center shrink-0 ${
                      isSelected ? "border-primary bg-primary text-primary-foreground" : "border-muted-foreground/30"
                    }`}>
                      {isSelected && (
                        <svg className="h-3 w-3" viewBox="0 0 12 12" fill="none">
                          <path d="M2 6l3 3 5-5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                        </svg>
                      )}
                    </div>
                    <div className="min-w-0">
                      <Badge variant={isSelected ? "default" : "secondary"} className="text-xs-plus font-mono px-1.5 py-0">
                        {shortName}
                      </Badge>
                      <p className="text-xs text-muted-foreground mt-0.5 leading-tight">
                        {t(`form.scopeDescriptions.${scope}`)}
                      </p>
                    </div>
                  </button>
                );
              })}
            </div>
            {errors.scopes && (
              <p className="text-xs text-destructive">{errors.scopes.message}</p>
            )}
          </div>

          {/* Expiry */}
          <div className="space-y-1.5">
            <Label htmlFor="key-expiry" className="flex items-center gap-1.5">
              <Clock className="h-3.5 w-3.5" />
              {t("form.expiry")}
            </Label>
            <Select
              value={expiry}
              onValueChange={(v) => setValue("expiry", v, { shouldValidate: true })}
            >
              <SelectTrigger id="key-expiry" className="text-base md:text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {EXPIRY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {t(`form.expiryOptions.${opt.value}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={saving || scopes.length === 0}>
              {saving ? t("form.creating") : t("form.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
