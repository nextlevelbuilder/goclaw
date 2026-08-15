import { useEffect, useCallback } from "react";
import { X, Download, ChevronLeft, ChevronRight } from "lucide-react";
import { formatSize, toDownloadUrl } from "@/lib/file-helpers";
import { useMediaUrl } from "@/hooks/use-media-url";

export interface LightboxImage {
  src: string;
  alt?: string;
  fileName?: string;
  size?: number;
}

interface ImageLightboxProps {
  src: string;
  alt?: string;
  fileName?: string;
  size?: number;
  onClose: () => void;
  /** Gallery mode: provide images array + currentIndex + onNavigate */
  images?: LightboxImage[];
  currentIndex?: number;
  onNavigate?: (index: number) => void;
}

const navBtnClass =
  "absolute top-1/2 -translate-y-1/2 rounded-full bg-white/90 dark:bg-neutral-800/90 p-2.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer";

const toolbarBtnClass =
  "rounded-full bg-white/90 dark:bg-neutral-800/90 p-2.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer";

/** Resolve current image — gallery mode uses images[currentIndex], single mode uses props directly. */
function resolveCurrentImage(props: ImageLightboxProps): LightboxImage {
  const { images, currentIndex, src, alt, fileName, size } = props;
  if (images && currentIndex != null && images[currentIndex]) {
    return images[currentIndex]!;
  }
  return { src, alt, fileName, size };
}

export function ImageLightbox(props: ImageLightboxProps) {
  const { onClose, images, currentIndex, onNavigate } = props;
  const current = resolveCurrentImage(props);
  const isGallery = images && images.length > 1 && onNavigate && currentIndex != null;
  const canPrev = isGallery && currentIndex > 0;
  const canNext = isGallery && currentIndex < images.length - 1;

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      if (isGallery) {
        if (e.key === "ArrowLeft" && canPrev) onNavigate(currentIndex - 1);
        if (e.key === "ArrowRight" && canNext) onNavigate(currentIndex + 1);
      }
    },
    [onClose, isGallery, canPrev, canNext, onNavigate, currentIndex],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  const displayName = current.fileName || current.alt || "image";
  const cachedSrc = useMediaUrl(current.src);

  return (
    <div
      className="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      {/* Toolbar */}
      <div className="absolute top-4 right-4 flex items-center gap-2">
        <a
          href={toDownloadUrl(current.src)}
          download={displayName}
          onClick={(e) => e.stopPropagation()}
          className={toolbarBtnClass}
          title="Download"
        >
          <Download className="h-5 w-5" />
        </a>
        <button type="button" onClick={onClose} className={toolbarBtnClass}>
          <X className="h-5 w-5" />
        </button>
      </div>

      {/* Prev / Next arrows */}
      {canPrev && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onNavigate(currentIndex - 1); }}
          className={`${navBtnClass} left-4`}
          title="Previous"
        >
          <ChevronLeft className="h-6 w-6" />
        </button>
      )}
      {canNext && (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onNavigate(currentIndex + 1); }}
          className={`${navBtnClass} right-4`}
          title="Next"
        >
          <ChevronRight className="h-6 w-6" />
        </button>
      )}

      {/* Image */}
      <img
        src={cachedSrc}
        alt={current.alt ?? "image"}
        className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
        onClick={(e) => e.stopPropagation()}
      />

      {/* Info bar: counter + filename + size */}
      {(isGallery || current.fileName || (current.size != null && current.size > 0)) && (
        <div
          className="mt-3 flex items-center gap-2 rounded-full bg-black/60 px-4 py-1.5 text-sm text-white/90"
          onClick={(e) => e.stopPropagation()}
        >
          {isGallery && (
            <>
              <span className="tabular-nums">{currentIndex + 1} / {images.length}</span>
              {(current.fileName || (current.size != null && current.size > 0)) && <span className="text-white/50">·</span>}
            </>
          )}
          {current.fileName && <span className="max-w-[300px] truncate">{current.fileName}</span>}
          {current.fileName && current.size != null && current.size > 0 && <span className="text-white/50">·</span>}
          {current.size != null && current.size > 0 && <span className="text-white/60">{formatSize(current.size)}</span>}
        </div>
      )}
    </div>
  );
}
