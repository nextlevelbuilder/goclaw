type ContactLike = { display_name?: string; username?: string } | null;
type Resolver = (id: string) => ContactLike;

/**
 * Format a user/sender ID into a human-readable label.
 * Display hierarchy: display_name > @username > formatted ID fallback.
 */
export function formatUserLabel(userId: string, resolve?: Resolver): string {
  if (!userId) return "";

  // Try contact resolver first
  if (resolve) {
    const contact = resolve(userId);
    if (contact?.display_name) return contact.display_name;
    if (contact?.username) return `@${contact.username}`;
  }

  // Special cases
  if (userId === "system") return "System";
  if (userId.startsWith("group:")) {
    const parts = userId.split(":");
    if (parts.length >= 3) {
      const channel = parts[1]!.charAt(0).toUpperCase() + parts[1]!.slice(1);
      return `${channel} ${parts.slice(2).join(":")}`;
    }
  }

  // Fallback: prefix numeric IDs with #, string IDs with @
  if (/^\d+$/.test(userId)) return `#${userId}`;
  return userId;
}
