import { useState, useCallback } from "react";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import rehypeHighlight from "rehype-highlight";
import { useClipboard } from "@/hooks/use-clipboard";
import { Check, Copy } from "lucide-react";
import { ImageLightbox } from "./image-lightbox";

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

export function MarkdownRenderer({ content, className }: MarkdownRendererProps) {
  const [lightbox, setLightbox] = useState<{ src: string; alt: string } | null>(null);
  const openLightbox = useCallback((src: string, alt: string) => setLightbox({ src, alt }), []);

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
          a({ href, children, ...props }) {
            return (
              <a href={href} target="_blank" rel="noopener noreferrer" {...props}>
                {children}
              </a>
            );
          },
          img({ src, alt, ...props }) {
            return (
              <img
                src={src}
                alt={alt ?? "image"}
                className="max-w-sm rounded-lg border shadow-sm cursor-pointer hover:opacity-90 transition-opacity"
                loading="lazy"
                onClick={(e) => {
                  e.preventDefault();
                  if (src) openLightbox(src, alt ?? "image");
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
    </div>
  );
}
