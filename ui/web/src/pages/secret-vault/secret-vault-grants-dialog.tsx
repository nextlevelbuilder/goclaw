import { useEffect, useState } from "react";
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
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { useSecretVaultGrants } from "./hooks/use-secret-vault";
import type { SecretVaultEntry } from "@/types/secret-vault";

interface Props {
  entry: SecretVaultEntry | null;
  onClose: () => void;
}

export function SecretVaultGrantsDialog({ entry, onClose }: Props) {
  const { t } = useTranslation("secret-vault");
  const { agents } = useAgents();
  const { agentIds, loading, saveGrants } = useSecretVaultGrants(entry?.id ?? "", !!entry);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setSelected(new Set(agentIds));
  }, [agentIds, entry?.id]);

  const toggle = (id: string) =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const handleSave = async () => {
    setSaving(true);
    try {
      await saveGrants([...selected]);
      onClose();
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={!!entry} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("grants.title")}</DialogTitle>
          <DialogDescription>{t("grants.description", { name: entry?.name })}</DialogDescription>
        </DialogHeader>

        <div className="max-h-72 space-y-1 overflow-y-auto overscroll-contain">
          {agents.length === 0 && !loading && (
            <p className="text-sm text-muted-foreground">{t("grants.empty")}</p>
          )}
          {agents.map((a) => (
            <label key={a.id} className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-2 hover:bg-muted/50">
              <input
                type="checkbox"
                className="h-4 w-4"
                checked={selected.has(a.id)}
                onChange={() => toggle(a.id)}
              />
              <span className="text-sm">{a.display_name || a.agent_key}</span>
              <span className="text-xs text-muted-foreground font-mono">{a.agent_key}</span>
            </label>
          ))}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={loading || saving}>
            {t("grants.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
