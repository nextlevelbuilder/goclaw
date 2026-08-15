import { z } from "zod";

export const s3ConfigSchema = z.object({
  access_key_id: z.string().min(1, "Required"),
  secret_access_key: z.string(),
  bucket: z.string().min(1, "Required"),
  region: z.string(),
  endpoint: z.string(),
  prefix: z.string(),
});

export type S3ConfigFormData = z.infer<typeof s3ConfigSchema>;
