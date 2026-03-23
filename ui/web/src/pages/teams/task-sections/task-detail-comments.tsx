import { useState } from "react";
import { MessageSquare, SendHorizontal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import { useTranslation } from "react-i18next";
import { formatDate } from "@/lib/format";
import { CollapsibleSection } from "./task-detail-content";
import type { TeamTaskComment } from "@/types/team";

/* ── Comment list + add form ──────────────────────────────────── */

interface TaskDetailCommentsProps {
  comments: TeamTaskComment[];
  onAddComment?: (content: string) => Promise<void>;
}

export function TaskDetailComments({ comments, onAddComment }: TaskDetailCommentsProps) {
  const { t } = useTranslation("teams");
  const [newComment, setNewComment] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const hasContent = comments.length > 0 || onAddComment;
  if (!hasContent) return null;

  const handleSubmit = async () => {
    if (!onAddComment || !newComment.trim()) return;
    setSubmitting(true);
    try {
      await onAddComment(newComment.trim());
      setNewComment("");
    } catch {
      /* error handled by caller */
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <CollapsibleSection
      icon={MessageSquare}
      title={t("tasks.detail.comments")}
      count={comments.length || undefined}
      defaultOpen={false}
    >
      {/* Comment list */}
      {comments.length > 0 && (
        <div className="space-y-0">
          {comments.map((c) => {
            const name = c.agent_key || (c.user_id ? "User" : "Unknown");
            const initial = (name[0] ?? "?").toUpperCase();
            return (
              <div key={c.id} className="flex gap-3 py-3 border-b border-border/50 last:border-0">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-bold text-primary">
                  {initial}
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium">{name}</span>
                    <span className="text-xs text-muted-foreground">{formatDate(c.created_at)}</span>
                  </div>
                  <MarkdownRenderer content={c.content} className="mt-0.5 text-sm" />
                </div>
              </div>
            );
          })}
        </div>
      )}

      {/* Add comment form */}
      {onAddComment && (
        <div className={comments.length > 0 ? "mt-3 border-t pt-3" : ""}>
          <div className="flex gap-2">
            <textarea
              className="min-h-[60px] flex-1 resize-none rounded-md border bg-background px-3 py-2 text-base md:text-sm placeholder:text-muted-foreground focus:outline-none focus:ring-1 focus:ring-ring"
              placeholder={t("tasks.detail.commentPlaceholder")}
              value={newComment}
              onChange={(e) => setNewComment(e.target.value)}
              disabled={submitting}
            />
            <Button
              size="sm"
              className="self-end"
              disabled={submitting || newComment.trim() === ""}
              onClick={handleSubmit}
            >
              <SendHorizontal className="mr-1.5 h-3.5 w-3.5" />
              {t("tasks.detail.addComment")}
            </Button>
          </div>
        </div>
      )}
    </CollapsibleSection>
  );
}
