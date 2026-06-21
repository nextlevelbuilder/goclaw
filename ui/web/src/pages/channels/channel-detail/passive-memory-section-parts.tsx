import { Check, Trash2, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { formatDate } from "@/lib/format";
import type { ChannelMemoryExtractionItem } from "@/types/channel";

export function ToggleRow({
  label,
  checked,
  onCheckedChange,
}: {
  label: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2 text-sm">
      <span>{label}</span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </label>
  );
}

export function NumberField({
  label,
  value,
  onChange,
}: {
  label: string;
  value: number;
  onChange: (value: number) => void;
}) {
  return (
    <label className="space-y-1 text-xs font-medium text-muted-foreground">
      <span>{label}</span>
      <Input type="number" min={0} value={value} onChange={(e) => onChange(Number(e.target.value))} />
    </label>
  );
}

export function TextareaBlock({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="space-y-1 text-xs font-medium text-muted-foreground">
      <span>{label}</span>
      <Textarea size="sm" value={value} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}

export function RunSummary({
  loading,
  status,
  t,
}: {
  loading: boolean;
  status?: {
    status: string;
    item_count: number;
    message_count: number;
    redaction_count: number;
    completed_at?: string;
    error_message?: string;
  };
  t: (key: string, opts?: Record<string, unknown>) => string;
}) {
  if (loading) {
    return <div className="rounded-lg border p-3 text-sm text-muted-foreground">{t("detail.passiveMemory.loading")}</div>;
  }
  if (!status) {
    return <div className="rounded-lg border p-3 text-sm text-muted-foreground">{t("detail.passiveMemory.noRuns")}</div>;
  }
  return (
    <div className="rounded-lg border p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <span className="font-medium">{t("detail.passiveMemory.lastRun")}</span>
        <Badge variant={status.status === "failed" ? "destructive" : "outline"}>{status.status}</Badge>
      </div>
      <div className="mt-2 text-muted-foreground">
        {t("detail.passiveMemory.runStats", {
          messages: status.message_count,
          items: status.item_count,
          redactions: status.redaction_count,
        })}
      </div>
      {status.completed_at && <div className="mt-1 text-xs text-muted-foreground">{formatDate(status.completed_at)}</div>}
      {status.error_message && <div className="mt-2 text-xs text-destructive">{status.error_message}</div>}
    </div>
  );
}

export function MemoryItemRow({
  item,
  pending,
  onAction,
}: {
  item: ChannelMemoryExtractionItem;
  pending: boolean;
  onAction: (action: "approve" | "reject" | "delete") => void;
}) {
  const { t } = useTranslation("channels");
  return (
    <div className="rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant="info">{t(`detail.passiveMemory.type.${item.item_type}`)}</Badge>
        <Badge variant={item.status === "pending_review" ? "warning" : item.status === "written" ? "success" : "outline"}>
          {item.status}
        </Badge>
        <span className="text-xs text-muted-foreground">{Math.round(item.confidence * 100)}%</span>
      </div>
      <p className="mt-2 text-sm">{item.summary}</p>
      <div className="mt-3 flex flex-wrap gap-2">
        {item.status === "pending_review" && (
          <>
            <Button size="xs" variant="outline" disabled={pending} onClick={() => onAction("approve")}>
              <Check />
              {t("detail.passiveMemory.approve")}
            </Button>
            <Button size="xs" variant="outline" disabled={pending} onClick={() => onAction("reject")}>
              <X />
              {t("detail.passiveMemory.reject")}
            </Button>
          </>
        )}
        <Button size="xs" variant="ghost" disabled={pending} onClick={() => onAction("delete")}>
          <Trash2 />
          {t("detail.passiveMemory.delete")}
        </Button>
      </div>
    </div>
  );
}
