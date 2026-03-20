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
}

/** Sticky footer bar with save button and loading spinner. Toast handles success/error feedback. */
export function StickySaveBar({
  onSave,
  saving,
  disabled,
  label,
  savingLabel,
}: StickySaveBarProps) {
  const { t } = useTranslation("common");
  const showSpin = useMinLoading(saving, 600);
  const resolvedLabel = label ?? t("save");
  const resolvedSavingLabel = savingLabel ?? t("saving");
  return (
    <div className="sticky bottom-0 z-10 -mx-3 mt-6 border-t bg-background/80 px-3 py-3 backdrop-blur-sm sm:-mx-4 sm:px-4">
      <div className="flex items-center justify-end gap-2">
        <Button onClick={onSave} disabled={saving || showSpin || disabled}>
          {showSpin ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {showSpin ? resolvedSavingLabel : resolvedLabel}
        </Button>
      </div>
    </div>
  );
}
