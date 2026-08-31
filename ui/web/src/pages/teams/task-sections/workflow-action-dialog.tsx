import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogDescription, DialogFooter,
  DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import type { WorkflowAction } from "@/types/team";

const MAX_REASON_LENGTH = 10_000;

interface WorkflowActionDialogProps {
  action: WorkflowAction | null;
  loading: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: (reason: string) => void;
}

export function WorkflowActionDialog({
  action, loading, onOpenChange, onConfirm,
}: WorkflowActionDialogProps) {
  const { t } = useTranslation("teams");
  const [reason, setReason] = useState("");

  useEffect(() => { setReason(""); }, [action]);

  if (!action) return null;
  const trimmed = reason.trim();
  const destructive = action === "cancel_workflow" || action === "fail_workflow";

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(`workflow.actions.${action}.label`)}</DialogTitle>
          <DialogDescription>{t(`workflow.actions.${action}.description`)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-2">
          <label className="text-sm font-medium" htmlFor="workflow-action-reason">
            {t("workflow.reason.label")}
          </label>
          <Textarea
            id="workflow-action-reason"
            value={reason}
            maxLength={MAX_REASON_LENGTH}
            placeholder={t("workflow.reason.placeholder")}
            disabled={loading}
            onChange={(event) => setReason(event.target.value)}
          />
          <div className="flex justify-between text-xs text-muted-foreground">
            <span>{!trimmed ? t("workflow.reason.required") : ""}</span>
            <span>{reason.length}/{MAX_REASON_LENGTH}</span>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={loading} onClick={() => onOpenChange(false)}>
            {t("workflow.actions.cancel")}
          </Button>
          <Button
            variant={destructive ? "destructive" : "default"}
            disabled={loading || !trimmed}
            onClick={() => onConfirm(trimmed)}
          >
            {loading ? t("workflow.actions.applying") : t("workflow.actions.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
