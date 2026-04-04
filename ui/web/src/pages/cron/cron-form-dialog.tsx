import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { CronSchedule } from "./hooks/use-cron";
import { slugify } from "@/lib/slug";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { cronCreateSchema, type CronCreateFormData } from "@/schemas/cron.schema";

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

export function CronFormDialog({ open, onOpenChange, onSubmit }: CronFormDialogProps) {
  const { t } = useTranslation("cron");
  const { agents } = useAgents();

  const { register, control, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<CronCreateFormData>({
    resolver: zodResolver(cronCreateSchema),
    mode: "onChange",
    defaultValues: {
      name: "",
      message: "",
      agentId: "",
      scheduleKind: "every",
      everyValue: "60",
      cronExpr: "0 * * * *",
    },
  });

  const scheduleKind = watch("scheduleKind");

  const onFormSubmit = async (data: CronCreateFormData) => {
    let schedule: CronSchedule;
    if (data.scheduleKind === "every") {
      schedule = { kind: "every", everyMs: Number(data.everyValue) * 1000 };
    } else if (data.scheduleKind === "cron") {
      schedule = { kind: "cron", expr: data.cronExpr };
    } else {
      schedule = { kind: "at", atMs: Date.now() + 60000 };
    }

    await onSubmit({
      name: data.name,
      schedule,
      message: data.message,
      agentId: data.agentId || undefined,
    });
    onOpenChange(false);
    reset();
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
            <Input
              {...register("name")}
              onChange={(e) => setValue("name", slugify(e.target.value), { shouldValidate: true })}
              placeholder={t("create.namePlaceholder")}
            />
            {errors.name ? (
              <p className="text-xs text-destructive">{errors.name.message}</p>
            ) : (
              <p className="text-xs text-muted-foreground">{t("create.nameHint")}</p>
            )}
          </div>

          <div className="space-y-2">
            <Label>{t("create.agentId")}</Label>
            <Controller
              control={control}
              name="agentId"
              render={({ field }) => (
                <Select
                  value={field.value || "__default__"}
                  onValueChange={(v) => field.onChange(v === "__default__" ? "" : v)}
                >
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue placeholder={t("create.agentIdPlaceholder")} />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default__">{t("create.agentIdPlaceholder")}</SelectItem>
                    {agents.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.display_name || a.agent_key || a.id}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            />
          </div>

          <div className="space-y-2">
            <Label>{t("create.scheduleType")}</Label>
            <div className="flex gap-2">
              {(["every", "cron", "at"] as const).map((kind) => (
                <Button
                  key={kind}
                  variant={scheduleKind === kind ? "default" : "outline"}
                  size="sm"
                  onClick={() => setValue("scheduleKind", kind)}
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
                {...register("everyValue")}
                placeholder="60"
              />
            </div>
          )}

          {scheduleKind === "cron" && (
            <div className="space-y-2">
              <Label>{t("create.cronExpression")}</Label>
              <Input
                {...register("cronExpr")}
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
              {...register("message")}
              placeholder={t("create.messagePlaceholder")}
              rows={3}
            />
            {errors.message && (
              <p className="text-xs text-destructive">{errors.message.message}</p>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t("create.cancel")}
          </Button>
          <Button
            onClick={handleSubmit(onFormSubmit)}
            disabled={isSubmitting || !!errors.name || !!errors.message}
          >
            {isSubmitting ? t("create.creating") : t("create.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
