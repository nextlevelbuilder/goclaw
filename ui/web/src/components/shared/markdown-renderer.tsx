import { useState, useCallback, useEffect, useRef, useMemo, memo } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";

// Stable plugin arrays — avoids new references on every render
const remarkPlugins = [remarkGfm];
const rehypePlugins = [rehypeHighlight];
import { toFileUrl, toDownloadUrl } from "@/lib/file-helpers";
import { Download, FileText } from "lucide-react";
import { ImageLightbox } from "./image-lightbox";
import { useChatImageGallery } from "@/components/chat/chat-image-gallery-context";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { CodeBlock } from "./markdown-code-block";
import { CachedMarkdownImg } from "./markdown-cached-img";

interface MarkdownRendererProps {
  content: string;
  className?: string;
}

/** Common file extensions for generated/local files */
const LOCAL_FILE_EXT_RE = /\.(png|jpg|jpeg|gif|webp|svg|bmp|mp3|wav|ogg|flac|aac|m4a|mp4|webm|mkv|avi|mov|pdf|doc|docx|xls|xlsx|csv|txt|md|json|zip)$/i;

/** Check if a URL points to a local file (via /v1/files/ or relative path) */
function isFileLink(href: string | undefined): boolean {
  if (!href) return false;
  if (href.startsWith("/v1/files/") || href.includes("/v1/files/")) return true;
  // Detect relative paths with file extensions (e.g. ./system/generated/file.png)
  if ((href.startsWith("./") || href.startsWith("../")) && LOCAL_FILE_EXT_RE.test(href)) return true;
  return false;
}

/** File type detection from name */
function isMarkdownExt(name: string): boolean {
  return /\.(md|mdx|markdown)$/i.test(name);
}
function isMediaFile(name: string): "image" | "audio" | "video" | null {
  if (/\.(jpg|jpeg|png|gif|webp|svg|bmp|ico)$/i.test(name)) return "image";
  if (/\.(mp3|wav|ogg|flac|aac|m4a|wma|opus)$/i.test(name)) return "audio";
  if (/\.(mp4|webm|mkv|avi|mov|wmv)$/i.test(name)) return "video";
  return null;
}

/** Extract filename from /v1/files/ URL */
function fileNameFromHref(href: string): string {
  const path = href.split("?")[0] ?? href;
  const segments = path.split("/");
  return segments[segments.length - 1] ?? "file";
}

