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
import { parseRichContent, deduplicateMediaLinks } from "./rich-content-parser";

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
