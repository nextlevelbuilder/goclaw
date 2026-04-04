/**
 * InlineAddForm — per-group manager add form used inside ChannelManagersTab.
 * Two render modes:
 * - with groupId prop: compact inline form (no group ID field)
 * - showGroupField: expanded form with group ID + user ID fields
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Plus, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { UserPickerCombobox } from "@/components/shared/user-picker-combobox";

export interface InlineAddFormProps {
  groupId?: string;
  showGroupField?: boolean;
  onAdd: (groupId: string, userId: string, displayName: string, username: string) => Promise<void>;
}

export function InlineAddForm({ groupId, showGroupField, onAdd }: InlineAddFormProps) {
  const { t } = useTranslation("channels");
  const [formGroupId, setFormGroupId] = useState("");
  const [userId, setUserId] = useState("");
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState("");

  const handleSubmit = async () => {
    const gid = groupId || formGroupId.trim();
    const uid = userId.trim();
    if (!gid || !uid) {
      setError(t("detail.managers.addForm.errors.groupUserRequired"));
      return;
    }
    setAdding(true);
    setError("");
    try {
      await onAdd(gid, uid, "", "");
      setUserId("");
      if (!groupId) setFormGroupId("");
    } catch (err) {
      setError(err instanceof Error ? err.message : t("detail.managers.addForm.errors.failedAdd"));
    } finally {
      setAdding(false);
    }
  };

  if (showGroupField) {
    return (
      <fieldset className="rounded-md border p-4 space-y-3">
        <legend className="px-1 text-sm font-medium">{t("detail.managers.addForm.title")}</legend>
        <p className="text-xs text-muted-foreground">{t("detail.managers.addForm.hint")}</p>
        <div className="flex flex-wrap items-end gap-2">
          <div className="grid gap-1.5 flex-1 min-w-[180px]">
            <Label className="text-xs">{t("detail.managers.addForm.groupId")}</Label>
            <Input
              value={formGroupId}
              onChange={(e) => setFormGroupId(e.target.value)}
              placeholder={t("detail.managers.addForm.groupIdPlaceholder")}
              className="text-base md:text-sm"
            />
          </div>
          <div className="grid gap-1.5 flex-1 min-w-[180px]">
            <Label className="text-xs">{t("detail.managers.addForm.userId")}</Label>
            <UserPickerCombobox
              value={userId}
              onChange={setUserId}
              placeholder={t("detail.managers.addForm.userIdPlaceholder")}
            />
          </div>
          <Button
            onClick={handleSubmit}
            disabled={adding || !formGroupId.trim() || !userId.trim()}
            size="sm"
            className="h-9 gap-1 shrink-0"
          >
            {adding ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
            {t("detail.managers.addForm.addManager")}
          </Button>
        </div>
        {error && <p className="text-sm text-destructive mt-1">{error}</p>}
      </fieldset>
    );
  }

  return (
    <div className="flex items-end gap-2">
      <div className="grid gap-1 flex-1 min-w-[140px]">
        <Label className="text-xs text-muted-foreground">{t("detail.managers.addForm.userId")}</Label>
        <UserPickerCombobox
          value={userId}
          onChange={setUserId}
          placeholder={t("detail.managers.addForm.userIdPlaceholder")}
          className="h-8"
        />
      </div>
      <Button
        size="sm"
        className="h-8 gap-1 shrink-0"
        onClick={handleSubmit}
        disabled={adding || !userId.trim()}
      >
        {adding ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Plus className="h-3.5 w-3.5" />}
        {t("detail.managers.addForm.add")}
      </Button>
      {error && <p className="text-xs text-destructive mt-1">{error}</p>}
    </div>
  );
}
