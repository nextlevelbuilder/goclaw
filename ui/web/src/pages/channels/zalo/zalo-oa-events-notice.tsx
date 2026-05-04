import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, BellRing } from "lucide-react";

interface ZaloOAEventsNoticeProps {
  channelType: string;
}

// Keep in sync with the event switch in internal/channels/zalo/oa/webhook.go.
const SUPPORTED_EVENTS = [
  "user_send_text",
  "user_send_image",
  "user_send_link",
  "user_send_sticker",
  "user_send_gif",
  "user_send_file",
];

export function ZaloOAEventsNotice({ channelType }: ZaloOAEventsNoticeProps) {
  const { t } = useTranslation("channels");
  const [expanded, setExpanded] = useState(false);

  if (channelType !== "zalo_oa") return null;

  return (
    <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950 text-sm">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 w-full px-3 py-2 text-left text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900 rounded-md transition-colors"
      >
        <BellRing className="h-4 w-4 shrink-0" />
        <span className="flex-1 font-medium">{t("zaloOaEvents.title")}</span>
        {expanded
          ? <ChevronDown className="h-4 w-4 shrink-0" />
          : <ChevronRight className="h-4 w-4 shrink-0" />}
      </button>
      {expanded && (
        <div className="px-3 pb-3 space-y-2">
          <p className="text-xs text-amber-700 dark:text-amber-300">
            {t("zaloOaEvents.description")}
          </p>
          <div className="space-y-0.5">
            {SUPPORTED_EVENTS.map((evt) => (
              <div key={evt} className="flex items-baseline gap-2 text-xs font-mono">
                <code className="text-amber-900 dark:text-amber-100">{evt}</code>
              </div>
            ))}
          </div>
          <p className="text-xs text-amber-600 dark:text-amber-400 pt-1">
            {t("zaloOaEvents.location")}
          </p>
        </div>
      )}
    </div>
  );
}
