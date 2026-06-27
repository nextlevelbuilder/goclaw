import { z } from "zod";
import { isValidSlug } from "@/lib/slug";

export const cronCreateSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .refine(isValidSlug, "Only lowercase letters, numbers, and hyphens"),
  payloadKind: z.enum(["agent_turn", "command"]),
  message: z.string().optional(),
  commandJson: z.string().optional(),
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
      const parsed = JSON.parse(data.commandJson || "");
      if (!Array.isArray(parsed?.argv) || parsed.argv.length === 0 || !parsed.argv[0]) {
        ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["commandJson"], message: "argv must be a non-empty array" });
      }
    } catch {
      ctx.addIssue({ code: z.ZodIssueCode.custom, path: ["commandJson"], message: "Invalid JSON" });
    }
  }
});

export type CronCreateFormData = z.infer<typeof cronCreateSchema>;
