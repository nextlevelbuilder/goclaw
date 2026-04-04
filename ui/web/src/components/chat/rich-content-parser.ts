/**
 * Parser and deduplicator for chat message rich content blocks.
 * Extracts special markup (media tags, file blocks, forwarded messages, etc.)
 * into structured RichBlock objects for rendering.
 */

// <media:image>, <media:video>, <media:audio>, <media:voice>, <media:document>
const MEDIA_TAG_RE = /<media:(image|video|audio|voice|document|animation)>/g;

// <file name="..." mime="...">content</file>
const FILE_BLOCK_RE = /<file\s+name="([^"]+)"\s+mime="([^"]*)">([\s\S]*?)<\/file>/g;

// [Forwarded from Name at Date] at the start
const FORWARD_RE = /^\[Forwarded from (.+?) at (.+?)\]\n?/;

// [Replying to Sender]\nbody\n[/Replying]
const REPLY_RE = /\[Replying to (.+?)\]\n([\s\S]*?)\n\[\/Replying\]/;

// [Video received — ...]
const VIDEO_NOTICE_RE = /\[Video received[^\]]*\]/g;

// Location: Coordinates: lat, lng
const LOCATION_RE = /Coordinates:\s*([-\d.]+),\s*([-\d.]+)/;

export type RichBlock =
  | { type: "markdown"; content: string }
  | { type: "media"; mediaType: string }
  | { type: "video-notice"; content: string }
  | { type: "file"; name: string; mime: string; content: string }
  | { type: "forward"; from: string; date: string }
  | { type: "reply"; sender: string; body: string }
  | { type: "location"; lat: string; lng: string };

/** Parse message content into rich blocks for rendering */
export function parseRichContent(content: string): RichBlock[] {
  const blocks: RichBlock[] = [];
  let text = content;

  // Extract forward info (always at start)
  const fwdMatch = text.match(FORWARD_RE);
  if (fwdMatch) {
    blocks.push({ type: "forward", from: fwdMatch[1]!, date: fwdMatch[2]! });
    text = text.slice(fwdMatch[0].length);
  }

  // Extract reply block
  const replyMatch = text.match(REPLY_RE);
  let replyBlock: RichBlock | null = null;
  if (replyMatch) {
    replyBlock = { type: "reply", sender: replyMatch[1]!, body: replyMatch[2]! };
    text = text.replace(REPLY_RE, "");
  }

  // Extract file blocks
  const fileBlocks: RichBlock[] = [];
  text = text.replace(FILE_BLOCK_RE, (_match, name: string, mime: string, body: string) => {
    fileBlocks.push({ type: "file", name, mime, content: body });
    return "";
  });

  // Extract media tags
  const mediaBlocks: RichBlock[] = [];
  text = text.replace(MEDIA_TAG_RE, (_match, mediaType: string) => {
    mediaBlocks.push({ type: "media", mediaType });
    return "";
  });

  // Extract video notices
  text = text.replace(VIDEO_NOTICE_RE, (match) => {
    mediaBlocks.push({ type: "video-notice", content: match });
    return "";
  });

  // Extract location
  const locMatch = text.match(LOCATION_RE);
  let locationBlock: RichBlock | null = null;
  if (locMatch) {
    locationBlock = { type: "location", lat: locMatch[1]!, lng: locMatch[2]! };
    text = text.replace(LOCATION_RE, "");
  }

  // Build final block list: forward → media → markdown → files → reply → location
  if (mediaBlocks.length > 0) blocks.push(...mediaBlocks);

  // Clean up leftover whitespace
  const trimmed = text.replace(/\n{3,}/g, "\n\n").trim();
  if (trimmed) blocks.push({ type: "markdown", content: trimmed });

  if (fileBlocks.length > 0) blocks.push(...fileBlocks);
  if (replyBlock) blocks.push(replyBlock);
  if (locationBlock) blocks.push(locationBlock);

  return blocks;
}

/** Extract basename from a markdown link/image URL, stripping query params. */
function linkBasename(line: string): string | null {
  const m = line.match(/\]\(([^)]+)\)/);
  if (!m?.[1]) return null;
  const url = m[1].split("?")[0] ?? "";
  const slash = url.lastIndexOf("/");
  return slash >= 0 ? url.slice(slash + 1) : url;
}

/**
 * Remove duplicate media links from content. When the same file (by basename)
 * appears in both the agent's body text and an appended ContentSuffix block,
 * keep the first occurrence and drop later duplicates.
 */
export function deduplicateMediaLinks(content: string): string {
  const lines = content.split("\n");
  // Pass 1: collect first occurrence index of each media basename (inline or standalone).
  const firstSeen = new Map<string, number>();
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    if (!line.includes("](/")) continue;
    const base = linkBasename(line);
    if (base && !firstSeen.has(base)) firstSeen.set(base, i);
  }
  // Pass 2: drop STANDALONE link/image lines whose basename appeared in an earlier line.
  const seen = new Set<string>();
  const result: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? "";
    const trimmed = line.trim();
    const isStandaloneLink = (trimmed.startsWith("![") || trimmed.startsWith("[")) &&
      trimmed.includes("](/") && trimmed.endsWith(")");
    if (isStandaloneLink) {
      const base = linkBasename(trimmed);
      if (base) {
        if (seen.has(base)) continue; // duplicate standalone — drop
        seen.add(base);
        // Also skip if this basename appeared inline in an earlier line
        const first = firstSeen.get(base);
        if (first !== undefined && first < i) continue;
      }
    }
    result.push(line);
  }
  return result.join("\n");
}
