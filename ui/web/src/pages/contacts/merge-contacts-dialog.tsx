import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Merge } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "@/stores/use-toast-store";
import type { ChannelContact } from "@/types/contact";
import { useContactMerge } from "./hooks/use-contact-merge";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";

interface MergeContactsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  selectedContacts: ChannelContact[];
  onSuccess: () => void;
}

type MergeMode = "existing" | "create";

export function MergeContactsDialog({
  open,
  onOpenChange,
  selectedContacts,
  onSuccess,
}: MergeContactsDialogProps) {
  const { t } = useTranslation("contacts");
  const { merge } = useContactMerge();

  const [mode, setMode] = useState<MergeMode>("existing");
  const [selectedUserId, setSelectedUserId] = useState("");
  const [newDisplayName, setNewDisplayName] = useState("");
  const [newUserId, setNewUserId] = useState("");
  const [submitting, setSubmitting] = useState(false);

  // Reset form state when dialog opens
  useEffect(() => {
    if (open) {
      setMode("existing");
      setSelectedUserId("");
      setNewDisplayName("");
      setNewUserId("");
    }
  }, [open]);

  // Derive default user_id from first contact's username
  const defaultUserId =
    selectedContacts[0]?.username || selectedContacts[0]?.sender_id || "";

  const handleSubmit = async () => {
    const contactIds = selectedContacts.map((c) => c.id);
    setSubmitting(true);
    try {
      if (mode === "existing") {
        if (!selectedUserId) return;
        await merge({ contact_ids: contactIds, tenant_user_id: selectedUserId });
      } else {
        const userId = newUserId || defaultUserId;
        if (!userId) return;
        await merge({
          contact_ids: contactIds,
          create_user: {
            user_id: userId,
            display_name: newDisplayName || undefined,
          },
        });
      }
      toast.success(t("merge.dialogTitle"), t("merge.success"));
      onOpenChange(false);
      onSuccess();
    } catch (err) {
      toast.error(t("merge.dialogTitle"), err instanceof Error ? err.message : t("merge.error"));
    } finally {
      setSubmitting(false);
    }
  };

  const canSubmit = mode === "existing" ? !!selectedUserId : !!(newUserId || defaultUserId);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Merge className="h-4 w-4" />
            {t("merge.dialogTitle")}
          </DialogTitle>
          <DialogDescription>{t("merge.dialogDescription")}</DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {/* Mode selection — simple radio buttons */}
          <div className="space-y-2">
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                name="merge-mode"
                checked={mode === "existing"}
                onChange={() => setMode("existing")}
                className="accent-primary"
              />
              <span className="text-sm font-medium">{t("merge.linkExisting")}</span>
            </label>

            {mode === "existing" && (
              <div className="ml-6">
                <UserPickerCombobox
                  value={selectedUserId}
                  onChange={setSelectedUserId}
                  placeholder={t("merge.selectUser")}
                  source="tenant_user"
                />
              </div>
            )}

            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="radio"
                name="merge-mode"
                checked={mode === "create"}
                onChange={() => setMode("create")}
                className="accent-primary"
              />
              <span className="text-sm font-medium">{t("merge.createNew")}</span>
            </label>

            {mode === "create" && (
              <div className="ml-6 space-y-3">
                <div>
                  <Label className="text-xs">{t("merge.displayName")}</Label>
                  <Input
                    value={newDisplayName}
                    onChange={(e) => setNewDisplayName(e.target.value)}
                    placeholder={t("merge.displayNamePlaceholder")}
                    className="mt-1"
                  />
                </div>
                <div>
                  <Label className="text-xs">{t("merge.userId")}</Label>
                  <Input
                    value={newUserId}
                    onChange={(e) => setNewUserId(e.target.value)}
                    placeholder={defaultUserId || t("merge.userIdPlaceholder")}
                    className="mt-1"
                  />
                  {!newUserId && defaultUserId && (
                    <p className="text-xs text-muted-foreground mt-1">
                      Default: {defaultUserId}
                    </p>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* Selected contacts summary */}
          <div className="text-xs text-muted-foreground border-t pt-2">
            {t("selectedCount", { count: selectedContacts.length })}
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("merge.cancel", { defaultValue: "Cancel" })}
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit || submitting}>
            {t("merge.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
