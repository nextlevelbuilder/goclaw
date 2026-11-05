import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
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
import type { SecretKind, SecretVaultEntry, SecretVaultCreateInput } from "@/types/secret-vault";
import { defaultApproval, isValidSecretName, parseDomains } from "./secret-vault-utils";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Present when editing: metadata is editable, the stored value can only be replaced. */
  entry?: SecretVaultEntry | null;
  onCreate: (input: SecretVaultCreateInput) => Promise<unknown>;
  onUpdate: (
    id: string,
    meta: { allowed_domains: string[]; require_approval: boolean; hints: string },
    value?: { username?: string; secret: string },
  ) => Promise<unknown>;
}

export function SecretVaultFormDialog({ open, onOpenChange, entry, onCreate, onUpdate }: Props) {
  const { t } = useTranslation("secret-vault");
  const editing = !!entry;

  const [name, setName] = useState("");
  const [kind, setKind] = useState<SecretKind>("api_key");
  const [domains, setDomains] = useState("");
  const [approval, setApproval] = useState(false);
  const [hints, setHints] = useState("");
  const [username, setUsername] = useState("");
  // The secret lives only in this component and is wiped on submit and on close.
  const [secret, setSecret] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) {
      setSecret("");
      return;
    }
    setName(entry?.name ?? "");
    setKind(entry?.kind ?? "api_key");
    setDomains(entry?.allowed_domains.join(", ") ?? "");
    setApproval(entry?.require_approval ?? defaultApproval("api_key"));
    setHints(entry?.hints ?? "");
    setUsername("");
    setSecret("");
  }, [open, entry]);

  const domainList = parseDomains(domains);
  const needsUsername = kind === "login";
  const hasValue = secret !== "" || username !== "";
  const valueOk = secret !== "" && (!needsUsername || username !== "");
  const valid = domainList.length > 0 && (editing ? !hasValue || valueOk : isValidSecretName(name) && valueOk);

  const handleKind = (k: SecretKind) => {
    setKind(k);
    setApproval(defaultApproval(k));
  };

  const handleSubmit = async () => {
    if (!valid) return;
    setSaving(true);
    const value = hasValue ? { username: needsUsername ? username : undefined, secret } : undefined;
    try {
      if (entry) {
        await onUpdate(entry.id, { allowed_domains: domainList, require_approval: approval, hints }, value);
      } else {
        await onCreate({
          name,
          kind,
          allowed_domains: domainList,
          require_approval: approval,
          hints,
          username: needsUsername ? username : undefined,
          secret,
        });
      }
      setSecret("");
      onOpenChange(false);
    } finally {
      setSecret("");
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{editing ? t("form.editTitle") : t("form.createTitle")}</DialogTitle>
        </DialogHeader>

        <form
          autoComplete="off"
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void handleSubmit();
          }}
        >
          <div className="space-y-1.5">
            <Label htmlFor="sv-name">{t("form.name")}</Label>
            <Input
              id="sv-name"
              value={name}
              disabled={editing}
              onChange={(e) => setName(e.target.value.toLowerCase())}
              placeholder={t("form.namePlaceholder")}
              autoComplete="off"
            />
            <p className="text-xs text-muted-foreground">{t("form.nameHint", { example: "{{secret:name}}" })}</p>
          </div>

          <div className="space-y-1.5">
            <Label>{t("form.kind")}</Label>
            <Select value={kind} onValueChange={(v) => handleKind(v as SecretKind)} disabled={editing}>
              <SelectTrigger className="text-base md:text-sm">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="api_key">{t("kind.api_key")}</SelectItem>
                <SelectItem value="login">{t("kind.login")}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sv-domains">{t("form.domains")}</Label>
            <Input
              id="sv-domains"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              placeholder={t("form.domainsPlaceholder")}
              autoComplete="off"
            />
            <p className="text-xs text-muted-foreground">{t("form.domainsHint")}</p>
          </div>

          {needsUsername && (
            <div className="space-y-1.5">
              <Label htmlFor="sv-username">{t("form.username")}</Label>
              <Input
                id="sv-username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="off"
              />
            </div>
          )}

          <div className="space-y-1.5">
            <Label htmlFor="sv-secret">{needsUsername ? t("form.password") : t("form.secret")}</Label>
            <Input
              id="sv-secret"
              type="password"
              value={secret}
              onChange={(e) => setSecret(e.target.value)}
              autoComplete="new-password"
              spellCheck={false}
            />
            <p className="text-xs text-muted-foreground">
              {editing ? t("form.valueSaved") : t("form.secretHint")}
            </p>
          </div>

          <div className="flex items-start justify-between gap-3 rounded-md border p-3">
            <div>
              <Label htmlFor="sv-approval">{t("form.approval")}</Label>
              <p className="text-xs text-muted-foreground">{t("form.approvalHint")}</p>
            </div>
            <Switch id="sv-approval" checked={approval} onCheckedChange={setApproval} />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sv-hints">{t("form.hints")}</Label>
            <Textarea
              id="sv-hints"
              value={hints}
              onChange={(e) => setHints(e.target.value)}
              placeholder={t("form.hintsPlaceholder")}
              rows={2}
              className="text-base md:text-sm"
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={!valid || saving}>
              {t("form.save")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
