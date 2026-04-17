import { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Loader2, CheckCircle2, XCircle, Circle } from "lucide-react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import type { UpdateInfo, ApplyAllResult } from "../hooks/use-updates";

interface Props {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  updates: UpdateInfo[];
  /** Whether apply-all mutation is in flight */
  isPending: boolean;
  /** Result from the last apply-all call — used to render per-package status */
  result?: ApplyAllResult;
  onApply: (specs: string[]) => Promise<ApplyAllResult>;
}

type RowStatus = "pending" | "updating" | "succeeded" | "failed";

/**
 * Confirmation dialog for bulk package updates.
 * - Checkbox list lets users deselect packages before confirming.
 * - Shows per-package status during/after the mutation (from WS events or result).
 * Mobile: full-screen slide-up (via DialogContent default pattern in dialog.tsx).
 */
export function UpdateAllModal({
  open,
  onOpenChange,
  updates,
  isPending,
  result,
  onApply,
}: Props) {
  const { t } = useTranslation("packages");

  // Track which packages are selected (default: all)
  const [selected, setSelected] = useState<Set<string>>(() => new Set(updates.map((u) => u.name)));

  // Per-row status derived from in-progress WS events or final result
  const [rowStatus, setRowStatus] = useState<Record<string, RowStatus>>({});

  // Reset selection when modal opens with fresh update list
  useEffect(() => {
    if (open) {
      setSelected(new Set(updates.map((u) => u.name)));
      setRowStatus({});
    }
  }, [open, updates]);

  // Populate row status from the settled result
  useEffect(() => {
    if (!result) return;
    const next: Record<string, RowStatus> = {};
    for (const s of result.succeeded) {
      // package field is the full spec "github:name"
      const name = s.package.replace(/^github:/, "");
      next[name] = "succeeded";
    }
    for (const f of result.failed) {
      const name = f.package.replace(/^github:/, "");
      next[name] = "failed";
    }
    setRowStatus(next);
  }, [result]);

  const togglePackage = (name: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(name)) {
        next.delete(name);
      } else {
        next.add(name);
      }
      return next;
    });
  };

  const toggleAll = () => {
    if (selected.size === updates.length) {
      setSelected(new Set());
    } else {
      setSelected(new Set(updates.map((u) => u.name)));
    }
  };

  const handleApply = async () => {
    const specs = updates
      .filter((u) => selected.has(u.name))
      .map((u) => `github:${u.name}`);

    if (specs.length === 0) return;

    // Mark all selected as "updating" while in flight
    const updating: Record<string, RowStatus> = {};
    for (const name of selected) updating[name] = "updating";
    setRowStatus(updating);

    try {
      await onApply(specs);
    } finally {
      // Result effect will populate final status; modal stays open to show outcome
    }
    onOpenChange(false);
  };

  const selectedCount = selected.size;
  const allSelected = selectedCount === updates.length;
  const someSelected = selectedCount > 0 && !allSelected;

  const rowStatusIcon = (name: string) => {
    const s = rowStatus[name];
    if (s === "updating") return <Loader2 className="h-4 w-4 animate-spin text-sky-500" />;
    if (s === "succeeded") return <CheckCircle2 className="h-4 w-4 text-green-500" />;
    if (s === "failed") return <XCircle className="h-4 w-4 text-destructive" />;
    return <Circle className="h-4 w-4 text-muted-foreground/30" />;
  };

  return (
    <Dialog open={open} onOpenChange={isPending ? undefined : onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t("updates.confirmAllTitle", { count: updates.length })}
          </DialogTitle>
          <p className="text-sm text-muted-foreground">
            {t("updates.confirmAllBody")}
          </p>
        </DialogHeader>

        {/* Select-all toggle */}
        <div className="flex items-center gap-2 pb-1 border-b">
          <input
            type="checkbox"
            id="select-all"
            className="h-4 w-4 cursor-pointer accent-primary"
            checked={allSelected}
            ref={(el) => {
              if (el) el.indeterminate = someSelected;
            }}
            onChange={toggleAll}
            disabled={isPending}
          />
          <label htmlFor="select-all" className="text-sm font-medium cursor-pointer select-none">
            {t("updates.selected", { count: selectedCount })}
          </label>
        </div>

        {/* Package list */}
        <div className="max-h-[50vh] overflow-y-auto overscroll-contain divide-y">
          {updates.map((u) => {
            const isChecked = selected.has(u.name);
            const status = rowStatus[u.name];
            return (
              <label
                key={u.name}
                className="flex items-center gap-3 py-2.5 px-1 cursor-pointer hover:bg-muted/50 transition-colors"
              >
                <input
                  type="checkbox"
                  className="h-4 w-4 shrink-0 cursor-pointer accent-primary"
                  checked={isChecked}
                  onChange={() => togglePackage(u.name)}
                  disabled={isPending || !!status}
                />
                <div className="flex-1 min-w-0">
                  <span className="font-mono text-sm truncate block">{u.name}</span>
                  <span className="text-xs text-muted-foreground font-mono">
                    {u.currentVersion} → {u.latestVersion}
                    {u.meta?.prerelease && (
                      <span className="ml-1.5 text-amber-600 dark:text-amber-400">(pre-release)</span>
                    )}
                  </span>
                </div>
                <div className="shrink-0">{rowStatusIcon(u.name)}</div>
              </label>
            );
          })}
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={isPending}
          >
            {t("actions.cancel", { defaultValue: "Cancel" })}
          </Button>
          <Button
            onClick={handleApply}
            disabled={isPending || selectedCount === 0}
          >
            {isPending ? (
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />
            ) : null}
            {t("updates.updateAll")} ({selectedCount})
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
