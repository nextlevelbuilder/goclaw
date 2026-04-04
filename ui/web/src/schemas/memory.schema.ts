import { z } from "zod";

export const memoryCreateSchema = z.object({
  selectedAgentId: z.string(),
  path: z.string().min(1),
  content: z.string().min(1),
  scopeMode: z.enum(["global", "existing", "custom"]),
  selectedUserId: z.string(),
  customUserId: z.string(),
  autoIndex: z.boolean(),
});

export type MemoryCreateFormData = z.infer<typeof memoryCreateSchema>;
