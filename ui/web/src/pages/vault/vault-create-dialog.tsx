import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from "@/components/ui/dialog";
import { useCreateDocument } from "./hooks/use-vault";

const DOC_TYPES = ["context", "memory", "note", "skill"] as const;
const SCOPES = ["personal", "team", "shared"] as const;

interface Props {
  agentId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: () => void;
}

export function VaultCreateDialog({ agentId, open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation("vault");
  const { create } = useCreateDocument(agentId);

  const [title, setTitle] = useState("");
  const [path, setPath] = useState("");
  const [docType, setDocType] = useState<string>("note");
  const [scope, setScope] = useState<string>("personal");
  const [saving, setSaving] = useState(false);

  const reset = () => {
    setTitle("");
    setPath("");
    setDocType("note");
    setScope("personal");
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!title.trim() || !path.trim()) return;
    setSaving(true);
    try {
      await create({ title: title.trim(), path: path.trim(), doc_type: docType, scope });
      reset();
      onCreated?.();
      onOpenChange(false);
    } catch {
      // error toasted in hook
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!saving) { reset(); onOpenChange(v); } }}>
      <DialogContent className="sm:max-w-md max-sm:inset-0">
        <DialogHeader>
          <DialogTitle>{t("createDoc")}</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="vault-title">{t("fields.title")} *</Label>
            <Input
              id="vault-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder={t("fields.titlePlaceholder")}
              className="text-base md:text-sm"
              required
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="vault-path">{t("fields.path")} *</Label>
            <Input
              id="vault-path"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              placeholder={t("fields.pathPlaceholder")}
              className="text-base md:text-sm font-mono"
              required
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="vault-doctype">{t("fields.docType")}</Label>
              <select
                id="vault-doctype"
                value={docType}
                onChange={(e) => setDocType(e.target.value)}
                className="w-full text-base md:text-sm border rounded px-2 py-1.5 bg-background"
              >
                {DOC_TYPES.map((dt) => (
                  <option key={dt} value={dt}>{t(`type.${dt}`)}</option>
                ))}
              </select>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="vault-scope">{t("fields.scope")}</Label>
              <select
                id="vault-scope"
                value={scope}
                onChange={(e) => setScope(e.target.value)}
                className="w-full text-base md:text-sm border rounded px-2 py-1.5 bg-background"
              >
                {SCOPES.map((s) => (
                  <option key={s} value={s}>{t(`scope.${s}`)}</option>
                ))}
              </select>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => { reset(); onOpenChange(false); }} disabled={saving}>
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={saving || !title.trim() || !path.trim()}>
              {saving ? t("saving") : t("create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
