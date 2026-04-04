import { z } from "zod";

export const cliCredentialSchema = z.object({
  binaryName: z.string().min(1, "Required"),
  binaryPath: z.string().optional(),
  description: z.string().optional(),
  denyArgs: z.string().optional(),
  denyVerbose: z.string().optional(),
  timeout: z.number().min(1),
  tips: z.string().optional(),
  isGlobal: z.boolean(),
  enabled: z.boolean(),
});

export type CliCredentialFormData = z.infer<typeof cliCredentialSchema>;
