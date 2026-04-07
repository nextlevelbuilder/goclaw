import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, X, RotateCcw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { formatRelativeTime } from "@/lib/format";
import type { EvolutionSuggestion } from "@/types/evolution";

const TYPE_COLORS: Record<string, string> = {
  threshold: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  tool_order: "bg-orange-100 text-orange-700 dark:bg-orange-900 dark:text-orange-300",
  skill_add: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  memory_prune: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
};

const STATUS_COLORS: Record<string, string> = {
  pending: "bg-yellow-100 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300",
  approved: "bg-blue-100 text-blue-700 dark:bg-blue-900 dark:text-blue-300",
  applied: "bg-green-100 text-green-700 dark:bg-green-900 dark:text-green-300",
  rejected: "bg-red-100 text-red-700 dark:bg-red-900 dark:text-red-300",
  rolled_back: "bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300",
};

interface Props {
  suggestions: EvolutionSuggestion[];
  loading: boolean;
  onUpdateStatus: (id: string, status: "approved" | "rejected" | "rolled_back") => Promise<void>;
}

export function EvolutionSuggestionsTable({ suggestions, loading, onUpdateStatus }: Props) {
  const { t } = useTranslation("agents");
  const [confirm, setConfirm] = useState<{ id: string; action: "approved" | "rejected" | "rolled_back" } | null>(null);
  const [acting, setActing] = useState(false);

  const handleConfirm = async () => {
    if (!confirm) return;
    setActing(true);
    try {
      await onUpdateStatus(confirm.id, confirm.action);
    } finally {
      setActing(false);
      setConfirm(null);
    }
  };

  if (loading) {
    return <div className="h-[120px] animate-pulse rounded-md bg-muted" />;
  }

  if (suggestions.length === 0) {
    return <p className="text-xs text-muted-foreground py-4">{t("detail.evolution.noSuggestions")}</p>;
  }

  return (
    <>
      <div className="rounded-md border">
        <div className="overflow-x-auto">
          <table className="w-full text-sm min-w-[600px]">
            <thead>
              <tr className="border-b bg-muted/50 text-left">
                <th className="px-3 py-2 font-medium">Type</th>
                <th className="px-3 py-2 font-medium">Suggestion</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Created</th>
                <th className="px-3 py-2 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {suggestions.map((s) => (
                <tr key={s.id} className="border-b hover:bg-muted/30">
                  <td className="px-3 py-2">
                    <Badge variant="outline" className={TYPE_COLORS[s.suggestion_type] ?? ""}>
                      {s.suggestion_type}
                    </Badge>
                  </td>
                  <td className="px-3 py-2 max-w-[280px]">
                    <p className="text-sm truncate" title={s.suggestion}>{s.suggestion}</p>
                    <p className="text-xs text-muted-foreground truncate" title={s.rationale}>{s.rationale}</p>
                  </td>
                  <td className="px-3 py-2">
                    <Badge variant="outline" className={STATUS_COLORS[s.status] ?? ""}>
                      {s.status}
                    </Badge>
                  </td>
                  <td className="px-3 py-2 text-xs text-muted-foreground whitespace-nowrap">
                    {formatRelativeTime(s.created_at)}
                  </td>
                  <td className="px-3 py-2 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {s.status === "pending" && (
                        <>
                          <Button
                            size="sm" variant="ghost"
                            className="h-7 w-7 p-0 text-green-600 hover:text-green-700"
                            title={t("detail.evolution.approve")}
                            onClick={() => setConfirm({ id: s.id, action: "approved" })}
                          >
                            <Check className="h-4 w-4" />
                          </Button>
                          <Button
                            size="sm" variant="ghost"
                            className="h-7 w-7 p-0 text-red-600 hover:text-red-700"
                            title={t("detail.evolution.reject")}
                            onClick={() => setConfirm({ id: s.id, action: "rejected" })}
                          >
                            <X className="h-4 w-4" />
                          </Button>
                        </>
                      )}
                      {s.status === "applied" && (
                        <Button
                          size="sm" variant="ghost"
                          className="h-7 w-7 p-0 text-orange-600 hover:text-orange-700"
                          title={t("detail.evolution.rollback")}
                          onClick={() => setConfirm({ id: s.id, action: "rolled_back" })}
                        >
                          <RotateCcw className="h-4 w-4" />
                        </Button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* Confirmation dialog */}
      <Dialog open={!!confirm} onOpenChange={(open: boolean) => !open && setConfirm(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {confirm?.action === "approved" && t("detail.evolution.confirmApprove")}
              {confirm?.action === "rejected" && t("detail.evolution.confirmReject")}
              {confirm?.action === "rolled_back" && t("detail.evolution.confirmRollback")}
            </DialogTitle>
            <DialogDescription>
              {t("detail.evolution.confirmDescription")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setConfirm(null)} disabled={acting}>
              Cancel
            </Button>
            <Button onClick={handleConfirm} disabled={acting}>
              {acting ? "..." : "Confirm"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
