import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { useClipboard } from "@/hooks/use-clipboard";
import { useAuthStore } from "@/stores/use-auth-store";
import { toFileUrl } from "@/lib/file-helpers";
import { Check, Copy, Download, FileText } from "lucide-react";
import { ImageLightbox } from "./image-lightbox";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

function CodeBlock({
  className,
  children,
}: {
  className?: string;
  children?: React.ReactNode;
}) {
  const { copied, copy } = useClipboard();
  const { t } = useTranslation("common");
  const text = String(children).replace(/\n$/, "");
  const lang = className?.replace("language-", "") ?? "";

  return (
    <div className="not-prose group relative my-6 overflow-hidden rounded-lg border border-border/60">
      <div className="flex items-center justify-between border-b border-border/40 bg-muted/70 px-3 py-1.5 text-[11px] font-medium tracking-wide text-muted-foreground uppercase">
        <span>{lang || "code"}</span>
        <button
          type="button"
          onClick={() => copy(text)}
          className="cursor-pointer opacity-0 transition-opacity group-hover:opacity-100"
          title={t("copyCode")}
        >
          {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
        </button>
      </div>
      <pre className="overflow-x-auto bg-muted/30 p-4 text-[13px] leading-relaxed text-foreground whitespace-pre">
        <code className={className} style={{ fontFamily: "'JetBrains Mono', 'Fira Code', ui-monospace, monospace", wordWrap: "normal", overflowWrap: "normal" }}>{children}</code>
      </pre>
    </div>
  );
}

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

export function MarkdownRenderer({ content, className }: MarkdownRendererProps) {
  const token = useAuthStore((s) => s.token);
  const [lightbox, setLightbox] = useState<{ src: string; alt: string } | null>(null);
  const openLightbox = useCallback((src: string, alt: string) => setLightbox({ src, alt }), []);
  const [filePreview, setFilePreview] = useState<{ name: string; href: string; content: string; mediaType?: "image" | "audio" | "video" } | null>(null);
  const [fileLoading, setFileLoading] = useState(false);

  const handleFileClick = useCallback((href: string, name: string) => {
    // Media files: open preview directly without fetching text content
    const media = isMediaFile(name);
    if (media) {
      setFilePreview({ name, href, content: "", mediaType: media });
      return;
    }
    // Text/code files: fetch content (href already includes ?token= from toFileUrl)
    setFileLoading(true);
    fetch(href)
      .then((res) => {
        if (!res.ok) throw new Error(res.statusText);
        return res.text();
      })
      .then((text) => setFilePreview({ name, href, content: text }))
      .catch(() => { /* fetch failed — file may not exist, ignore */ })
      .finally(() => setFileLoading(false));
  }, []);

  return (
    <div className={`md-render prose dark:prose-invert max-w-none break-words ${className ?? ""}`}>
      {lightbox && (
        <ImageLightbox src={lightbox.src} alt={lightbox.alt} onClose={() => setLightbox(null)} />
      )}
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeHighlight]}
        components={{
          pre({ children }) {
            // Strip the outer <pre> from ReactMarkdown — CodeBlock renders its own
            return <>{children}</>;
          },
          code({ className, children, node, ...props }) {
            // Block code: has className OR parent is <pre>
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
          a({ href, children }) {
            if (isFileLink(href)) {
              const resolvedHref = toFileUrl(href!, token);
              const name = typeof children === "string" ? children : fileNameFromHref(href!);
              return (
                <span className="inline-flex items-center gap-0.5 rounded border bg-muted/50 text-[0.85em] font-medium">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 px-1.5 py-0.5 text-primary hover:bg-muted cursor-pointer rounded-l"
                    onClick={(e) => { e.preventDefault(); handleFileClick(resolvedHref, name); }}
                  >
                    <FileText className="h-3.5 w-3.5" />
                    {children}
                  </button>
                  <a
                    href={resolvedHref}
                    download={name}
                    className="inline-flex items-center px-1 py-0.5 text-muted-foreground hover:bg-muted cursor-pointer rounded-r border-l"
                    onClick={(e) => e.stopPropagation()}
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
          img({ src, alt, ...props }) {
            const resolvedSrc = isFileLink(src) ? toFileUrl(src!, token) : src;
            return (
              <img
                src={resolvedSrc}
                alt={alt ?? "image"}
                className="max-w-sm rounded-lg border shadow-sm cursor-pointer hover:opacity-90 transition-opacity"
                loading="lazy"
                onClick={(e) => {
                  e.preventDefault();
                  if (resolvedSrc) openLightbox(resolvedSrc, alt ?? "image");
                }}
                {...props}
              />
            );
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
          hr({ ...props }) {
            return <hr className="my-6 border-none h-0.5 bg-border" {...props} />;
          },
          input({ type, checked, ...props }) {
            if (type === "checkbox") {
              return <input type="checkbox" checked={checked} disabled className="mr-1" {...props} />;
            }
            return <input type={type} {...props} />;
          },
        }}
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
                href={filePreview.href}
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
}
