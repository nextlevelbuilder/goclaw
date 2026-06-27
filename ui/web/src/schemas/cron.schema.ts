import { z } from "zod";
import { isValidSlug } from "@/lib/slug";

export const cronCreateSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .refine(isValidSlug, "Only lowercase letters, numbers, and hyphens"),
  payloadKind: z.enum(["agent_turn", "command"]),
  message: z.string().optional(),
  commandArgvText: z.string().optional(),
  commandCwd: z.string().optional(),
  commandTimeoutSeconds: z.string().optional(),
  commandNoOutputTimeoutSeconds: z.string().optional(),
  commandOutputMaxBytes: z.string().optional(),
  commandInput: z.string().optional(),
  agentId: z.string().optional(),
  scheduleKind: z.enum(["every", "cron", "at"]),
  everyValue: z.string().min(1, "Required"),
  cronExpr: z.string().min(1, "Required"),
}).superRefine((data, ctx) => {
  if (data.payloadKind === "agent_turn" && !data.message?.trim()) {
    ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["message"], message: "Required" });
  }
  if (data.payloadKind === "command") {
    try {
      const argv = (data.commandArgvText || "").split("\n").map((v) => v.trim()).filter(Boolean);
      if (argv.length === 0 || !argv[0]) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["commandArgvText"], message: "argv must be a non-empty list" });
      }
      for (const field of ["commandTimeoutSeconds", "commandNoOutputTimeoutSeconds", "commandOutputMaxBytes"] as const) {
        const raw = data[field];
        if (raw && (!/^\d+$/.test(raw) || Number(raw) < 0)) {
          ctx.addIssue({ code: z.ZodIssueCode.custom, path: [field], message: "Must be a non-negative integer" });
        }
      }
    } catch {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["commandArgvText"], message: "Invalid command" });
    }
  }
});

export type CronCreateFormData = z.infer<typeof cronCreateSchema>;
