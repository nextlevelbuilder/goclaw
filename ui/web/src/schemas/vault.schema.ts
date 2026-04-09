import { z } from "zod";

export const vaultCreateSchema = z.object({
  title: z.string().min(1, "Required"),
  path: z.string().min(1, "Required"),
  docType: z.string().min(1),
  scope: z.string().min(1),
});

export const vaultLinkSchema = z.object({
  toDocId: z.string().min(1, "Required"),
  linkType: z.string().min(1, "Required"),
  context: z.string().max(500).optional(),
});

export type VaultCreateFormData = z.infer<typeof vaultCreateSchema>;
export type VaultLinkFormData = z.infer<typeof vaultLinkSchema>;
