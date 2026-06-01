import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { DialogFooter } from "@/components/ui/dialog";
import { BitrixPortalHelpSection } from "./bitrix-portal-help-section";
import { useBitrixPortalCreate } from "./use-bitrix-portals";

// Validation mirrors the server-side regex in
// internal/gateway/methods/bitrix_portals.go. Server is authoritative;
// client validation is purely UX so the operator gets feedback before a
// round-trip. Pattern intentionally accepts a wide TLD set (Bitrix24 has
// regional clouds: .com, .eu, .ru, .de, .fr, .jp, .in, .kz, .ua, .by) plus
// .bitrix.info for self-hosted.
const BITRIX_DOMAIN_RE =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.(bitrix24\.(com|eu|ru|de|fr|jp|in|kz|ua|by)|bitrix\.info)$/;
const PORTAL_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,62}[a-z0-9]$/;

interface BitrixPortalFormStepProps {
  /** Invoked with the server response after bitrix.portals.create succeeds. */
  onSuccess: (createdName: string, installUrl: string, warning?: string) => void;
  onCancel: () => void;
}

// Step 1 of the BitrixPortalCreateModal: collect name/domain/client_id/secret
// and POST to bitrix.portals.create. On success, the parent flips to step 2
// (authorize). Auto-fills the portal name from the subdomain prefix when the
// admin tabs out of the Domain field — cheap UX win, easy to override.
export function BitrixPortalFormStep({ onSuccess, onCancel }: BitrixPortalFormStepProps) {
  const { t } = useTranslation("channels");
  const create = useBitrixPortalCreate();
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [clientId, setClientId] = useState("");
  const [clientSecret, setClientSecret] = useState("");
  const [nameTouched, setNameTouched] = useState(false);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [serverError, setServerError] = useState("");

  // Auto-derive name from domain subdomain when user hasn't manually edited
  // the name field. "tamgiac.bitrix24.com" → "tamgiac".
  const handleDomainBlur = () => {
    if (nameTouched || !domain) return;
    const m = domain.toLowerCase().match(/^([a-z0-9-]+)\./);
    if (m && m[1]) setName(m[1]);
  };

  const validate = (): boolean => {
    const e: Record<string, string> = {};
    if (!PORTAL_NAME_RE.test(name)) {
      e.name = t("bitrix24.create.errors.invalidName", {
        defaultValue: "Use lowercase letters, digits, hyphens, underscores (2-64 chars).",
      });
    }
    if (!BITRIX_DOMAIN_RE.test(domain.toLowerCase())) {
      e.domain = t("bitrix24.create.errors.invalidDomain", {
        defaultValue: "Must be a valid Bitrix24 portal domain (e.g. mycorp.bitrix24.com).",
      });
    }
    if (!clientId.trim()) e.client_id = t("common.required", { defaultValue: "Required" });
    if (!clientSecret.trim()) e.client_secret = t("common.required", { defaultValue: "Required" });
    setErrors(e);
    return Object.keys(e).length === 0;
  };

  const handleSubmit = async () => {
    setServerError("");
    if (!validate()) return;
    try {
      const res = await create.mutateAsync({
        name,
        domain: domain.toLowerCase(),
        client_id: clientId,
        client_secret: clientSecret,
      });
      onSuccess(res.name, res.install_url, res.warning);
    } catch (err: unknown) {
      // WS errors come back with .code on the ApiError-shaped object.
      const apiErr = err as { code?: string; message?: string };
      if (apiErr?.code === "ALREADY_EXISTS") {
        setErrors({
          name: t("bitrix24.create.errors.duplicateName", {
            defaultValue: "A portal with this name already exists.",
          }),
        });
        return;
      }
      if (apiErr?.code === "UNAUTHORIZED") {
        setServerError(
          t("bitrix24.create.errors.forbidden", {
            defaultValue: "You need tenant admin permission to create portals.",
          }),
        );
        return;
      }
      if (apiErr?.code === "FAILED_PRECONDITION") {
        // Gateway hasn't observed its public URL yet — typical first-boot
        // scenario. Tell the admin what to do instead of accepting a
        // half-success row we can't authorize.
        setServerError(
          t("bitrix24.create.errors.gatewayURLUnknown", {
            defaultValue:
              "Open the goclaw UI via your public URL first (not localhost), then retry.",
          }),
        );
        return;
      }
      setServerError(apiErr?.message ?? t("common.unknownError", { defaultValue: "Unknown error" }));
    }
  };

  return (
    <div className="grid gap-3">
      <div className="grid gap-1.5">
        <Label htmlFor="bp-domain">
          {t("bitrix24.create.fields.domain", { defaultValue: "Domain" })} *
        </Label>
        <Input
          id="bp-domain"
          value={domain}
          onChange={(e) => setDomain(e.target.value)}
          onBlur={handleDomainBlur}
          placeholder="tamgiac.bitrix24.com"
          autoComplete="off"
          autoFocus
        />
        {errors.domain && <p className="text-xs text-destructive">{errors.domain}</p>}
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="bp-name">
          {t("bitrix24.create.fields.name", { defaultValue: "Portal name" })} *
        </Label>
        <Input
          id="bp-name"
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setNameTouched(true);
          }}
          placeholder="tamgiac"
          autoComplete="off"
        />
        {errors.name && <p className="text-xs text-destructive">{errors.name}</p>}
        <p className="text-xs text-muted-foreground">
          {t("bitrix24.create.fields.nameHint", {
            defaultValue: "Internal slug. Auto-filled from domain; you can edit it.",
          })}
        </p>
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="bp-cid">
          {t("bitrix24.create.fields.clientId", { defaultValue: "Client ID" })} *
        </Label>
        <Input
          id="bp-cid"
          value={clientId}
          onChange={(e) => setClientId(e.target.value)}
          placeholder="local.61f8a3d2bc1234.78901234"
          autoComplete="off"
        />
        {errors.client_id && <p className="text-xs text-destructive">{errors.client_id}</p>}
      </div>

      <div className="grid gap-1.5">
        <Label htmlFor="bp-secret">
          {t("bitrix24.create.fields.clientSecret", { defaultValue: "Client Secret" })} *
        </Label>
        <Input
          id="bp-secret"
          type="password"
          value={clientSecret}
          onChange={(e) => setClientSecret(e.target.value)}
          autoComplete="off"
        />
        {errors.client_secret && <p className="text-xs text-destructive">{errors.client_secret}</p>}
      </div>

      <BitrixPortalHelpSection />

      {serverError && <p className="text-sm text-destructive">{serverError}</p>}

      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel} disabled={create.isPending}>
          {t("common.cancel", { defaultValue: "Cancel" })}
        </Button>
        <Button type="button" onClick={handleSubmit} disabled={create.isPending}>
          {create.isPending
            ? t("common.loading", { defaultValue: "Loading..." })
            : t("bitrix24.create.submit", { defaultValue: "Create & Authorize →" })}
        </Button>
      </DialogFooter>
    </div>
  );
}
