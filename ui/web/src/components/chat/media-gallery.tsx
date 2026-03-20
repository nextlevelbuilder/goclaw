import { useState } from "react";
import { File, FileText, FileCode, Music, Film } from "lucide-react";
import { ImageLightbox } from "@/components/shared/image-lightbox";
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

export function MediaGallery({ items }: MediaGalleryProps) {
  const [lightboxIdx, setLightboxIdx] = useState<number | null>(null);

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
            <a
              key={i}
              href={item.path.startsWith("javascript:") ? "#" : item.path}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2 rounded-md border bg-muted/50 px-3 py-1.5 text-sm hover:bg-muted"
            >
              {fileIcon(item.kind)}
              <span className="max-w-[200px] truncate">{item.fileName ?? "file"}</span>
            </a>
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
    </div>
  );
}
