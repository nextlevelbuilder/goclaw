import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import { vaultLinkSchema, type VaultLinkFormData } from "@/schemas/vault.schema";

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
  const [saving, setSaving] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<VaultLinkFormData>({
    resolver: zodResolver(vaultLinkSchema),
    defaultValues: {
      toDocId: "",
      linkType: "reference",
      context: "",
    },
  });

  const toDocId = watch("toDocId");
  const linkType = watch("linkType");

  const docOptions = documents
    .filter((d) => d.id !== fromDoc.id)
    .map((d) => ({ value: d.id, label: d.title || d.path }));

  const onValid = async (data: VaultLinkFormData) => {
    setSaving(true);
    try {
      await create({
        from_doc_id: fromDoc.id,
        to_doc_id: data.toDocId,
        link_type: data.linkType.trim(),
        context: data.context?.trim() || undefined,
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

  const handleClose = (v: boolean) => {
    if (!saving) { reset(); onOpenChange(v); }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md max-sm:inset-0">
        <DialogHeader>
          <DialogTitle>{t("createLink")}</DialogTitle>
          <DialogDescription className="text-xs truncate">
            {t("fields.fromDoc")}: {fromDoc.title || fromDoc.path}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit(onValid)} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="link-to-doc">{t("fields.toDoc")} *</Label>
            <Combobox
              value={toDocId}
              onChange={(v) => setValue("toDocId", v, { shouldValidate: true })}
              options={docOptions}
              placeholder={t("fields.selectDoc")}
            />
            {errors.toDocId && (
              <p className="text-xs text-destructive">{errors.toDocId.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="link-type">{t("fields.linkType")}</Label>
            <Combobox
              value={linkType}
              onChange={(v) => setValue("linkType", v, { shouldValidate: true })}
              options={LINK_TYPES}
              allowCustom
              customLabel={t("fields.customType") || "Custom:"}
              placeholder="reference"
            />
            {errors.linkType && (
              <p className="text-xs text-destructive">{errors.linkType.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="link-context">{t("fields.linkContext")}</Label>
            <Textarea
              id="link-context"
              {...register("context")}
              placeholder={t("fields.linkContextPlaceholder")}
              className="text-base md:text-sm resize-none"
              rows={2}
            />
            {errors.context && (
              <p className="text-xs text-destructive">{errors.context.message}</p>
            )}
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
