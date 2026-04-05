/**
 * Convert any string to a valid slug: lowercase, [a-z0-9-], no leading/trailing dashes.
 * Handles Vietnamese and other diacritical characters by stripping accents first.
 */
export function slugify(input: string): string {
  return input
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .replace(/đ/g, 'd')
    .replace(/Đ/g, 'd')
    .toLowerCase()
    .replace(/[^a-z0-9-]/g, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^-+/g, '')
    .replace(/-+$/g, '')
}

/**
 * Validate slug format: lowercase alphanumeric + hyphens, cannot start/end with hyphen.
 */
export function isValidSlug(slug: string): boolean {
  return /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/.test(slug)
}
