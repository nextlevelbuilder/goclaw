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
// round-trip. Pattern accepts Bitrix24 regional clouds (.com, .eu, .ru,
// .de, .fr, .jp, .in, .kz, .ua, .by, .vn, .tr, .es, .com.br, .com.ar),
// .bitrix.info self-hosted, plus any valid hostname/port for fully
// self-hosted custom domains.
const BITRIX_DOMAIN_RE =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.(bitrix24\.(com|eu|ru|de|fr|jp|in|kz|ua|by|vn|tr|es|com\.br|com\.ar)|bitrix\.info)$/;
const SELF_HOSTED_DOMAIN_RE =
  /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*(:\d+)?$/;
const PORTAL_NAME_RE = /^[a-z0-9][a-z0-9_-]{0,62}[a-z0-9]$/;

// validateSelfHostedDomain mirrors the backend SSRF + port validation.
// Rejects localhost, .local, .localhost TLDs, literal private/loopback IPs,
// and invalid port ranges (0, >65535).
function validateSelfHostedDomain(domain: string): string | null {
  // Extract host and optional port.
  let host = domain;
  let portStr: string | undefined;
  const colonIdx = domain.lastIndexOf(":");
  if (colonIdx !== -1) {
    host = domain.slice(0, colonIdx);
    portStr = domain.slice(colonIdx + 1);
  }

  // Validate port range.
  if (portStr !== undefined) {
    const port = Number(portStr);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      return "port must be 1-65535";
    }
  }

  // Reject localhost and .local/.localhost TLDs.
  const lowerHost = host.toLowerCase();
  if (
    lowerHost === "localhost" ||
    lowerHost.endsWith(".localhost") ||
    lowerHost.endsWith(".local")
  ) {
    return "private/internal hostnames (localhost, .local, .localhost) are not allowed";
  }

  // Reject literal private/loopback IPs.
  // Simple check for common patterns — backend does full CIDR validation.
  if (
    lowerHost === "127.0.0.1" ||
    lowerHost.startsWith("127.") ||
    lowerHost === "10.0.0.0" ||
    lowerHost.startsWith("10.") ||
    lowerHost.startsWith("192.168.") ||
    lowerHost.startsWith("172.16.") ||
    lowerHost.startsWith("172.17.") ||
    lowerHost.startsWith("172.18.") ||
    lowerHost.startsWith("172.19.") ||
    lowerHost.startsWith("172.2") ||
    lowerHost.startsWith("172.3") ||
    lowerHost === "169.254.169.254" ||
    lowerHost.startsWith("169.254.") ||
    lowerHost === "::1" ||
    lowerHost === "0.0.0.0"
  ) {
    return "IP is in a blocked range (loopback/private/metadata)";
  }

  return null;
}

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
    const domainLower = domain.toLowerCase();
    const isCloud = BITRIX_DOMAIN_RE.test(domainLower);
    const isSelfHostedSyntax = SELF_HOSTED_DOMAIN_RE.test(domainLower);
    if (!isCloud && !isSelfHostedSyntax) {
      e.domain = t("bitrix24.create.errors.invalidDomain", {
        defaultValue: "Must be a valid hostname (e.g. *.bitrix24.com, *.bitrix.info, or your self-hosted domain).",
      });
    } else if (!isCloud) {
      // SSRF + port validation for self-hosted domains.
      const ssrfErr = validateSelfHostedDomain(domainLower);
      if (ssrfErr) {
        e.domain = t("bitrix24.create.errors.invalidDomain", {
          defaultValue: ssrfErr,
        });
      }
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
          placeholder="mycorp.bitrix24.vn or bitrix.example.com"
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
