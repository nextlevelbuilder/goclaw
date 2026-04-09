import { z } from "zod";
import { isValidSlug } from "@/lib/slug";

export const agentCreateSchema = z.object({
  emoji: z.string().max(2).optional(),
  displayName: z.string().min(1, "Required"),
  agentKey: z
    .string()
    .min(1, "Required")
    .refine(isValidSlug, "Only lowercase letters, numbers, and hyphens"),
  provider: z.string().min(1, "Required"),
  model: z.string().min(1, "Required"),
  agentType: z.enum(["predefined"]),
  description: z.string().optional(),
  selfEvolve: z.boolean(),
  promptMode: z.enum(["full", "task", "minimal", "none"]).optional(),
});

export type AgentCreateFormData = z.infer<typeof agentCreateSchema>;
