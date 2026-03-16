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
import type { GatewayUserCreateInput } from "@/types/gateway-user";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreate: (input: GatewayUserCreateInput) => Promise<void>;
}

export function GatewayUserCreateDialog({ open, onOpenChange, onCreate }: Props) {
  const { t } = useTranslation("gateway-users");
  const [userId, setUserId] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!userId.trim()) return;
    setSaving(true);
    try {
      await onCreate({ user_id: userId.trim() });
      setUserId("");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-sm:inset-0 max-sm:translate-x-0 max-sm:translate-y-0 sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("form.title")}</DialogTitle>
          <DialogDescription>{t("form.description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="gw-user-id">{t("form.userId")}</Label>
            <Input
              id="gw-user-id"
              value={userId}
              onChange={(e) => setUserId(e.target.value)}
              placeholder={t("form.userIdPlaceholder")}
              className="text-base md:text-sm"
              maxLength={255}
              required
            />
          </div>

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("form.cancel")}
            </Button>
            <Button type="submit" disabled={saving || !userId.trim()}>
              {saving ? t("form.creating") : t("form.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
