import { Save, Check, AlertCircle } from "lucide-react";
import { Button } from "@/components/ui/button";

interface StickySaveBarProps {
  onSave: () => void;
  saving: boolean;
  saved: boolean;
  error?: string | null;
  label?: string;
  savingLabel?: string;
  savedLabel?: string;
}

/** Sticky footer bar with save button, saving spinner, and success/error feedback. */
export function StickySaveBar({
  onSave,
  saving,
  saved,
  error,
  label = "Save",
  savingLabel = "Saving...",
  savedLabel = "Saved",
}: StickySaveBarProps) {
  return (
    <div className="sticky bottom-0 z-10 -mx-3 mt-6 border-t bg-background/80 px-3 py-3 backdrop-blur-sm sm:-mx-4 sm:px-4">
      {error && (
        <div className="mb-2 flex items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          {error}
        </div>
      )}
      <div className="flex items-center justify-end gap-2">
        {saved && (
          <span className="flex items-center gap-1 text-sm text-success">
            <Check className="h-3.5 w-3.5" /> {savedLabel}
          </span>
        )}
        <Button onClick={onSave} disabled={saving}>
          {!saving && <Save className="h-4 w-4" />}
          {saving ? savingLabel : label}
        </Button>
      </div>
    </div>
  );
}
