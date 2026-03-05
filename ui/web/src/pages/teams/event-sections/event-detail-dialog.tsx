import { Copy, Check } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { useClipboard } from "@/hooks/use-clipboard";
import type { TeamEventEntry } from "@/stores/use-team-event-store";

interface EventDetailDialogProps {
  entry: TeamEventEntry;
  onClose: () => void;
}

export function EventDetailDialog({ entry, onClose }: EventDetailDialogProps) {
  const json = JSON.stringify(entry.payload, null, 2);
  const { copied, copy } = useClipboard();

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Badge variant="secondary" className="font-mono text-xs">
              {entry.event}
            </Badge>
            <span className="text-sm font-normal text-muted-foreground">
              {new Date(entry.timestamp).toLocaleString()}
            </span>
          </DialogTitle>
        </DialogHeader>

        <div className="relative">
          <Button
            variant="ghost"
            size="sm"
            className="absolute top-2 right-2 z-10 h-7 gap-1 text-xs"
            onClick={() => copy(json)}
          >
            {copied ? (
              <Check className="h-3 w-3 text-green-500" />
            ) : (
              <Copy className="h-3 w-3" />
            )}
            {copied ? "Copied" : "Copy"}
          </Button>
          <pre className="max-h-[60vh] overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-4 text-sm leading-relaxed">
            <code>
              <JsonHighlight json={json} />
            </code>
          </pre>
        </div>

        <DialogFooter showCloseButton />
      </DialogContent>
    </Dialog>
  );
}

/** Simple JSON syntax highlighter — no external deps. */
function JsonHighlight({ json }: { json: string }) {
  const parts = json.split(
    /("(?:\\.|[^"\\])*")\s*(:)?|(\btrue\b|\bfalse\b|\bnull\b)|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
  );

  return (
    <>
      {parts.map((part, i) => {
        if (part === undefined || part === "") return null;
        // String key (followed by colon in next capture group)
        if (part.startsWith('"') && parts[i + 1] === ":") {
          return (
            <span key={i} className="text-sky-600 dark:text-sky-400">
              {part}
            </span>
          );
        }
        // Colon separator
        if (part === ":") {
          return (
            <span key={i} className="text-muted-foreground">
              {part}
            </span>
          );
        }
        // String value
        if (part.startsWith('"')) {
          return (
            <span key={i} className="text-emerald-600 dark:text-emerald-400">
              {part}
            </span>
          );
        }
        // Boolean / null
        if (part === "true" || part === "false" || part === "null") {
          return (
            <span key={i} className="text-amber-600 dark:text-amber-400">
              {part}
            </span>
          );
        }
        // Number
        if (/^-?\d/.test(part)) {
          return (
            <span key={i} className="text-violet-600 dark:text-violet-400">
              {part}
            </span>
          );
        }
        // Punctuation / whitespace
        return <span key={i}>{part}</span>;
      })}
    </>
  );
}
