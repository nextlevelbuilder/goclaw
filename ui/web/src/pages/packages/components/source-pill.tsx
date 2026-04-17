import { cn } from "@/lib/utils";

interface Props {
  source: "github" | "pip" | "npm" | string;
}

const SOURCE_CLASSES: Record<string, string> = {
  github:
    "bg-slate-100 text-slate-900 dark:bg-slate-800 dark:text-slate-100",
  pip: "bg-blue-100 text-blue-900 dark:bg-blue-900/40 dark:text-blue-200",
  npm: "bg-amber-100 text-amber-900 dark:bg-amber-900/40 dark:text-amber-200",
};

const NEUTRAL =
  "bg-muted text-muted-foreground";

/**
 * Small colored pill indicating a package source (github / pip / npm / other).
 */
export function SourcePill({ source }: Props) {
  const classes = SOURCE_CLASSES[source] ?? NEUTRAL;
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium",
        classes,
      )}
    >
      {source}
    </span>
  );
}
