import { useState, useCallback } from "react";
import { Download, FileText, FileCode, Music, Film, File } from "lucide-react";
import { ImageLightbox } from "@/components/shared/image-lightbox";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { formatSize } from "@/lib/file-helpers";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { MediaItem } from "@/types/chat";

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

export function MediaGallery({ items }: MediaGalleryProps) {
  const [lightboxIdx, setLightboxIdx] = useState<number | null>(null);
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
            <button
              key={i}
              type="button"
              onClick={() => setLightboxIdx(i)}
              className="relative overflow-hidden rounded-lg border cursor-pointer"
            >
              <img
                src={item.path}
                alt={item.fileName ?? ""}
                className="h-40 w-full object-cover"
                loading="lazy"
              />
            </button>
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
                href={item.path}
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

      {lightboxIdx !== null && images[lightboxIdx] && (
        <ImageLightbox
          src={images[lightboxIdx]!.path}
          alt={images[lightboxIdx]!.fileName ?? ""}
          onClose={() => setLightboxIdx(null)}
        />
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
                href={preview.href}
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
