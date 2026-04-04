import { Switch } from "@/components/ui/switch";
import { Bell, Zap, Bot } from "lucide-react";
import { useTranslation } from "react-i18next";

interface TeamNotificationsSectionProps {
  notifyDispatched: boolean;
  setNotifyDispatched: (v: boolean) => void;
  notifyProgress: boolean;
  setNotifyProgress: (v: boolean) => void;
  notifyFailed: boolean;
  setNotifyFailed: (v: boolean) => void;
  notifyCompleted: boolean;
  setNotifyCompleted: (v: boolean) => void;
  notifyCommented: boolean;
  setNotifyCommented: (v: boolean) => void;
  notifyNewTask: boolean;
  setNotifyNewTask: (v: boolean) => void;
  notifySlowTool: boolean;
  setNotifySlowTool: (v: boolean) => void;
  notifyMode: "direct" | "leader";
  setNotifyMode: (v: "direct" | "leader") => void;
}

/** Notification toggles and delivery mode selector for team settings. */
export function TeamNotificationsSection({
  notifyDispatched, setNotifyDispatched,
  notifyProgress, setNotifyProgress,
  notifyFailed, setNotifyFailed,
  notifyCompleted, setNotifyCompleted,
  notifyCommented, setNotifyCommented,
  notifyNewTask, setNotifyNewTask,
  notifySlowTool, setNotifySlowTool,
  notifyMode, setNotifyMode,
}: TeamNotificationsSectionProps) {
  const { t } = useTranslation("teams");

  const modeOptions = [
    { value: "direct" as const, Icon: Zap, labelKey: "notifyModeDirect", descKey: "notifyModeDirectDesc" },
    { value: "leader" as const, Icon: Bot, labelKey: "notifyModeLeader", descKey: "notifyModeLeaderDesc" },
  ];

  return (
    <div className="space-y-4">
      <h3 className="text-sm font-medium">{t("settings.notifications")}</h3>
      <div className="rounded-lg border bg-gradient-to-r from-blue-500/5 to-orange-500/5 p-4 space-y-3">
        <div className="flex items-start gap-4">
          <div className="rounded-lg bg-blue-500/10 p-2.5 text-blue-600 dark:text-blue-400">
            <Bell className="h-5 w-5" />
          </div>
          <div className="flex-1 space-y-3">
            {[
              { label: t("settings.notifyDispatched"), hint: t("settings.notifyDispatchedHint"), checked: notifyDispatched, onChange: setNotifyDispatched },
              { label: t("settings.notifyProgress"), hint: t("settings.notifyProgressHint"), checked: notifyProgress, onChange: setNotifyProgress },
              { label: t("settings.notifyFailed"), hint: t("settings.notifyFailedHint"), checked: notifyFailed, onChange: setNotifyFailed },
              { label: t("settings.notifyCompleted"), hint: undefined, checked: notifyCompleted, onChange: setNotifyCompleted },
              { label: t("settings.notifyCommented"), hint: undefined, checked: notifyCommented, onChange: setNotifyCommented },
              { label: t("settings.notifyNewTask"), hint: undefined, checked: notifyNewTask, onChange: setNotifyNewTask },
              { label: t("settings.notifySlowTool"), hint: t("settings.notifySlowToolHint"), checked: notifySlowTool, onChange: setNotifySlowTool },
            ].map(({ label, hint, checked, onChange }) => (
              <div key={label} className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{label}</span>
                  {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
                </div>
                <Switch checked={checked} onCheckedChange={onChange} />
              </div>
            ))}

            <div className="border-t pt-3 space-y-2">
              <span className="text-sm font-semibold">{t("settings.notifyMode")}</span>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {modeOptions.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setNotifyMode(opt.value)}
                    className={
                      "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors cursor-pointer " +
                      (notifyMode === opt.value
                        ? "border-primary bg-primary/5"
                        : "border-border hover:border-primary/50")
                    }
                  >
                    <opt.Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                    <div>
                      <div className="text-sm font-medium">{t(`settings.${opt.labelKey}`)}</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">
                        {t(`settings.${opt.descKey}`)}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
              {notifyMode === "leader" && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  ⚠️ {t("settings.notifyModeLeaderWarning")}
                </p>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
