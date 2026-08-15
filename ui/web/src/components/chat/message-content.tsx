import { RichContent } from "./rich-content";

interface MessageContentProps {
  content: string;
  role: string;
  /** Basenames of media files rendered separately by MediaGallery — strip from markdown. */
  mediaBasenames?: string[];
}

export function MessageContent({ content, role, mediaBasenames }: MessageContentProps) {
  const cleaned = mediaBasenames?.length ? stripGalleryDuplicates(content, mediaBasenames) : content;
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
