import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from "@/components/ui/dialog";
import { useCreateDocument } from "./hooks/use-vault";
import { vaultCreateSchema, type VaultCreateFormData } from "@/schemas/vault.schema";

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
  const [saving, setSaving] = useState(false);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<VaultCreateFormData>({
    resolver: zodResolver(vaultCreateSchema),
    defaultValues: {
      title: "",
      path: "",
      docType: "note",
      scope: "personal",
    },
  });

  const docType = watch("docType");
  const scope = watch("scope");

  const onValid = async (data: VaultCreateFormData) => {
    setSaving(true);
    try {
      await create({ title: data.title.trim(), path: data.path.trim(), doc_type: data.docType, scope: data.scope });
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
          <DialogTitle>{t("createDoc")}</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit(onValid)} className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="vault-title">{t("fields.title")} *</Label>
            <Input
              id="vault-title"
              {...register("title")}
              placeholder={t("fields.titlePlaceholder")}
              className="text-base md:text-sm"
            />
            {errors.title && (
              <p className="text-xs text-destructive">{errors.title.message}</p>
            )}
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="vault-path">{t("fields.path")} *</Label>
            <Input
              id="vault-path"
              {...register("path")}
              placeholder={t("fields.pathPlaceholder")}
              className="text-base md:text-sm font-mono"
            />
            {errors.path && (
              <p className="text-xs text-destructive">{errors.path.message}</p>
            )}
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div className="space-y-1.5">
              <Label htmlFor="vault-doctype">{t("fields.docType")}</Label>
              <select
                id="vault-doctype"
                value={docType}
                onChange={(e) => setValue("docType", e.target.value, { shouldValidate: true })}
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
                onChange={(e) => setValue("scope", e.target.value, { shouldValidate: true })}
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
            <Button type="submit" disabled={saving}>
              {saving ? t("saving") : t("create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
