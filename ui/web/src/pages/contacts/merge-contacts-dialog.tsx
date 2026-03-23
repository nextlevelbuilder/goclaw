import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Construction, Merge } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Combobox } from "@/components/ui/combobox";
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
import { useTenantUsersList } from "./hooks/use-tenant-users-list";

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
  const { users } = useTenantUsersList();

  const [mode, setMode] = useState<MergeMode>("existing");
  const [selectedUserId, setSelectedUserId] = useState("");
  const [newDisplayName, setNewDisplayName] = useState("");
  const [newUserId, setNewUserId] = useState("");
  // TODO: re-enable when merge feature is ready
  // const [submitting, setSubmitting] = useState(false);
  const setSubmitting = (_v: boolean) => {};

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

  const userOptions = users.map((u) => ({
    value: u.id,
    label: u.display_name || u.user_id,
  }));

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

  // TODO: re-enable when merge feature is ready
  // const canSubmit = mode === "existing" ? !!selectedUserId : !!(newUserId || defaultUserId);

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

        {/* Coming soon banner */}
        <div className="flex items-center gap-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
          <Construction className="h-4 w-4 shrink-0" />
          <span className="font-medium">{t("merge.comingSoon")}</span>
        </div>

        <div className="space-y-4 py-2 pointer-events-none opacity-50">
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
                <Combobox
                  value={selectedUserId}
                  onChange={setSelectedUserId}
                  options={userOptions}
                  placeholder={t("merge.selectUser")}
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
          <Button onClick={handleSubmit} disabled>
            {t("merge.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
