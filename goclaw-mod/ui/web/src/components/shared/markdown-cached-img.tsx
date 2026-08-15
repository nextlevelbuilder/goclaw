/**
 * CachedMarkdownImg — image component for MarkdownRenderer that:
 * - Resolves /v1/files/ URLs via useMediaUrl cache
 * - Opens lightbox on click
 * - Shows a download button on hover
 */
import { Download } from "lucide-react";
import { toFileUrl, toDownloadUrl } from "@/lib/file-helpers";
import { useMediaUrl } from "@/hooks/use-media-url";

function fileNameFromHref(href: string): string {
  const path = href.split("?")[0] ?? href;
  const segments = path.split("/");
  return segments[segments.length - 1] ?? "file";
}

export function CachedMarkdownImg({
  src,
  alt,
  openLightbox,
  ...props
}: {
  src?: string;
  alt?: string;
  openLightbox: (src: string, alt: string) => void;
  [key: string]: unknown;
}) {
  const isFileLink = src
    ? src.startsWith("/v1/files/") || src.includes("/v1/files/")
    : false;
  const resolvedSrc = isFileLink ? toFileUrl(src!) : src;
  const cachedSrc = useMediaUrl(resolvedSrc);
  const displayName = alt || fileNameFromHref(src ?? "");

  return (
    <span className="group/img relative inline-block overflow-hidden rounded-lg border shadow-sm">
      <img
        src={cachedSrc}
        alt={alt ?? "image"}
        className="block max-w-sm cursor-pointer hover:opacity-90 transition-opacity"
        loading="lazy"
        onClick={(e: React.MouseEvent) => {
          e.preventDefault();
          if (resolvedSrc) openLightbox(resolvedSrc, alt ?? "image");
        }}
        {...props}
      />
      {resolvedSrc && (
        <a
          href={toDownloadUrl(resolvedSrc)}
          download={displayName}
          onClick={(e: React.MouseEvent) => e.stopPropagation()}
          className="absolute top-2 right-2 flex items-center justify-center rounded-lg bg-white/90 dark:bg-neutral-800/90 p-1.5 text-neutral-700 dark:text-neutral-200 shadow-md ring-1 ring-black/10 dark:ring-white/10 opacity-0 transition-opacity group-hover/img:opacity-100 hover:bg-white dark:hover:bg-neutral-700 cursor-pointer"
          title="Download"
        >
          <Download className="h-4.5 w-4.5" />
        </a>
      )}
    </span>
  );
}
