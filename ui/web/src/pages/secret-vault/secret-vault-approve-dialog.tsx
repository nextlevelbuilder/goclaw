import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { SecretVaultEntry } from "@/types/secret-vault";
import { parseDomains } from "./secret-vault-utils";

interface Props {
  /** Pending entry to approve; null closes the dialog. Remount (key) per entry. */
  entry: SecretVaultEntry | null;
  /** Display name of the creating agent, or null when unknown or deleted. */
  agentName: string | null;
  onApprove: (id: string, domains: string[]) => Promise<void>;
  onClose: () => void;
}

export function SecretVaultApproveDialog({ entry, agentName, onApprove, onClose }: Props) {
  const { t } = useTranslation("secret-vault");
  // The agent's proposal is only a starting point: the user confirms or edits it.
  const [domains, setDomains] = useState(() => entry?.allowed_domains.join(", ") ?? "");
  const [saving, setSaving] = useState(false);
  const parsed = parseDomains(domains);

  const handleApprove = async () => {
    if (!entry || parsed.length === 0) return;
    setSaving(true);
    try {
      await onApprove(entry.id, parsed);
      onClose();
    } catch {
      // The hook already showed an error toast; keep the dialog open to retry.
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={!!entry} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-lg overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{t("approve.title")}</DialogTitle>
          <DialogDescription>{t("approve.description", { name: entry?.name })}</DialogDescription>
        </DialogHeader>

        <form
          autoComplete="off"
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            void handleApprove();
          }}
        >
          <div className="space-y-1.5">
            <div className="text-sm font-medium">{t("approve.agent")}</div>
            <div className="text-sm">
              {agentName ?? <span className="text-muted-foreground">{t("approve.unknownAgent")}</span>}
            </div>
          </div>

          <div className="space-y-1.5">
            <div className="text-sm font-medium">{t("approve.proposed")}</div>
            <div className="flex flex-wrap gap-1">
              {entry && entry.allowed_domains.length > 0 ? (
                entry.allowed_domains.map((d) => (
                  <Badge key={d} variant="outline" className="font-mono text-xs-plus px-1.5 py-0">
                    {d}
                  </Badge>
                ))
              ) : (
                <span className="text-sm text-muted-foreground">{t("approve.noneProposed")}</span>
              )}
            </div>
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="sv-approve-domains">{t("approve.domains")}</Label>
            <Input
              id="sv-approve-domains"
              value={domains}
              onChange={(e) => setDomains(e.target.value)}
              placeholder={t("form.domainsPlaceholder")}
              autoComplete="off"
            />
            <p className="text-xs text-muted-foreground">{t("approve.domainsHint")}</p>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={parsed.length === 0 || saving}>
              {t("approve.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
