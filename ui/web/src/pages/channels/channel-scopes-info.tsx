import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDown, ChevronRight, Shield } from "lucide-react";
import { requiredScopes } from "./channel-schemas";

interface ChannelScopesInfoProps {
  channelType: string;
}

export function ChannelScopesInfo({ channelType }: ChannelScopesInfoProps) {
  const { t } = useTranslation("channels");
  const scopes = requiredScopes[channelType];
  const [expanded, setExpanded] = useState(false);

  if (!scopes || scopes.length === 0) return null;

  return (
    <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950 text-sm">
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 w-full px-3 py-2 text-left text-amber-800 dark:text-amber-200 hover:bg-amber-100 dark:hover:bg-amber-900 rounded-md transition-colors"
      >
        <Shield className="h-4 w-4 shrink-0" />
        <span className="flex-1 font-medium">{t("scopes.title")}</span>
        {expanded
          ? <ChevronDown className="h-4 w-4 shrink-0" />
          : <ChevronRight className="h-4 w-4 shrink-0" />}
      </button>
      {expanded && (
        <div className="px-3 pb-3 space-y-2">
          <p className="text-xs text-amber-700 dark:text-amber-300">
            {t("scopes.description")}
          </p>
          <div className="space-y-0.5">
            {scopes.map((s) => (
              <div key={s.scope} className="flex items-baseline gap-2 text-xs font-mono">
                <code className="text-amber-900 dark:text-amber-100">{s.scope}</code>
                {s.note && (
                  <span className="text-amber-600 dark:text-amber-400 font-sans text-[11px]">
                    — {s.note}
                  </span>
                )}
              </div>
            ))}
          </div>
          <p className="text-xs text-amber-600 dark:text-amber-400 pt-1">
            {t("scopes.publishReminder")}
          </p>
        </div>
      )}
    </div>
  );
}
