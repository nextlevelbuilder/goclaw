import { RichContent } from "./rich-content";

interface MessageContentProps {
  content: string;
  role: string;
  /** Basenames of media files rendered separately by MediaGallery — strip from markdown. */
  mediaBasenames?: string[];
}

export function MessageContent({ content, role, mediaBasenames }: MessageContentProps) {
  const normalized = normalizeLegacyUserTailDuplicate(content, role);
  const cleaned = mediaBasenames?.length ? stripGalleryDuplicates(normalized, mediaBasenames) : normalized;
  return <RichContent content={cleaned} role={role} />;
}

/**
 * Remove standalone markdown image/link lines whose basename matches a MediaGallery item.
 * This prevents the same file from appearing twice (once in markdown, once in gallery).
 */
function stripGalleryDuplicates(content: string, basenames: string[]): string {
  if (!basenames.length) return content;
  const baseSet = new Set(basenames);
  return content
    .split("\n")
    .filter((line) => {
      const trimmed = line.trim();
      if (!(trimmed.startsWith("![") || trimmed.startsWith("[")) || !trimmed.includes("](/") || !trimmed.endsWith(")")) {
        return true; // not a standalone link — keep
      }
      const m = trimmed.match(/\]\(([^)]+)\)/);
      if (!m?.[1]) return true;
      const url = m[1].split("?")[0] ?? "";
      const base = url.split("/").pop() ?? "";
      return !baseSet.has(base); // drop if gallery already shows this file
    })
    .join("\n");
}

function normalizeLegacyUserTailDuplicate(content: string, role: string): string {
  if (role !== "user") return content;

  const normalized = content.replace(/\r\n/g, "\n");
  const parts = normalized.split(/\n{2,}/).map((part) => part.trim()).filter(Boolean);
  if (parts.length !== 2) return content;

  const main = parts[0];
  const trailing = parts[1];
  if (!main || !trailing) return content;

  const lastWord = main.split(/\s+/).filter(Boolean).pop();
  if (!lastWord) return content;

  const canonicalTrailing = trailing.trim();
  if (!canonicalTrailing || canonicalTrailing.includes("\n")) return content;
  if (canonicalTrailing !== lastWord) return content;

  return main;
}
