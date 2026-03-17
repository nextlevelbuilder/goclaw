import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import type { CronSchedule } from "./hooks/use-cron";
import { slugify, isValidSlug } from "@/lib/slug";

interface CronFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: {
    name: string;
    schedule: CronSchedule;
    message: string;
    agentId?: string;
  }) => Promise<void>;
}

type ScheduleKind = "every" | "cron" | "at";

export function CronFormDialog({ open, onOpenChange, onSubmit }: CronFormDialogProps) {
  const { t } = useTranslation("cron");
  const [name, setName] = useState("");
  const [message, setMessage] = useState("");
  const [agentId, setAgentId] = useState("");
  const [scheduleKind, setScheduleKind] = useState<ScheduleKind>("every");
  const [everyValue, setEveryValue] = useState("60");
  const [cronExpr, setCronExpr] = useState("0 * * * *");
  const [saving, setSaving] = useState(false);

  const handleSubmit = async () => {
    if (!name.trim() || !message.trim()) return;

    let schedule: CronSchedule;
    if (scheduleKind === "every") {
      schedule = { kind: "every", everyMs: Number(everyValue) * 1000 };
    } else if (scheduleKind === "cron") {
      schedule = { kind: "cron", expr: cronExpr };
    } else {
      schedule = { kind: "at", atMs: Date.now() + 60000 };
    }

    setSaving(true);
    try {
      await onSubmit({
        name: name.trim(),
        schedule,
        message: message.trim(),
        agentId: agentId.trim() || undefined,
      });
      onOpenChange(false);
      setName("");
      setMessage("");
      setAgentId("");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle>{t("create.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 -mx-4 px-4 sm:-mx-6 sm:px-6 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label>{t("create.name")}</Label>
            <Input value={name} onChange={(e) => setName(slugify(e.target.value))} placeholder={t("create.namePlaceholder")} />
            <p className="text-xs text-muted-foreground">{t("create.nameHint")}</p>
          </div>

          <div className="space-y-2">
            <Label>{t("create.agentId")}</Label>
            <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} placeholder={t("create.agentIdPlaceholder")} />
          </div>

          <div className="space-y-2">
            <Label>{t("create.scheduleType")}</Label>
            <div className="flex gap-2">
              {(["every", "cron", "at"] as const).map((kind) => (
                <Button
                  key={kind}
                  variant={scheduleKind === kind ? "default" : "outline"}
                  size="sm"
                  onClick={() => setScheduleKind(kind)}
                >
                  {kind === "every" ? t("create.every") : kind === "cron" ? t("create.cron") : t("create.once")}
                </Button>
              ))}
            </div>
          </div>

          {scheduleKind === "every" && (
            <div className="space-y-2">
              <Label>{t("create.intervalSeconds")}</Label>
              <Input
                type="number"
                min={1}
                value={everyValue}
                onChange={(e) => setEveryValue(e.target.value)}
                placeholder="60"
              />
            </div>
          )}

          {scheduleKind === "cron" && (
            <div className="space-y-2">
              <Label>{t("create.cronExpression")}</Label>
              <Input
                value={cronExpr}
                onChange={(e) => setCronExpr(e.target.value)}
                placeholder="0 * * * *"
              />
              <p className="text-xs text-muted-foreground">{t("create.cronHint")}</p>
            </div>
          )}

          {scheduleKind === "at" && (
            <p className="text-sm text-muted-foreground">
              {t("create.onceDesc")}
            </p>
          )}

          <div className="space-y-2">
            <Label>{t("create.message")}</Label>
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder={t("create.messagePlaceholder")}
              rows={3}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("create.cancel")}
          </Button>
          <Button onClick={handleSubmit} disabled={saving || !name.trim() || !isValidSlug(name.trim()) || !message.trim()}>
            {saving ? t("create.creating") : t("create.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
