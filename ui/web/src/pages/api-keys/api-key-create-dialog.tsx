import { useState } from "react";
import { useTranslation } from "react-i18next";
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
import type { ApiKeyCreateInput } from "@/types/api-key";

const ALL_SCOPES = [
  "operator.admin",
  "operator.read",
  "operator.write",
  "operator.approvals",
  "operator.pairing",
] as const;

const EXPIRY_OPTIONS = [
  { value: "never", seconds: 0 },
  { value: "7d", seconds: 7 * 86400 },
  { value: "30d", seconds: 30 * 86400 },
  { value: "90d", seconds: 90 * 86400 },
] as const;

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: ApiKeyCreateInput) => Promise<void>;
}

export function ApiKeyCreateDialog({ open, onOpenChange, onCreate }: Props) {
  const { t } = useTranslation("api-keys");
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<string[]>([]);
  const [expiry, setExpiry] = useState("never");
  const [saving, setSaving] = useState(false);

  const toggleScope = (scope: string) => {
    setScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope],
    );
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim() || scopes.length === 0) return;
    setSaving(true);
    try {
      const expiryOption = EXPIRY_OPTIONS.find((o) => o.value === expiry);
      await onCreate({
        name: name.trim(),
        scopes,
        expires_in: expiryOption && expiryOption.seconds > 0 ? expiryOption.seconds : undefined,
      });
      // Reset form
      setName("");
      setScopes([]);
      setExpiry("never");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("form.title")}</DialogTitle>
          <DialogDescription>{t("description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="key-name">{t("form.name")}</Label>
            <Input
              id="key-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t("form.namePlaceholder")}
              className="text-base md:text-sm"
              required
            />
          </div>

          <div className="space-y-2">
            <Label>{t("form.scopes")}</Label>
            <div className="space-y-2">
              {ALL_SCOPES.map((scope) => (
                <label key={scope} className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="checkbox"
                    checked={scopes.includes(scope)}
                    onChange={() => toggleScope(scope)}
                    className="h-4 w-4 rounded border-gray-300"
                  />
                  <span className="text-base md:text-sm">
                    <span className="font-medium">{scope.replace("operator.", "")}</span>
                    <span className="text-muted-foreground ml-1">
                      — {t(`form.scopeDescriptions.${scope}`)}
                    </span>
                  </span>
                </label>
              ))}
            </div>
          </div>

          <div className="space-y-2">
            <Label htmlFor="key-expiry">{t("form.expiry")}</Label>
            <select
              id="key-expiry"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value)}
              className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base md:text-sm shadow-sm"
            >
              {EXPIRY_OPTIONS.map((opt) => (
                <option key={opt.value} value={opt.value}>
                  {t(`form.expiryOptions.${opt.value}`)}
                </option>
              ))}
            </select>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("created.done")}
            </Button>
            <Button type="submit" disabled={saving || !name.trim() || scopes.length === 0}>
              {saving ? t("form.creating") : t("form.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
