import { useEffect } from "react";
import { useForm, Controller } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useTranslation } from "react-i18next";
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { hookFormSchema, type HookFormData } from "@/schemas/hooks.schema";
import type { HookConfig } from "@/hooks/use-hooks";
import { useAuthStore } from "@/stores/use-auth-store";
import { useAgents } from "@/pages/agents/hooks/use-agents";
import { ScriptEditor } from "./script-editor";

const HOOK_EVENTS = [
  "session_start", "user_prompt_submit", "pre_tool_use",
  "post_tool_use", "stop", "subagent_start", "subagent_stop",
] as const;

interface HookFormDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: HookFormData) => Promise<void>;
  initial?: HookConfig | null;
}

export function HookFormDialog({ open, onOpenChange, onSubmit, initial }: HookFormDialogProps) {
  const { t } = useTranslation("hooks");
  const isMasterScope = useAuthStore((s) => s.isMasterScope);
  // Global scope hidden for non-master callers; existing `global` hooks still render as-is in edit mode.
  const scopeOptions = isMasterScope
    ? (["global", "tenant", "agent"] as const)
    : (["tenant", "agent"] as const);

  const {
    register, control, handleSubmit, watch, reset,
    formState: { errors, isSubmitting },
  } = useForm<HookFormData>({
    resolver: zodResolver(hookFormSchema),
    defaultValues: {
      name: "",
      agent_ids: [],
      event: "pre_tool_use",
      handler_type: "script",
      scope: "tenant",
      timeout_ms: 5000,
      on_timeout: "block",
      priority: 100,
      enabled: true,
      method: "POST",
      max_invocations_per_turn: 5,
    },
  });

  const handlerType = watch("handler_type");
  const scope = watch("scope");
  const { agents } = useAgents();
  // Builtin rows (Phase 04/05) ship with source='builtin'. UI + backend agree:
  // only `enabled` is mutable. All other inputs render as read-only, and the
  // Save button label changes to reflect the narrowed scope.
  const isBuiltin = initial?.source === "builtin";

  useEffect(() => {
    if (open) {
      if (initial) {
        // Defensive: backend may emit config as null when a row was inserted
        // without a config payload. Treat null/undefined as empty so the
        // property reads below don't crash the edit dialog.
        const cfg = (initial.config ?? {}) as Record<string, unknown>;
        // Legacy `command` rows coerce to `http` in the form — user cannot save as
        // `command` (enum narrowed post-Wave-1). Phase 07 auto-disables them regardless.
        const handlerType: HookFormData["handler_type"] =
          initial.handler_type === "command" ? "http" : initial.handler_type;
        reset({
          name: initial.name ?? "",
          agent_ids: initial.agent_ids ?? [],
          event: initial.event as HookFormData["event"],
          handler_type: handlerType,
          scope: initial.scope,
          matcher: initial.matcher ?? "",
          if_expr: initial.if_expr ?? "",
          timeout_ms: initial.timeout_ms,
          on_timeout: initial.on_timeout,
          priority: initial.priority,
          enabled: initial.enabled,
          url: (cfg.url as string) ?? "",
          method: (cfg.method as HookFormData["method"]) ?? "POST",
          headers: cfg.headers ? JSON.stringify(cfg.headers) : "",
          body_template: (cfg.body_template as string) ?? "",
          prompt_template: (cfg.prompt_template as string) ?? "",
          model: (cfg.model as string) ?? "",
          max_invocations_per_turn: (cfg.max_invocations_per_turn as number) ?? 5,
          script_source: (cfg.source as string) ?? "",
        });
      } else {
        reset();
      }
    }
  }, [open, initial, reset]);

  const onFormSubmit = async (data: HookFormData) => {
    await onSubmit(data);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] flex flex-col max-sm:inset-0 max-sm:rounded-none sm:max-w-3xl lg:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{initial ? t("form.title_edit") : t("form.title_create")}</DialogTitle>
        </DialogHeader>

        <div className="flex-1 space-y-4 overflow-y-auto -mx-4 px-4 sm:-mx-6 sm:px-6">
          {isBuiltin && (
            <div className="rounded-lg border border-blue-200 bg-blue-50 px-3 py-2 text-xs text-blue-800 dark:border-blue-900/50 dark:bg-blue-900/20 dark:text-blue-300">
              {t("form.builtinReadonly")}
            </div>
          )}

          {/* Name — optional user-facing label */}
          <div className="space-y-1.5">
            <Label>{t("form.name")}</Label>
            <Input
              {...register("name")}
              placeholder={t("form.namePlaceholder")}
              className="text-base md:text-sm"
              disabled={isBuiltin}
            />
          </div>

          {/* Event + Scope side-by-side on wider viewports */}
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>{t("form.event")}</Label>
              <Controller control={control} name="event" render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange} disabled={isBuiltin}>
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {HOOK_EVENTS.map((e) => (
                      <SelectItem key={e} value={e}>{e}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )} />
            </div>
            {/* Scope — global hidden for non-master callers (advisory UI hint; backend enforces). */}
            <div className="space-y-1.5">
              <Label>{t("form.scope")}</Label>
              <Controller control={control} name="scope" render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange} disabled={isBuiltin}>
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {scopeOptions.map((s) => (
                      <SelectItem key={s} value={s}>{s}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )} />
            </div>
          </div>

          {/* Agent picker — visible only when scope=agent */}
          {scope === "agent" && agents.length > 0 && (
            <Controller control={control} name="agent_ids" render={({ field }) => (
              <div className="space-y-1.5">
                <Label>{t("form.agents")}</Label>
                <div className="grid gap-1.5 max-h-40 overflow-y-auto rounded border p-2 sm:grid-cols-2">
                  {agents.map((a) => {
                    const checked = (field.value ?? []).includes(a.id);
                    return (
                      <label key={a.id} className="flex items-center gap-2 text-sm cursor-pointer rounded px-1.5 py-1 hover:bg-muted">
                        <input
                          type="checkbox"
                          checked={checked}
                          disabled={isBuiltin}
                          className="rounded border-input"
                          onChange={(e) => {
                            const ids = field.value ?? [];
                            field.onChange(e.target.checked ? [...ids, a.id] : ids.filter((x: string) => x !== a.id));
                          }}
                        />
                        <span className="truncate">{a.display_name || a.agent_key}</span>
                      </label>
                    );
                  })}
                </div>
                <p className="text-xs text-muted-foreground">{t("form.agentsHint")}</p>
              </div>
            )} />
          )}

          {/* Handler type — `command` intentionally absent post-Wave-1. Lite users keep
              existing rows but cannot create new ones via UI (DB/CLI path only).
              `script` added in Phase 06 (goja sandbox; builtin PII redactor = Phase 05). */}
          <div className="space-y-1.5">
            <Label>{t("form.handlerType")}</Label>
            <Controller control={control} name="handler_type" render={({ field }) => (
              <RadioGroup
                value={field.value}
                onValueChange={field.onChange}
                className="flex flex-wrap gap-4"
                disabled={isBuiltin}
              >
                {(["script", "http", "prompt"] as const).map((ht) => (
                  <div key={ht} className="flex items-center gap-1.5">
                    <RadioGroupItem value={ht} id={`ht-${ht}`} />
                    <Label htmlFor={`ht-${ht}`} className="cursor-pointer">
                      {ht}
                    </Label>
                  </div>
                ))}
              </RadioGroup>
            )} />
          </div>

          {/* Matcher + if_expr side-by-side */}
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-1.5">
              <Label>{t("form.matcher")}</Label>
              <Input
                {...register("matcher")}
                placeholder="^bash$"
                className="text-base md:text-sm font-mono"
                disabled={isBuiltin}
              />
              {errors.matcher
                ? <p className="text-xs text-destructive">{errors.matcher.message}</p>
                : <p className="text-xs text-muted-foreground">{t("form.matcherHint")}</p>
              }
            </div>
            <div className="space-y-1.5">
              <Label>{t("form.ifExpr")}</Label>
              <Input
                {...register("if_expr")}
                placeholder='tool_input.path.startsWith("/etc")'
                className="text-base md:text-sm font-mono"
                disabled={isBuiltin}
              />
              <p className="text-xs text-muted-foreground">{t("form.ifExprHint")}</p>
            </div>
          </div>

          {/* Handler-specific sub-forms */}
          {handlerType === "script" && (
            <div className="rounded-lg border p-3">
            <div className="space-y-3 max-h-[50vh] overflow-y-auto">
              <Controller
                control={control}
                name="script_source"
                render={({ field }) => (
                  <ScriptEditor
                    value={field.value ?? ""}
                    onChange={field.onChange}
                    error={errors.script_source?.message as string | undefined}
                    readOnly={isBuiltin}
                  />
                )}
              />
            </div>
            </div>
          )}

          {handlerType === "http" && (
            <div className="space-y-3 rounded-lg border p-3">
              <div className="space-y-1.5">
                <Label>{t("form.url")}</Label>
                <Input
                  {...register("url")}
                  placeholder="https://hooks.example.com/agent"
                  className="text-base md:text-sm"
                  disabled={isBuiltin}
                />
                {errors.url && <p className="text-xs text-destructive">{errors.url.message}</p>}
              </div>
              <div className="space-y-1.5">
                <Label>{t("form.method")}</Label>
                <Controller control={control} name="method" render={({ field }) => (
                  <Select value={field.value ?? "POST"} onValueChange={field.onChange} disabled={isBuiltin}>
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {["GET", "POST", "PUT", "PATCH", "DELETE"].map((m) => (
                        <SelectItem key={m} value={m}>{m}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )} />
              </div>
              <div className="space-y-1.5">
                <Label>{t("form.bodyTemplate")}</Label>
                <Textarea
                  {...register("body_template")}
                  rows={3}
                  placeholder='{"event": "{{.Event}}"}'
                  className="text-base md:text-sm font-mono"
                  disabled={isBuiltin}
                />
              </div>
            </div>
          )}

          {handlerType === "prompt" && (
            <div className="space-y-3 rounded-lg border p-3">
              <div className="space-y-1.5">
                <Label>{t("form.promptTemplate")}</Label>
                <Textarea
                  {...register("prompt_template")}
                  rows={4}
                  placeholder="Evaluate the tool call and decide whether to allow or block it."
                  className="text-base md:text-sm"
                  disabled={isBuiltin}
                />
                {errors.prompt_template && (
                  <p className="text-xs text-destructive">{errors.prompt_template.message}</p>
                )}
              </div>
              <div className="space-y-1.5">
                <Label>{t("form.model")}</Label>
                <Controller control={control} name="model" render={({ field }) => (
                  <Select value={field.value ?? "haiku"} onValueChange={field.onChange} disabled={isBuiltin}>
                    <SelectTrigger className="text-base md:text-sm">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="haiku">haiku</SelectItem>
                      <SelectItem value="sonnet">sonnet</SelectItem>
                      <SelectItem value="opus">opus</SelectItem>
                    </SelectContent>
                  </Select>
                )} />
              </div>
              <div className="space-y-1.5">
                <Label>{t("form.maxInvocationsPerTurn")}</Label>
                <Input
                  type="number"
                  min={1}
                  max={20}
                  {...register("max_invocations_per_turn", { valueAsNumber: true })}
                  className="text-base md:text-sm w-24"
                  disabled={isBuiltin}
                />
              </div>
            </div>
          )}

          {/* Timeout / on_timeout / priority on one row when wide, stacks on mobile.
              Enabled toggle stays actionable on every row (only field user can
              change for builtin hooks). */}
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <div className="space-y-1.5">
              <Label>{t("form.timeout")}</Label>
              <Input
                type="number"
                min={100}
                {...register("timeout_ms", { valueAsNumber: true })}
                className="text-base md:text-sm"
                disabled={isBuiltin}
              />
            </div>
            <div className="space-y-1.5">
              <Label>{t("form.onTimeout")}</Label>
              <Controller control={control} name="on_timeout" render={({ field }) => (
                <Select value={field.value} onValueChange={field.onChange} disabled={isBuiltin}>
                  <SelectTrigger className="text-base md:text-sm">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="block">block</SelectItem>
                    <SelectItem value="allow">allow</SelectItem>
                  </SelectContent>
                </Select>
              )} />
            </div>
            <div className="space-y-1.5">
              <Label>{t("form.priority")}</Label>
              <Input
                type="number"
                min={0}
                max={1000}
                {...register("priority", { valueAsNumber: true })}
                className="text-base md:text-sm"
                disabled={isBuiltin}
              />
            </div>
            <div className="space-y-1.5">
              <Label>{t("form.enabled")}</Label>
              <div className="flex h-9 items-center">
                <Controller control={control} name="enabled" render={({ field }) => (
                  <Switch checked={field.value} onCheckedChange={field.onChange} />
                )} />
              </div>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={isSubmitting}>
            {t("form.cancel")}
          </Button>
          <Button onClick={handleSubmit(onFormSubmit)} disabled={isSubmitting}>
            {t(isBuiltin ? "form.saveToggle" : "form.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
