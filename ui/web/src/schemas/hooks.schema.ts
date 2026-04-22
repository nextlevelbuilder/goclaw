import { z } from "zod";

export const HookEventEnum = z.enum([
  "session_start",
  "user_prompt_submit",
  "pre_tool_use",
  "post_tool_use",
  "stop",
  "subagent_start",
  "subagent_stop",
]);

// `command` deliberately absent: Wave 1 removes UI surface for it. Legacy Standard rows
// with handler_type="command" are auto-disabled at startup (Phase 07). Lite keeps running
// existing command hooks via dispatcher but cannot create new ones through the UI.
// `script` added in Phase 06 (goja ES5.1 runtime; builtin PII redactor ships in Phase 05).
export const HookHandlerTypeEnum = z.enum(["script", "http", "prompt"]);

export const HookScopeEnum = z.enum(["global", "tenant", "agent"]);

export const hookFormSchema = z
  .object({
    name: z.string().max(255).optional(),
    agent_ids: z.array(z.string()).optional(),
    event: HookEventEnum,
    handler_type: HookHandlerTypeEnum,
    scope: HookScopeEnum,
    matcher: z.string().optional(),
    if_expr: z.string().optional(),
    timeout_ms: z.number().int().min(100).max(300_000),
    on_timeout: z.enum(["block", "allow"]),
    priority: z.number().int().min(0).max(1000),
    enabled: z.boolean(),
    // handler-specific config fields
    url: z.string().url().optional().or(z.literal("")),
    method: z.enum(["GET", "POST", "PUT", "PATCH", "DELETE"]).optional(),
    headers: z.string().optional(), // JSON string
    body_template: z.string().optional(),
    prompt_template: z.string().optional(),
    model: z.string().optional(),
    max_invocations_per_turn: z.number().int().min(1).max(20).optional(),
    // Script handler source (ES5.1 JavaScript). Cap mirrors backend 32 KiB
    // enforcement in the goja handler.
    script_source: z.string().max(32_768).optional(),
  })
  .superRefine((data, ctx) => {
    // Validate regex if matcher is provided
    if (data.matcher) {
      try {
        new RegExp(data.matcher);
      } catch {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["matcher"],
          message: "validation.invalidRegex",
        });
      }
    }
    // Prompt handler requires matcher OR if_expr
    if (data.handler_type === "prompt") {
      if (!data.matcher && !data.if_expr) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["matcher"],
          message: "validation.promptRequiresMatcher",
        });
      }
      if (!data.prompt_template) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          path: ["prompt_template"],
          message: "validation.promptTemplateRequired",
        });
      }
    }
    if (data.handler_type === "script" && !data.script_source?.trim()) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ["script_source"],
        message: "validation.scriptSourceRequired",
      });
    }
  });

export type HookFormData = z.infer<typeof hookFormSchema>;
export type HookEvent = z.infer<typeof HookEventEnum>;
export type HookHandlerType = z.infer<typeof HookHandlerTypeEnum>;
export type HookScope = z.infer<typeof HookScopeEnum>;
