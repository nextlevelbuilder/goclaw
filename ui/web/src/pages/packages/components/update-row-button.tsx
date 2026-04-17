import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowUpCircle, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type { UpdateInfo } from "../hooks/use-updates";

interface Props {
  update: UpdateInfo;
  /** Whether any global apply-all mutation is in flight (disables all row buttons) */
  globalPending?: boolean;
  isMaster: boolean;
  onUpdate: (spec: string) => void;
}

/**
 * Inline "Update" button rendered inside each GitHub Binaries table row.
 * - Renders only when an update is available for the row's package.
 * - Disabled (not hidden) for non-master users with an explanatory tooltip.
 * - Tracks its own local pending state so rapid clicks don't double-fire.
 */
export function UpdateRowButton({ update, globalPending, isMaster, onUpdate }: Props) {
  const { t } = useTranslation("packages");
  const [localPending, setLocalPending] = useState(false);

  const isPending = localPending || !!globalPending;
  const spec = `github:${update.name}`;

  const handleClick = () => {
    if (isPending || !isMaster) return;
    setLocalPending(true);
    try {
      onUpdate(spec);
    } finally {
      // Reset after a short delay — the parent invalidates the query on success
      // so the button will unmount once the update info is gone.
      setTimeout(() => setLocalPending(false), 3000);
    }
  };

  const tooltipText = !isMaster
    ? t("updates.adminOnly")
    : `${update.currentVersion} → ${update.latestVersion}`;

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          {/* Wrap in span so Tooltip works on disabled buttons */}
          <span className="inline-flex">
            <Button
              variant="outline"
              size="sm"
              className="h-7 px-2 gap-1 text-xs"
              disabled={isPending || !isMaster}
              onClick={handleClick}
              aria-label={t("updates.update")}
            >
              {isPending ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin" />
              ) : (
                <ArrowUpCircle className="h-3.5 w-3.5" />
              )}
              {t("updates.update")}
            </Button>
          </span>
        </TooltipTrigger>
        <TooltipContent side="top">
          <p>{tooltipText}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
