import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter,
} from "@/components/ui/dialog";
import { useTranslation } from "react-i18next";
import { toast } from "@/stores/use-toast-store";
import type { TeamTaskData, TeamMemberData, ScopeEntry } from "@/types/team";

interface CreateTaskDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  teamId: string;
  members: TeamMemberData[];
  selectedScope: ScopeEntry | null;
  createTask: (teamId: string, params: {
    subject: string; description?: string; priority?: number;
    taskType?: string; assignTo?: string; channel?: string; chatId?: string;
  }) => Promise<TeamTaskData>;
  onCreated: () => void;
}

export function CreateTaskDialog({
  open, onOpenChange, teamId, members, selectedScope, createTask, onCreated,
}: CreateTaskDialogProps) {
  const { t } = useTranslation("teams");
  const [subject, setSubject] = useState("");
  const [description, setDescription] = useState("");
  const [priority, setPriority] = useState(0);
  const [taskType, setTaskType] = useState("general");
  const [assignTo, setAssignTo] = useState("");
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    if (!subject.trim()) return;
    setCreating(true);
    try {
      await createTask(teamId, {
        subject: subject.trim(),
        description: description.trim() || undefined,
        priority,
        taskType: taskType || undefined,
        assignTo: assignTo || undefined,
        channel: selectedScope?.channel || undefined,
        chatId: selectedScope?.chat_id || undefined,
      });
      toast.success(t("toast.taskCreated"));
      setSubject(""); setDescription(""); setPriority(0);
      setTaskType("general"); setAssignTo("");
      onOpenChange(false);
      onCreated();
    } catch {
      toast.error(t("toast.failedCreateTask"));
    } finally {
      setCreating(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[95vw] sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("tasks.createTaskTitle")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-4 py-2">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("tasks.columns.subject")} *</label>
            <input type="text" value={subject} onChange={(e) => setSubject(e.target.value)}
              placeholder={t("tasks.subjectPlaceholder")}
              className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm" autoFocus />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("tasks.detail.description")}</label>
            <textarea value={description} onChange={(e) => setDescription(e.target.value)}
              placeholder={t("tasks.descriptionPlaceholder")} rows={3}
              className="w-full resize-none rounded-md border bg-background px-3 py-2 text-base md:text-sm" />
          </div>
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <label className="text-sm font-medium">{t("tasks.detail.type").replace(":", "")}</label>
              <select value={taskType} onChange={(e) => setTaskType(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm">
                <option value="general">General</option>
                <option value="delegation">Delegation</option>
                <option value="escalation">Escalation</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <label className="text-sm font-medium">{t("tasks.priorityLabel")}</label>
              <input type="number" value={priority} onChange={(e) => setPriority(Number(e.target.value))}
                min={0} className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm" />
            </div>
          </div>
          {members.length > 0 && (
            <div className="space-y-1.5">
              <label className="text-sm font-medium">{t("tasks.assignTo")}</label>
              <select value={assignTo} onChange={(e) => setAssignTo(e.target.value)}
                className="w-full rounded-md border bg-background px-3 py-2 text-base md:text-sm">
                <option value="">{t("tasks.unassigned")}</option>
                {members.map((m) => (
                  <option key={m.agent_id} value={m.agent_id}>
                    {m.display_name || m.agent_key || m.agent_id}
                    {m.role === "lead" ? " (Lead)" : ""}
                  </option>
                ))}
              </select>
            </div>
          )}
        </div>
        <DialogFooter className="gap-2">
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} disabled={creating}>
            {t("create.cancel")}
          </Button>
          <Button size="sm" onClick={handleCreate} disabled={creating || !subject.trim()}>
            {creating ? t("create.creating") : t("tasks.createTask")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
