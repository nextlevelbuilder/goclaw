/**
 * WikilinkPill — renders a [[wikilink]] as a styled inline pill.
 * Clickable if onWikilinkClick is provided, otherwise styled text only.
 */
import { Link2 } from "lucide-react";

interface WikilinkProps {
  target: string;
  onClick?: (target: string) => void;
}

export function WikilinkPill({ target, onClick }: WikilinkProps) {
  if (onClick) {
    return (
      <button
        type="button"
        onClick={() => onClick(target)}
        className="inline-flex cursor-pointer items-center gap-1 rounded-md bg-primary/10 px-1.5 py-0.5 text-[0.85em] font-medium text-primary hover:bg-primary/20 transition-colors"
      >
        <Link2 className="h-3 w-3 shrink-0" />
        {target}
      </button>
    );
  }

  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-primary/10 px-1.5 py-0.5 text-[0.85em] font-medium text-primary">
      <Link2 className="h-3 w-3 shrink-0" />
      {target}
    </span>
  );
}
