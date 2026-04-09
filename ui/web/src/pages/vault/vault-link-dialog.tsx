import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from "@/components/ui/dialog";
import { Combobox } from "@/components/ui/combobox";
import { useCreateLink, useVaultDocuments } from "./hooks/use-vault";
import type { VaultDocument } from "@/types/vault";

const LINK_TYPES = [
  { value: "reference", label: "Reference" },
  { value: "related", label: "Related" },
  { value: "extends", label: "Extends" },
  { value: "depends_on", label: "Depends on" },
  { value: "supersedes", label: "Supersedes" },
];

interface Props {
  agentId: string;
  fromDoc: VaultDocument;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated?: () => void;
}

export function VaultLinkDialog({ agentId, fromDoc, open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation("vault");
  const { create } = useCreateLink(agentId);
  const { documents } = useVaultDocuments(agentId, { limit: 100 });

  const [toDocId, setToDocId] = useState("");
  const [linkType, setLinkType] = useState("reference");
  const [context, setContext] = useState("");
  const [saving, setSaving] = useState(false);

  const docOptions = documents
    .filter((d) => d.id !== fromDoc.id)
    .map((d) => ({ value: d.id, label: d.title || d.path }));

  const reset = () => {
    setToDocId("");
    setLinkType("reference");
    setContext("");
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!toDocId || !linkType.trim()) return;
    setSaving(true);
    try {
      await create({
        from_doc_id: fromDoc.id,
        to_doc_id: toDocId,
        link_type: linkType.trim(),
        context: context.trim() || undefined,
      });
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
          <DialogTitle>{t("createLink")}</DialogTitle>
          <DialogDescription className="text-xs truncate">
            {t("fields.fromDoc")}: {fromDoc.title || fromDoc.path}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="link-to-doc">{t("fields.toDoc")} *</Label>
            <Combobox
              value={toDocId}
              onChange={setToDocId}
              options={docOptions}
              placeholder={t("fields.selectDoc")}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="link-type">{t("fields.linkType")}</Label>
            <Combobox
              value={linkType}
              onChange={setLinkType}
              options={LINK_TYPES}
              allowCustom
              customLabel={t("fields.customType") || "Custom:"}
              placeholder="reference"
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="link-context">{t("fields.linkContext")}</Label>
            <Textarea
              id="link-context"
              value={context}
              onChange={(e) => setContext(e.target.value)}
              placeholder={t("fields.linkContextPlaceholder")}
              className="text-base md:text-sm resize-none"
              rows={2}
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => { reset(); onOpenChange(false); }} disabled={saving}>
              {t("cancel")}
            </Button>
            <Button type="submit" disabled={saving || !toDocId || !linkType.trim()}>
              {saving ? t("saving") : t("createLink")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
