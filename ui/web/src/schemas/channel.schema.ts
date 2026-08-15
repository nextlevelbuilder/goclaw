import { z } from "zod";
import { isValidSlug } from "@/lib/slug";

export const channelInstanceSchema = z.object({
  name: z
    .string()
    .min(1, "Required")
    .refine(isValidSlug, "Only lowercase letters, numbers, and hyphens"),
  displayName: z.string().optional(),
  channelType: z.string().min(1, "Required"),
  agentId: z.string().min(1, "Required"),
  enabled: z.boolean(),
});

export type ChannelInstanceFormData = z.infer<typeof channelInstanceSchema>;
