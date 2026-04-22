import { useState, useCallback } from "react";
import { Download, FileText, FileCode, Music, Film, File } from "lucide-react";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { formatSize, toDownloadUrl } from "@/lib/file-helpers";
import { useMediaUrl } from "@/hooks/use-media-url";
import { useChatImageGallery } from "./chat-image-gallery-context";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { MediaItem } from "@/types/chat";

/** Hex/UUID filename pattern — matches assistant-generated image basenames
 *  such as `a1b2c3d4e5f6.png` or `550e8400-e29b-41d4-a716-446655440000.png`. */
const GENERATED_FILENAME_RE = /^[0-9a-f-]{8,}\.png$/i;

/**
 * Format a Date as YYYYMMDD-HHmmss for use in download filenames.
 * Uses local time so filenames match the user's clock.
 */
function formatTimestamp(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}` +
    `-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`
  );
}

/**
 * Returns the download filename for an image MediaItem.
 * For assistant-generated PNGs (hex/UUID basename), uses a descriptive
 * `generated-YYYYMMDD-HHmmss.png` name instead of the opaque server hash.
 */
function resolveImageDownloadName(item: MediaItem): string {
  const base = item.fileName ?? "image";
  if (item.mimeType === "image/png" && GENERATED_FILENAME_RE.test(base)) {
    return `generated-${formatTimestamp(new Date())}.png`;
  }
  return base;
}

interface MediaGalleryProps {
  items: MediaItem[];
}

function fileIcon(kind: MediaItem["kind"]) {
  switch (kind) {
    case "document": return <FileText className="h-4 w-4 text-muted-foreground" />;
    case "code":     return <FileCode className="h-4 w-4 text-muted-foreground" />;
    case "audio":    return <Music className="h-4 w-4 text-muted-foreground" />;
    case "video":    return <Film className="h-4 w-4 text-muted-foreground" />;
    default:         return <File className="h-4 w-4 text-muted-foreground" />;
  }
}

function isMarkdownExt(name: string): boolean {
  return /\.(md|mdx|markdown)$/i.test(name);
}

function isMediaKind(kind: string): "image" | "audio" | "video" | null {
  if (kind === "image" || kind === "audio" || kind === "video") return kind;
  return null;
}

/** Image with blob-cached src to prevent flickering on session switch. */
function CachedImage({ src, alt, className, loading, onClick }: {
  src: string; alt: string; className?: string; loading?: "lazy" | "eager";
  onClick?: () => void;
}) {
  const cachedSrc = useMediaUrl(src);
  return <img src={cachedSrc} alt={alt} className={className} loading={loading} onClick={onClick} />;
}

export function MediaGallery({ items }: MediaGalleryProps) {
  const { openImage } = useChatImageGallery();
  const [preview, setPreview] = useState<{
    name: string;
    href: string;
    content: string;
    mediaType?: "image" | "audio" | "video";
  } | null>(null);
  const [loading, setLoading] = useState(false);

  const handleFileClick = useCallback((item: MediaItem) => {
    const media = isMediaKind(item.kind);
    if (media) {
      setPreview({ name: item.fileName ?? "file", href: item.path, content: "", mediaType: media });
      return;
    }
    // Text/code/document files: fetch content for preview
    setLoading(true);
    fetch(item.path)
      .then((res) => {
        if (!res.ok) throw new Error(res.statusText);
        return res.text();
      })
      .then((text) => setPreview({ name: item.fileName ?? "file", href: item.path, content: text }))
      .catch(() => { /* fetch failed — file may not exist yet, ignore */ })
      .finally(() => setLoading(false));
  }, []);

  if (items.length === 0) return null;

  const images = items.filter((i) => i.kind === "image");
  const files  = items.filter((i) => i.kind !== "image");

  return (
    <div className="space-y-2">
      {images.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {images.map((item, i) => (
            <div key={i} className="flex flex-col overflow-hidden rounded-lg border">
              <div className="group relative">
                <button
                  type="button"
                  onClick={() => openImage(item.path)}
                  className="block w-full cursor-pointer"
                >
                  <CachedImage
                    src={item.path}
                    alt={item.fileName ?? ""}
                    className="h-40 w-full object-cover"
                    loading="lazy"
                  />
                </button>
                <div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/50 via-transparent to-transparent opacity-0 transition-opacity group-hover:opacity-100" />
                <div className="absolute inset-x-0 bottom-0 flex items-end justify-between px-2 pb-1.5 opacity-0 transition-opacity group-hover:opacity-100">
                  <div className="flex min-w-0 flex-col text-xs text-white drop-shadow-sm">
                    {item.fileName && <span className="truncate">{item.fileName}</span>}
                    {item.size != null && item.size > 0 && (
                      <span className="text-white/70">{formatSize(item.size)}</span>
                    )}
                  </div>
                  <a
                    href={toDownloadUrl(item.path)}
                    download={resolveImageDownloadName(item)}
                    onClick={(e) => e.stopPropagation()}
                    className="shrink-0 rounded-lg bg-white/90 dark:bg-neutral-800/90 p-1.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer"
                    title="Download"
                  >
                    <Download className="h-4.5 w-4.5" />
                  </a>
                </div>
              </div>
              {item.prompt && (
                <div
                  className="px-2 py-1 text-xs text-muted-foreground italic line-clamp-2"
                  title={item.prompt}
                >
                  {item.prompt}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {files.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {files.map((item, i) => (
            <div key={i} className="flex items-center rounded-md border bg-muted/50 text-sm">
              <button
                type="button"
                onClick={() => handleFileClick(item)}
                className="flex items-center gap-2 px-3 py-1.5 hover:bg-muted cursor-pointer rounded-l-md"
              >
                {fileIcon(item.kind)}
                <span className="max-w-[200px] truncate">{item.fileName ?? "file"}</span>
                {item.size != null && item.size > 0 && (
                  <span className="text-xs text-muted-foreground">{formatSize(item.size)}</span>
                )}
              </button>
              <a
                href={toDownloadUrl(item.path)}
                download={item.fileName ?? "file"}
                className="flex items-center px-2 py-1.5 text-muted-foreground hover:bg-muted cursor-pointer rounded-r-md border-l"
                onClick={(e) => e.stopPropagation()}
              >
                <Download className="h-3.5 w-3.5" />
              </a>
            </div>
          ))}
        </div>
      )}

      {loading && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/50">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
        </div>
      )}

      <Dialog open={!!preview} onOpenChange={(open) => { if (!open) setPreview(null); }}>
        {preview && (
          <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
            <DialogHeader className="flex-row items-center gap-2 pr-10">
              <DialogTitle className="truncate text-base flex-1">{preview.name}</DialogTitle>
              <a
                href={toDownloadUrl(preview.href)}
                download={preview.name}
                className="flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs text-muted-foreground hover:bg-muted"
              >
                <Download className="h-3.5 w-3.5" />
                Download
              </a>
            </DialogHeader>
            <div className="min-h-0 flex-1 overflow-y-auto rounded-md border bg-muted/20 p-4">
              {preview.mediaType === "image" ? (
                <img src={preview.href} alt={preview.name} className="max-w-full rounded" />
              ) : preview.mediaType === "audio" ? (
                <audio controls src={preview.href} className="w-full" />
              ) : preview.mediaType === "video" ? (
                <video controls src={preview.href} className="max-w-full rounded" />
              ) : isMarkdownExt(preview.name) ? (
                <MarkdownRenderer content={preview.content} />
              ) : (
                <pre className="whitespace-pre-wrap text-xs font-mono"><code>{preview.content}</code></pre>
              )}
            </div>
          </DialogContent>
        )}
      </Dialog>
    </div>
  );
}
