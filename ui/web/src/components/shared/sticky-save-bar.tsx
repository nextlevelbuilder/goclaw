import { Save, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useTranslation } from "react-i18next";
import { useMinLoading } from "@/hooks/use-min-loading";

interface StickySaveBarProps {
  onSave: () => void;
  saving: boolean;
  disabled?: boolean;
  label?: string;
  savingLabel?: string;
  variant?: "footer" | "floating";
}

/** Sticky save action. Toast handles success/error feedback. */
export function StickySaveBar({
  onSave,
  saving,
  disabled,
  label,
  savingLabel,
  variant = "footer",
}: StickySaveBarProps) {
  const { t } = useTranslation("common");
  const showSpin = useMinLoading(saving, 600);
  const resolvedLabel = label ?? t("save");
  const resolvedSavingLabel = savingLabel ?? t("saving");

  if (variant === "floating") {
    return (
      <div className="pointer-events-none fixed inset-x-0 bottom-4 z-30 flex justify-end px-4 sm:px-6 lg:px-8 safe-bottom">
        <div className="pointer-events-auto flex items-center justify-end gap-2 rounded-xl border bg-background/95 p-2 shadow-lg backdrop-blur-sm">
          <Button onClick={onSave} disabled={saving || showSpin || disabled}>
            {showSpin ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
            {showSpin ? resolvedSavingLabel : resolvedLabel}
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="sticky bottom-0 z-20 -mx-3 mt-6 bg-gradient-to-t from-background via-background/95 to-background/0 px-3 pb-1 pt-6 sm:-mx-4 sm:px-4">
      <div className="flex items-center justify-end border-t border-border/70 pt-3">
        <Button onClick={onSave} disabled={saving || showSpin || disabled}>
          {showSpin ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {showSpin ? resolvedSavingLabel : resolvedLabel}
        </Button>
      </div>
    </div>
  );
}
