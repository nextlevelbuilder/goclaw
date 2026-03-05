import { useState } from "react";
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
          <DialogTitle>Create Cron Job</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label>Name</Label>
            <Input value={name} onChange={(e) => setName(slugify(e.target.value))} placeholder="my-daily-task" />
            <p className="text-xs text-muted-foreground">Lowercase letters, numbers, and hyphens only</p>
          </div>

          <div className="space-y-2">
            <Label>Agent ID (optional)</Label>
            <Input value={agentId} onChange={(e) => setAgentId(e.target.value)} placeholder="default" />
          </div>

          <div className="space-y-2">
            <Label>Schedule Type</Label>
            <div className="flex gap-2">
              {(["every", "cron", "at"] as const).map((kind) => (
                <Button
                  key={kind}
                  variant={scheduleKind === kind ? "default" : "outline"}
                  size="sm"
                  onClick={() => setScheduleKind(kind)}
                >
                  {kind === "every" ? "Every" : kind === "cron" ? "Cron" : "Once"}
                </Button>
              ))}
            </div>
          </div>

          {scheduleKind === "every" && (
            <div className="space-y-2">
              <Label>Interval (seconds)</Label>
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
              <Label>Cron Expression</Label>
              <Input
                value={cronExpr}
                onChange={(e) => setCronExpr(e.target.value)}
                placeholder="0 * * * *"
              />
              <p className="text-xs text-muted-foreground">Standard 5-field cron: min hour day month weekday</p>
            </div>
          )}

          {scheduleKind === "at" && (
            <p className="text-sm text-muted-foreground">
              The job will run once, approximately 1 minute from now.
            </p>
          )}

          <div className="space-y-2">
            <Label>Message</Label>
            <Textarea
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder="What should the agent do?"
              rows={3}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={saving || !name.trim() || !isValidSlug(name.trim()) || !message.trim()}>
            {saving ? "Creating..." : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
