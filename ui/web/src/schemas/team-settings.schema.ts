import { z } from "zod";

export const teamSettingsSchema = z.object({
  // Notifications
  notifyDispatched: z.boolean(),
  notifyProgress: z.boolean(),
  notifyFailed: z.boolean(),
  notifyCompleted: z.boolean(),
  notifyCommented: z.boolean(),
  notifyNewTask: z.boolean(),
  notifySlowTool: z.boolean(),
  notifyMode: z.enum(["direct", "leader"]),
  // Orchestration
  workspaceScope: z.string(),
  memberRequestsEnabled: z.boolean(),
  memberRequestsAutoDispatch: z.boolean(),
  blockerEscalationEnabled: z.boolean(),
  followupInterval: z.number().min(1).max(1440),
  followupMaxReminders: z.number().min(0).max(100),
  // Access control
  allowUserIds: z.array(z.string()),
  denyUserIds: z.array(z.string()),
  allowChannels: z.array(z.string()),
  denyChannels: z.array(z.string()),
  // Routing rules
  routingEnabled: z.boolean(),
  routingFallbackToSelf: z.boolean(),
  routingConfidenceThreshold: z.number().min(0.01).max(1),
  routingRules: z.array(z.object({
    agent_key: z.string().min(1),
    keywords: z.array(z.string()),
    file_patterns: z.array(z.string()),
    criteria: z.string(),
    enabled: z.boolean(),
  })),
});

export type TeamSettingsFormData = z.infer<typeof teamSettingsSchema>;
