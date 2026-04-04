import { z } from "zod";
import { isValidSlug } from "@/lib/slug";

export const cronCreateSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .refine(isValidSlug, "Only lowercase letters, numbers, and hyphens"),
  message: z.string().min(1, "Required"),
  agentId: z.string().optional(),
  scheduleKind: z.enum(["every", "cron", "at"]),
  everyValue: z.string().min(1, "Required"),
  cronExpr: z.string().min(1, "Required"),
});

export type CronCreateFormData = z.infer<typeof cronCreateSchema>;
