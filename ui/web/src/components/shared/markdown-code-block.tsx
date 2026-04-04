/**
 * CodeBlock component for MarkdownRenderer — syntax-highlighted code block
 * with a copy button and language label in the header bar.
 */
import { useTranslation } from "react-i18next";
import { Check, Copy } from "lucide-react";
import { useClipboard } from "@/hooks/use-clipboard";

export function CodeBlock({
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
    <div className="not-prose group relative my-3 overflow-hidden rounded-lg border border-border/60">
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
      <pre className="overflow-x-auto bg-muted/30 p-3 text-[13px] leading-normal text-foreground whitespace-pre">
        <code
          className={className}
          style={{
            fontFamily: "'JetBrains Mono', 'Fira Code', ui-monospace, monospace",
            wordWrap: "normal",
            overflowWrap: "normal",
          }}
        >
          {children}
        </code>
      </pre>
    </div>
  );
}
