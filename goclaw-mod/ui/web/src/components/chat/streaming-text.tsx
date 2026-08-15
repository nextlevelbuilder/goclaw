import { memo } from "react";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";

interface StreamingTextProps {
  text: string;
}

export const StreamingText = memo(function StreamingText({ text }: StreamingTextProps) {
  if (!text) return <ThinkingIndicator />;

  return (
    <div className="relative">
      <MarkdownRenderer content={text} />
      <span className="inline-block h-4 w-0.5 animate-pulse bg-foreground align-text-bottom" />
    </div>
  );
});

function ThinkingIndicator() {
  return (
    <div className="flex items-center gap-1 py-1">
      <span className="flex gap-1">
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:0ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:150ms]" />
        <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-muted-foreground [animation-delay:300ms]" />
      </span>
    </div>
  );
}
