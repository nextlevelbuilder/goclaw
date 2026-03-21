import { useEffect, useCallback } from "react";
import { X, Download } from "lucide-react";
import { formatSize } from "@/lib/file-helpers";

interface ImageLightboxProps {
  src: string;
  alt?: string;
  fileName?: string;
  size?: number;
  onClose: () => void;
}

export function ImageLightbox({ src, alt, fileName, size, onClose }: ImageLightboxProps) {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    },
    [onClose],
  );

  useEffect(() => {
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  const displayName = fileName || alt || "image";

  return (
    <div
      className="fixed inset-0 z-[100] flex flex-col items-center justify-center bg-black/80 backdrop-blur-sm"
      onClick={onClose}
    >
      <div className="absolute top-4 right-4 flex items-center gap-2">
        <a
          href={src}
          download={displayName}
          onClick={(e) => e.stopPropagation()}
          className="rounded-full bg-white/90 dark:bg-neutral-800/90 p-2.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer"
          title="Download"
        >
          <Download className="h-5 w-5" />
        </a>
        <button
          type="button"
          onClick={onClose}
          className="rounded-full bg-white/90 dark:bg-neutral-800/90 p-2.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 hover:bg-white dark:hover:bg-neutral-700 transition-colors cursor-pointer"
        >
          <X className="h-5 w-5" />
        </button>
      </div>
      <img
        src={src}
        alt={alt ?? "image"}
        className="max-h-[85vh] max-w-[90vw] rounded-lg object-contain"
        onClick={(e) => e.stopPropagation()}
      />
      {(fileName || (size != null && size > 0)) && (
        <div
          className="mt-3 flex items-center gap-2 rounded-full bg-black/60 px-4 py-1.5 text-sm text-white/90"
          onClick={(e) => e.stopPropagation()}
        >
          {fileName && <span className="max-w-[300px] truncate">{fileName}</span>}
          {fileName && size != null && size > 0 && <span className="text-white/50">·</span>}
          {size != null && size > 0 && <span className="text-white/60">{formatSize(size)}</span>}
        </div>
      )}
    </div>
  );
}
