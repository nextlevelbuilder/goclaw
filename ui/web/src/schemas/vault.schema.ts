import { z } from "zod";

export const vaultLinkSchema = z.object({
  toDocId: z.string().min(1, "Required"),
  linkType: z.string().min(1, "Required"),
  context: z.string().max(500).optional(),
});

export type VaultLinkFormData = z.infer<typeof vaultLinkSchema>;
