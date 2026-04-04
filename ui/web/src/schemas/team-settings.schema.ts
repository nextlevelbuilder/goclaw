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
});

export type TeamSettingsFormData = z.infer<typeof teamSettingsSchema>;
