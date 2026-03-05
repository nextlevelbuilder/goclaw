import { useState } from "react";
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
  const { pending, loading, refresh, approve, deny } = useApprovals();
  const spinning = useMinLoading(loading);
  const showSkeleton = useDeferredLoading(loading && pending.length === 0);
  const [denyTarget, setDenyTarget] = useState<PendingApproval | null>(null);
  const [approveTarget, setApproveTarget] = useState<{ approval: PendingApproval; always: boolean } | null>(null);

  return (
    <div className="p-4 sm:p-6">
      <PageHeader
        title="Approvals"
        description="Pending execution approvals"
        actions={
          <div className="flex items-center gap-2">
            {pending.length > 0 && (
              <Badge variant="destructive">{pending.length} pending</Badge>
            )}
            <Button variant="outline" size="sm" onClick={refresh} disabled={spinning} className="gap-1">
              <RefreshCw className={"h-3.5 w-3.5" + (spinning ? " animate-spin" : "")} /> Refresh
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
            title="No pending approvals"
            description="All execution requests have been resolved. New requests will appear here in real-time."
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
                      <Check className="h-3.5 w-3.5" /> Allow Once
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setApproveTarget({ approval, always: true })}
                      className="gap-1"
                    >
                      <Check className="h-3.5 w-3.5" /> Allow Always
                    </Button>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => setDenyTarget(approval)}
                      className="gap-1"
                    >
                      <X className="h-3.5 w-3.5" /> Deny
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
        title={approveTarget?.always ? "Allow Always" : "Allow Once"}
        description={
          approveTarget?.always
            ? `Permanently allow "${approveTarget.approval.command}" for agent "${approveTarget.approval.agentId}"? This command will be auto-approved in the future.`
            : `Allow execution of "${approveTarget?.approval.command}" from agent "${approveTarget?.approval.agentId}"?`
        }
        confirmLabel={approveTarget?.always ? "Allow Always" : "Allow Once"}
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
        title="Deny Execution"
        description={`Deny execution request "${denyTarget?.command}" from agent "${denyTarget?.agentId}"?`}
        confirmLabel="Deny"
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
