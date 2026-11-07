import { z } from 'zod'

/** Matches http(s) URLs; an empty string is allowed (field optional). */
const HTTP_URL = /^https?:\/\/\S+$/i

/**
 * Validation messages are i18n keys in the `providers` namespace
 * (`form.errors.*`); the form translates them next to each field.
 */
export const providerFormSchema = z.object({
  providerType: z.string().min(1, 'form.errors.typeRequired'),
  displayName: z.string(),
  apiBase: z.string().refine((v) => v.trim() === '' || HTTP_URL.test(v.trim()), 'form.errors.apiBaseScheme'),
  apiKey: z.string(),
  enabled: z.boolean(),
})

export type ProviderFormData = z.infer<typeof providerFormSchema>
