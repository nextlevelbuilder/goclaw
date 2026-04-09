import { useTranslation } from "react-i18next";
import { FileText } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import type { BootstrapFile } from "@/types/agent";

/** Special sentinel value indicating the system prompt preview is selected. */
export const PREVIEW_SENTINEL = "__preview__";

/**
 * Estimate token count from text content (Unicode-aware).
 * Splits into ASCII and non-ASCII segments for better accuracy:
 * - ASCII: ~4 chars per token (English average)
 * - Non-ASCII (Vietnamese diacritics, CJK, emoji): ~1.5 chars per token
 */
function estimateTokensFromContent(content: string): number {
  let ascii = 0;
  let nonAscii = 0;
  for (const ch of content) {
    // eslint-disable-next-line @typescript-eslint/no-non-null-assertion
    if (ch.codePointAt(0)! < 128) ascii++;
    else nonAscii++;
  }
  return Math.max(1, Math.ceil(ascii / 4 + nonAscii / 1.5));
}

/** Estimate token count from UTF-8 byte size (fallback when content is unavailable). */
function estimateTokensFromBytes(bytes: number): number {
  return Math.max(1, Math.ceil(bytes / 4));
}

function formatTokenCount(tokens: number): string {
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(1)}k`;
  return String(tokens);
}

interface FileSidebarProps {
  files: BootstrapFile[];
  selectedFile: string | null;
  onSelect: (name: string) => void;
  isUserScoped: (name: string) => boolean;
}

export function FileSidebar({
  files,
  selectedFile,
  onSelect,
  isUserScoped,
}: FileSidebarProps) {
  const { t } = useTranslation("agents");
  return (
    <div className="w-56 shrink-0 overflow-y-auto rounded-lg bg-muted/40 p-2">
      <div className="space-y-0.5">
        {files.map((file) => {
          const userScoped = isUserScoped(file.name);
          const active = selectedFile === file.name;
          return (
            <button
              key={file.name}
              type="button"
              onClick={() => onSelect(file.name)}
              className={`flex w-full items-center gap-2 rounded-md px-2.5 py-1.5 text-[13px] transition-colors ${
                active
                  ? "bg-background text-foreground shadow-sm cursor-pointer"
                  : "text-foreground hover:bg-background/60 cursor-pointer"
              }`}
            >
              <FileText className="mt-0.5 h-3.5 w-3.5 shrink-0 self-start" />
              <div className="min-w-0 flex-1 text-left">
                <div className="truncate">{file.name}</div>
                {userScoped ? (
                  <Badge variant="outline" className="mt-0.5 text-2xs">
                    {t("files.perUser")}
                  </Badge>
                ) : file.missing ? (
                  <span className="text-2xs text-muted-foreground/60">
                    {t("files.emptyFile")}
                  </span>
                ) : (
                  <div className="text-2xs text-muted-foreground/60">
                    {t("files.estTokens", { tokens: formatTokenCount(
                      file.content
                        ? estimateTokensFromContent(file.content)
                        : estimateTokensFromBytes(file.size),
                    ) })}
                  </div>
                )}
              </div>
            </button>
          );
        })}
      </div>

    </div>
  );
}
