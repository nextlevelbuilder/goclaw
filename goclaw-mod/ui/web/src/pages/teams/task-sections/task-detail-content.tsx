import { useState } from "react";
import { ChevronDown, ChevronRight, FileText, CheckCircle2 } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useTranslation } from "react-i18next";
import type { LucideIcon } from "lucide-react";

/* ── Reusable collapsible wrapper ─────────────────────────────── */

interface CollapsibleSectionProps {
  icon: LucideIcon;
  title: string;
  count?: number;
  defaultOpen?: boolean;
  children: React.ReactNode;
}

export function CollapsibleSection({
  icon: Icon, title, count, defaultOpen = true, children,
}: CollapsibleSectionProps) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <div className="rounded-lg border">
      <button
        type="button"
        className="flex w-full items-center gap-2 px-4 py-3 min-h-[44px] text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
        onClick={() => setOpen((v) => !v)}
      >
        <Icon className="h-4 w-4 shrink-0" />
        <span>{title}</span>
        {count != null && (
          <Badge variant="secondary" className="ml-1 text-2xs">{count}</Badge>
        )}
        {open
          ? <ChevronDown className="ml-auto h-4 w-4" />
          : <ChevronRight className="ml-auto h-4 w-4" />}
      </button>
      {open && <div className="border-t px-4 py-3">{children}</div>}
    </div>
  );
}

/* ── Description + Result sections ────────────────────────────── */

interface TaskDetailContentProps {
  description?: string;
  result?: string;
}

export function TaskDetailContent({ description, result }: TaskDetailContentProps) {
  const { t } = useTranslation("teams");

  if (!description && !result) return null;

  return (
    <>
      {description && (
        <CollapsibleSection icon={FileText} title={t("tasks.detail.description")}>
          <MarkdownRenderer content={description} className="text-sm max-h-60 overflow-y-auto" />
        </CollapsibleSection>
      )}
      {result && (
        <CollapsibleSection icon={CheckCircle2} title={t("tasks.detail.result")}>
          <MarkdownRenderer content={result} className="text-sm max-h-[40vh] overflow-y-auto" />
        </CollapsibleSection>
      )}
    </>
  );
}
