import type { Message } from "@/types/session";

/**
 * Derive a numeric timestamp (ms since epoch) from a message.
 * Uses server-provided `created_at` when available; falls back to
 * synthetic spacing (1 s apart, ending at "now") for older messages.
 */
export function messageToTimestamp(
  msg: Message,
  index: number,
  total: number,
): number {
  return msg.created_at
    ? new Date(msg.created_at).getTime()
    : Date.now() - (total - index) * 1000;
}
