import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

interface ConfirmDeleteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  confirmValue: string;
  confirmLabel?: string;
  onConfirm: () => void;
  loading?: boolean;
}

export function ConfirmDeleteDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmValue,
  confirmLabel,
  onConfirm,
  loading,
}: ConfirmDeleteDialogProps) {
  const { t } = useTranslation("common");
  const [inputValue, setInputValue] = useState("");

  const normalizeForCompare = (value: string) => value.normalize("NFC").trim().toLocaleLowerCase();
  const confirmationTarget = confirmValue.trim() || confirmValue;

  useEffect(() => {
    if (!open) setInputValue("");
  }, [open]);

  const isMatch = confirmValue
    ? normalizeForCompare(inputValue) === normalizeForCompare(confirmValue)
    : inputValue.trim().length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <div className="py-2">
          <p className="mb-2 text-sm text-muted-foreground">
            {t("typeToConfirmPrefix")} <span className="font-semibold text-foreground">{confirmationTarget}</span> {t("typeToConfirmSuffix")}
          </p>
          <Input
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            placeholder={confirmationTarget}
            autoFocus
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={loading}>
            {t("cancel")}
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={!isMatch || loading}
          >
            {loading ? "..." : (confirmLabel ?? t("delete"))}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
