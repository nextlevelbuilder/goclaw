import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ShieldCheck, Check, X, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { PageHeader } from "@/components/shared/page-header";
import { EmptyState } from "@/components/shared/empty-state";
import { TableSkeleton } from "@/components/shared/loading-skeleton";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { formatRelativeTime } from "@/lib/format";
import { useApprovals, type PendingApproval } from "./hooks/use-approvals";
import { useMinLoading } from "@/hooks/use-min-loading";
import { useDeferredLoading } from "@/hooks/use-deferred-loading";

export function ApprovalsPage() {
  const { t } = useTranslation("approvals");
  const { t: tc } = useTranslation("common");
  const { pending, loading, refresh, approve, deny } = useApprovals();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && pending.length === 0);
  const [denyTarget, setDenyTarget] = useState<PendingApproval | null>(null);
  const [approveTarget, setApproveTarget] = useState<{ approval: PendingApproval; always: boolean } | null>(null);

  return (
    <div className="p-4 sm:p-6 pb-10">
      <PageHeader
        title={t("title")}
        description={t("description")}
        actions={
          <div className="flex items-center gap-2">
            {pending.length > 0 && (
              <Badge variant="destructive">{t("pending", { count: pending.length })}</Badge>
            )}
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> {tc("refresh")}
            </Button>
          </div>
        }
      />

      <div className="mt-4">
        {showSkeleton ? (
          <TableSkeleton rows={3} />
        ) : pending.length === 0 ? (
          <EmptyState
            icon={ShieldCheck}
            title={t("emptyTitle")}
            description={t("emptyDescription")}
          />
        ) : (
          <div className="space-y-3">
            {pending.map((approval: PendingApproval) => (
              <div key={approval.id} className="rounded-lg border p-4">
                <div className="flex items-start justify-between gap-4">
                  <div className="flex-1">
                    <div className="flex items-center gap-2">
                      <Badge variant="outline">{approval.agentId}</Badge>
                      <span className="text-xs text-muted-foreground">
                        {formatRelativeTime(new Date(approval.createdAt))}
                      </span>
                    </div>
                    <pre className="mt-2 rounded-md bg-muted p-3 text-sm">
                      {approval.command}
                    </pre>
                  </div>
                  <div className="flex flex-col gap-2">
                    <Button
                      size="sm"
                      onClick={() => setApproveTarget({ approval, always: false })}
                      className="gap-1"
                    >
                      <Check className="h-3.5 w-3.5" /> {t("allowOnce")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setApproveTarget({ approval, always: true })}
                      className="gap-1"
                    >
                      <Check className="h-3.5 w-3.5" /> {t("allowAlways")}
                    </Button>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => setDenyTarget(approval)}
                      className="gap-1"
                    >
                      <X className="h-3.5 w-3.5" /> {t("deny")}
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={!!approveTarget}
        onOpenChange={() => setApproveTarget(null)}
        title={approveTarget?.always ? t("confirmAllowAlways.title") : t("confirmAllowOnce.title")}
        description={
          approveTarget?.always
            ? t("confirmAllowAlways.description", { command: approveTarget.approval.command, agentId: approveTarget.approval.agentId })
            : t("confirmAllowOnce.description", { command: approveTarget?.approval.command, agentId: approveTarget?.approval.agentId })
        }
        confirmLabel={approveTarget?.always ? t("allowAlways") : t("allowOnce")}
        onConfirm={async () => {
          if (approveTarget) {
            await approve(approveTarget.approval.id, approveTarget.always);
            setApproveTarget(null);
          }
        }}
      />

      <ConfirmDialog
        open={!!denyTarget}
        onOpenChange={() => setDenyTarget(null)}
        title={t("confirmDeny.title")}
        description={t("confirmDeny.description", { command: denyTarget?.command, agentId: denyTarget?.agentId })}
        confirmLabel={t("confirmDeny.confirmLabel")}
        variant="destructive"
        onConfirm={async () => {
          if (denyTarget) {
            await deny(denyTarget.id);
            setDenyTarget(null);
          }
        }}
      />
    </div>
  );
}
