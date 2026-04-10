import { z } from "zod";

export const tokenFormSchema = z.object({
  userId: z.string().min(1, "Required"),
  token: z.string().min(1, "Required"),
});

export const pairingFormSchema = z.object({
  userId: z.string().min(1, "Required"),
});

export type TokenFormData = z.infer<typeof tokenFormSchema>;
export type PairingFormData = z.infer<typeof pairingFormSchema>;
