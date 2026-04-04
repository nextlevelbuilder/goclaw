import { Badge } from "@/components/ui/badge";

interface StateGroupEntry {
  name: string;
  label: string;
  detail?: string;
}

interface StateGroupProps {
  title: string;
  count: number;
  variant: "success" | "warning" | "outline" | "destructive";
  entries: StateGroupEntry[];
  emptyLabel: string;
}

export function StateGroup({
  title,
  count,
  variant,
  entries,
  emptyLabel,
}: StateGroupProps) {
  return (
    <div className="self-start rounded-lg border bg-muted/10 px-2.5 py-2 [@media(max-height:760px)]:px-2 [@media(max-height:760px)]:py-1.5">
      <div className="flex items-center justify-between gap-2">
        <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {title}
        </p>
        <Badge variant={variant}>{count}</Badge>
      </div>
      {entries.length > 0 ? (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {entries.map((entry) => (
            <div
              key={entry.name}
              className="rounded-md border bg-background/80 px-2 py-1 text-xs"
            >
              <span className="font-medium">{entry.label}</span>
              {entry.detail ? (
                <span className="text-muted-foreground"> · {entry.detail}</span>
              ) : null}
            </div>
          ))}
        </div>
      ) : (
        <p className="mt-2 text-xs text-muted-foreground">{emptyLabel}</p>
      )}
    </div>
  );
}