export const MarkdownRenderer = memo(function MarkdownRenderer({ content, className }: MarkdownRendererProps) {
  const gallery = useChatImageGallery();
  const [lightbox, setLightbox] = useState<{ src: string; alt: string } | null>(null);
  // Use conversation-wide gallery if available (has images), else fall back to local lightbox
  const openLightbox = useCallback((src: string, alt: string) => {
    if (gallery.allImages.length > 0) {
      gallery.openImage(src);
    } else {
      setLightbox({ src, alt });
    }
  }, [gallery]);
  const [filePreview, setFilePreview] = useState<{ name: string; href: string; content: string; mediaType?: "image" | "audio" | "video" } | null>(null);
  const [fileLoading, setFileLoading] = useState(false);

  const abortRef = useRef<AbortController | null>(null);

  // Abort any in-flight file fetch on unmount
  useEffect(() => {
    return () => { abortRef.current?.abort(); };
  }, []);

  const handleFileClick = useCallback((href: string, name: string) => {
    // Media files: open preview directly without fetching text content
    const media = isMediaFile(name);
    if (media) {
      setFilePreview({ name, href, content: "", mediaType: media });
      return;
    }
    // Abort any in-flight fetch before starting a new one
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    // Text/code files: fetch content (href already includes ?ft= from server signing)
    setFileLoading(true);
    fetch(href, { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error(res.statusText);
        return res.text();
      })
      .then((text) => setFilePreview({ name, href, content: text }))
      .catch((err) => { if (err.name !== "AbortError") { /* fetch failed — file may not exist, ignore */ } })
      .finally(() => setFileLoading(false));
  }, []);

  // Stable components config — only recreated when token/callbacks change.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const components = useMemo((): Components => ({
    pre({ children }) {
      return <>{children}</>;
    },
    code({ className, children, node, ...props }: any) {
      const isBlock = !!className || node?.position?.start.line !== node?.position?.end.line || String(children).includes("\n");
      if (isBlock) {
        return <CodeBlock className={className}>{children}</CodeBlock>;
      }
      return (
        <code className="rounded bg-muted px-1.5 py-0.5 text-[0.85em] font-medium text-primary" style={{ fontFamily: "'JetBrains Mono', 'Fira Code', ui-monospace, monospace" }} {...props}>
          {children}
        </code>
      );
    },
    a({ href, children }: any) {
      if (isFileLink(href)) {
        const resolvedHref = toFileUrl(href!);
        const name = typeof children === "string" ? children : fileNameFromHref(href!);
        return (
          <span className="inline-flex items-center gap-0.5 rounded border bg-muted/50 text-[0.85em] font-medium">
            <button
              type="button"
              className="inline-flex items-center gap-1 px-1.5 py-0.5 text-primary hover:bg-muted cursor-pointer rounded-l"
              onClick={(e: React.MouseEvent) => { e.preventDefault(); handleFileClick(resolvedHref, name); }}
            >
              <FileText className="h-3.5 w-3.5" />
              {children}
            </button>
            <a
              href={toDownloadUrl(resolvedHref)}
              download={name}
              className="inline-flex items-center px-1 py-0.5 text-muted-foreground hover:bg-muted cursor-pointer rounded-r border-l"
              onClick={(e: React.MouseEvent) => e.stopPropagation()}
            >
              <Download className="h-3 w-3" />
            </a>
          </span>
        );
      }
      return (
        <a href={href} target="_blank" rel="noopener noreferrer">
          {children}
        </a>
      );
    },
    img({ src, alt, ...props }: any) {
      return <CachedMarkdownImg src={src} alt={alt} openLightbox={openLightbox} {...props} />;
    },
    table({ children, ...props }) {
      return (
        <div className="not-prose my-4 overflow-x-auto">
          <table className="w-full border-collapse text-[13px]" {...props}>{children}</table>
        </div>
      );
    },
    thead({ children, ...props }) {
      return <thead {...props}>{children}</thead>;
    },
    th({ children, ...props }) {
      return <th className="border border-border bg-muted px-3 py-1.5 text-left text-[13px] font-semibold" {...props}>{children}</th>;
    },
    td({ children, ...props }) {
      return <td className="border border-border px-3 py-1.5" {...props}>{children}</td>;
    },
    tr({ children, ...props }) {
      return <tr className="even:bg-muted/30" {...props}>{children}</tr>;
    },
    blockquote({ children, ...props }) {
      return (
        <blockquote className="my-4 border-l-4 border-muted-foreground rounded-r-md bg-muted px-4 py-3 text-muted-foreground not-italic" {...props}>
          {children}
        </blockquote>
      );
    },
    hr(props) {
      return <hr className="my-6 border-none h-0.5 bg-border" {...props} />;
    },
    input({ type, checked, ...props }: any) {
      if (type === "checkbox") {
        return <input type="checkbox" checked={checked} disabled className="mr-1" {...props} />;
      }
      return <input type={type} {...props} />;
    },
  }), [openLightbox, handleFileClick]);

  return (
    <div className={`md-render prose dark:prose-invert max-w-none break-words ${className ?? ""}`}>
      {lightbox && (
        <ImageLightbox src={lightbox.src} alt={lightbox.alt} onClose={() => setLightbox(null)} />
      )}
      <ReactMarkdown
        remarkPlugins={remarkPlugins}
        rehypePlugins={rehypePlugins}
        components={components}
      >
        {content}
      </ReactMarkdown>

      {fileLoading && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-background/50">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
        </div>
      )}

      <Dialog open={!!filePreview} onOpenChange={(open) => { if (!open) setFilePreview(null); }}>
        {filePreview && (
          <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col">
            <DialogHeader className="flex-row items-center gap-2 pr-10">
              <DialogTitle className="truncate text-base flex-1">{filePreview.name}</DialogTitle>
              <a
                href={toDownloadUrl(filePreview.href)}
                download={filePreview.name}
                className="flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs text-muted-foreground hover:bg-muted"
              >
                <Download className="h-3.5 w-3.5" />
                Download
              </a>
            </DialogHeader>
            <div className="min-h-0 flex-1 overflow-y-auto rounded-md border bg-muted/20 p-4">
              {filePreview.mediaType === "image" ? (
                <img src={filePreview.href} alt={filePreview.name} className="max-w-full rounded" />
              ) : filePreview.mediaType === "audio" ? (
                <audio controls src={filePreview.href} className="w-full" />
              ) : filePreview.mediaType === "video" ? (
                <video controls src={filePreview.href} className="max-w-full rounded" />
              ) : isMarkdownExt(filePreview.name) ? (
                <MarkdownRenderer content={filePreview.content} />
              ) : (
                <pre className="whitespace-pre-wrap text-xs font-mono"><code>{filePreview.content}</code></pre>
              )}
            </div>
          </DialogContent>
        )}
      </Dialog>
    </div>
  );
});
