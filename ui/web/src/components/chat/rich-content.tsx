import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import {
  Image,
  Video,
  Mic,
  FileText,
  Forward,
  Reply,
  MapPin,
  Download,
  ChevronRight,
} from "lucide-react";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

// --- Patterns to detect special blocks ---

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

type RichBlock =
  | { type: "markdown"; content: string }
  | { type: "media"; mediaType: string }
  | { type: "video-notice"; content: string }
  | { type: "file"; name: string; mime: string; content: string }
  | { type: "forward"; from: string; date: string }
  | { type: "reply"; sender: string; body: string }
  | { type: "location"; lat: string; lng: string };

/** Parse message content into rich blocks for rendering */
function parseRichContent(content: string): RichBlock[] {
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
  if (mediaBlocks.length > 0) {
    blocks.push(...mediaBlocks);
  }

  // Clean up leftover whitespace
  const trimmed = text.replace(/\n{3,}/g, "\n\n").trim();
  if (trimmed) {
    blocks.push({ type: "markdown", content: trimmed });
  }

  if (fileBlocks.length > 0) {
    blocks.push(...fileBlocks);
  }

  if (replyBlock) {
    blocks.push(replyBlock);
  }

  if (locationBlock) {
    blocks.push(locationBlock);
  }

  return blocks;
}

// --- Renderers for each block type ---

const mediaIcons: Record<string, typeof Image> = {
  image: Image,
  video: Video,
  audio: Mic,
  voice: Mic,
  document: FileText,
  animation: Video,
};

function MediaBadge({ mediaType }: { mediaType: string }) {
  const { t } = useTranslation("chat");
  const Icon = mediaIcons[mediaType] ?? FileText;
  const label = t(`media.${mediaType}`, { defaultValue: mediaType });

  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border border-blue-200 bg-blue-50 px-2 py-1 text-xs font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300">
      <Icon className="h-3.5 w-3.5" />
      {label} {t("media.attached")}
    </span>
  );
}

function ForwardBadge({ from, date }: { from: string; date: string }) {
  const { t } = useTranslation("chat");
  return (
    <div className="flex items-center gap-1.5 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300">
      <Forward className="h-3.5 w-3.5" />
      <span>
        {t("forwardedFrom")} <span className="font-medium">{from}</span>
        {date && <span className="text-amber-600 dark:text-amber-400"> &middot; {date}</span>}
      </span>
    </div>
  );
}

function ReplyQuote({ sender, body }: { sender: string; body: string }) {
  const { t } = useTranslation("chat");
  return (
    <div className="rounded-md border-l-2 border-muted-foreground/40 bg-muted/50 px-3 py-2">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Reply className="h-3 w-3" />
        {t("replyingTo")} {sender}
      </div>
      <div className="mt-1 text-xs text-muted-foreground/80 line-clamp-3">{body}</div>
    </div>
  );
}

/** Whether file content should be rendered as markdown (vs raw code) */
function isMarkdownFile(name: string, mime: string): boolean {
  return /\.(md|mdx|markdown)$/i.test(name) || mime.startsWith("text/markdown");
}

function FileBlock({ name: rawName, mime, content }: { name: string; mime: string; content: string }) {
  // Strip query params (e.g. ?ft=token) from filename for display and extension detection
  const name = rawName.replace(/\?.*$/, "");
  const [open, setOpen] = useState(false);

  const handleDownload = useCallback(() => {
    const blob = new Blob([content], { type: mime || "text/plain" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    a.click();
    URL.revokeObjectURL(url);
  }, [content, mime, name]);

  const renderMarkdown = isMarkdownFile(name, mime);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="flex w-full cursor-pointer items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-left text-xs font-medium hover:bg-muted/50"
      >
        <FileText className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="flex-1 truncate">{name}</span>
        <span className="text-muted-foreground font-normal">{mime}</span>
        <ChevronRight className="h-3.5 w-3.5 text-muted-foreground" />
      </button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader className="flex-row items-center justify-between gap-2">
            <DialogTitle className="truncate text-base">{name}</DialogTitle>
            <button
              type="button"
              onClick={handleDownload}
              className="mr-8 flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs text-muted-foreground hover:bg-muted"
            >
              <Download className="h-3.5 w-3.5" />
              Download
            </button>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto rounded-md border bg-muted/20 p-4">
            {renderMarkdown ? (
              <MarkdownRenderer content={content} />
            ) : (
              <pre className="whitespace-pre-wrap text-xs font-mono">
                <code>{content}</code>
              </pre>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}

function LocationBadge({ lat, lng }: { lat: string; lng: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border border-green-200 bg-green-50 px-2 py-1 text-xs font-medium text-green-700 dark:border-green-800 dark:bg-green-950 dark:text-green-300">
      <MapPin className="h-3.5 w-3.5" />
      {lat}, {lng}
    </span>
  );
}

function VideoNoticeBadge({ content }: { content: string }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-md border border-muted bg-muted/50 px-2 py-1 text-xs text-muted-foreground">
      <Video className="h-3.5 w-3.5" />
      {content.replace(/^\[|\]$/g, "")}
    </span>
  );
}

// --- Media link dedup ---

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
function deduplicateMediaLinks(content: string): string {
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

// --- Main component ---

interface RichContentProps {
  content: string;
  role: string;
}

export function RichContent({ content, role }: RichContentProps) {
  const cleaned = deduplicateMediaLinks(content);
  const blocks = parseRichContent(cleaned);

  // If no special blocks found, render as plain markdown (fast path)
  const first = blocks[0];
  if (blocks.length === 1 && first?.type === "markdown") {
    return <MarkdownRenderer content={cleaned} className={role === "user" ? "text-sm" : ""} />;
  }

  return (
    <div className="flex flex-col gap-2">
      {blocks.map((block, i) => {
        switch (block.type) {
          case "forward":
            return <ForwardBadge key={i} from={block.from} date={block.date} />;
          case "media":
            return <MediaBadge key={i} mediaType={block.mediaType} />;
          case "video-notice":
            return <VideoNoticeBadge key={i} content={block.content} />;
          case "markdown":
            return <MarkdownRenderer key={i} content={block.content} className={role === "user" ? "text-sm" : ""} />;
          case "file":
            return <FileBlock key={i} name={block.name} mime={block.mime} content={block.content} />;
          case "reply":
            return <ReplyQuote key={i} sender={block.sender} body={block.body} />;
          case "location":
            return <LocationBadge key={i} lat={block.lat} lng={block.lng} />;
          default:
            return null;
        }
      })}
    </div>
  );
}
