import { useTranslation } from "react-i18next";
import type { ChannelStatusEntry } from "./types";
import { formatRelativeTime, getChannelStatusMeta } from "@/pages/channels/channels-status-utils";

interface ChannelAttentionPanelProps {
  attentionEntries: [string, ChannelStatusEntry][];
  previewLimit?: number;
}

export function ChannelAttentionPanel({
  attentionEntries,
  previewLimit = 2,
}: ChannelAttentionPanelProps) {
  const { t } = useTranslation("overview");
  const attentionPreview = attentionEntries.slice(0, previewLimit);
  const hiddenAttentionCount = Math.max(0, attentionEntries.length - attentionPreview.length);

  if (attentionPreview.length === 0) return null;

  return (
    <div className="mt-3 rounded-lg border border-amber-200/70 bg-amber-500/[0.05] p-3 dark:border-amber-500/20 dark:bg-amber-500/10">
      <div className="mb-2 flex items-center justify-between gap-3">
        <p className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          {t("systemHealth.needsAttention", {
            defaultValue: "Needs attention",
          })}
        </p>
        <span className="text-xs text-muted-foreground">
          {t("systemHealth.channelsNeedingAttention", {
            defaultValue: "{{count}} channels",
            count: attentionEntries.length,
          })}
        </span>
      </div>
      <div className="space-y-2">
        {attentionPreview.map(([name, ch]) => {
          const meta = getChannelStatusMeta(ch, ch.enabled, t);
          const checked = formatRelativeTime(ch.checked_at);
          return (
            <div
              key={name}
              className="flex items-start gap-2 text-sm"
            >
              <span
                className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${meta.dotClass}`}
              />
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{name}</span>
                  <span className="text-xs text-muted-foreground">
                    {meta.label}
                  </span>
                  {checked && (
                    <span className="text-xs text-muted-foreground">
                      {checked}
                    </span>
                  )}
                </div>
                <p className="truncate text-xs text-muted-foreground">
                  {ch.summary || meta.label}
                </p>
              </div>
            </div>
          );
        })}
        {hiddenAttentionCount > 0 && (
          <p className="text-xs text-muted-foreground">
            {t("systemHealth.moreAttention", {
              defaultValue: "+{{count}} more",
              count: hiddenAttentionCount,
            })}
          </p>
        )}
      </div>
    </div>
  );
}
