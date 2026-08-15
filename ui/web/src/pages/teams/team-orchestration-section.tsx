import { useTranslation } from "react-i18next";
import { Switch } from "@/components/ui/switch";
import { ShieldAlert, Clock, FolderLock, FolderSync } from "lucide-react";

interface TeamOrchestrationSectionProps {
  workspaceScope: string;
  setWorkspaceScope: (v: string) => void;
  memberRequestsEnabled: boolean;
  setMemberRequestsEnabled: (v: boolean) => void;
  memberRequestsAutoDispatch: boolean;
  setMemberRequestsAutoDispatch: (v: boolean) => void;
  blockerEscalationEnabled: boolean;
  setBlockerEscalationEnabled: (v: boolean) => void;
  followupInterval: number;
  setFollowupInterval: (v: number) => void;
  followupMaxReminders: number;
  setFollowupMaxReminders: (v: number) => void;
}

const WORKSPACE_OPTIONS = [
  { value: "isolated", Icon: FolderLock, labelKey: "workspaceScopeIsolated", descKey: "workspaceScopeIsolatedDesc" },
  { value: "shared", Icon: FolderSync, labelKey: "workspaceScopeShared", descKey: "workspaceScopeSharedDesc" },
] as const;

/** Workspace scope, member requests, blocker escalation, and follow-up reminder settings. */
export function TeamOrchestrationSection({
  workspaceScope, setWorkspaceScope,
  memberRequestsEnabled, setMemberRequestsEnabled,
  memberRequestsAutoDispatch, setMemberRequestsAutoDispatch,
  blockerEscalationEnabled, setBlockerEscalationEnabled,
  followupInterval, setFollowupInterval,
  followupMaxReminders, setFollowupMaxReminders,
}: TeamOrchestrationSectionProps) {
  const { t } = useTranslation("teams");

  return (
    <>
      {/* Workspace Scope */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.workspace")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-emerald-500/5 to-teal-500/5 p-4">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-emerald-500/10 p-2.5 text-emerald-600 dark:text-emerald-400">
              <FolderSync className="h-5 w-5" />
            </div>
            <div className="flex-1 space-y-3">
              <div className="space-y-1">
                <span className="text-sm font-semibold">{t("settings.workspaceScope")}</span>
                <p className="text-xs text-muted-foreground leading-relaxed">{t("settings.workspaceScopeHint")}</p>
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {WORKSPACE_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setWorkspaceScope(opt.value)}
                    className={
                      "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors cursor-pointer " +
                      (workspaceScope === opt.value ? "border-primary bg-primary/5" : "border-border hover:border-primary/50")
                    }
                  >
                    <opt.Icon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                    <div>
                      <div className="text-sm font-medium">{t(`settings.${opt.labelKey}`)}</div>
                      <div className="mt-0.5 text-xs text-muted-foreground">{t(`settings.${opt.descKey}`)}</div>
                    </div>
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Member Requests */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.memberRequests")}</h3>
        <div className="rounded-lg border p-4 space-y-3">
          <div className="flex items-center justify-between">
            <div>
              <span className="text-sm font-semibold">{t("settings.memberRequestsEnabled")}</span>
              <p className="text-xs text-muted-foreground">{t("settings.memberRequestsEnabledDesc")}</p>
            </div>
            <Switch checked={memberRequestsEnabled} onCheckedChange={setMemberRequestsEnabled} />
          </div>
          {memberRequestsEnabled && (
            <div className="flex items-center justify-between border-t pt-3">
              <div>
                <span className="text-sm font-semibold">{t("settings.memberRequestsAutoDispatch")}</span>
                <p className="text-xs text-muted-foreground">{t("settings.memberRequestsAutoDispatchDesc")}</p>
              </div>
              <Switch checked={memberRequestsAutoDispatch} onCheckedChange={setMemberRequestsAutoDispatch} />
            </div>
          )}
        </div>
      </div>

      {/* Blocker Escalation */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.blockerEscalation")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-orange-500/5 to-red-500/5 p-4">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-orange-500/10 p-2.5 text-orange-600 dark:text-orange-400">
              <ShieldAlert className="h-5 w-5" />
            </div>
            <div className="flex-1">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <span className="text-sm font-semibold">{t("settings.blockerEscalationEnabled")}</span>
                  <p className="text-xs text-muted-foreground leading-relaxed">{t("settings.blockerEscalationHint")}</p>
                </div>
                <Switch checked={blockerEscalationEnabled} onCheckedChange={setBlockerEscalationEnabled} />
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* Follow-up Reminders */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.followupReminders")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-amber-500/5 to-yellow-500/5 p-4">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-amber-500/10 p-2.5 text-amber-600 dark:text-amber-400">
              <Clock className="h-5 w-5" />
            </div>
            <div className="flex-1 space-y-4">
              <div className="space-y-1.5">
                <label className="text-sm font-medium">{t("settings.followupInterval")}</label>
                <p className="text-xs text-muted-foreground leading-relaxed">{t("settings.followupIntervalHint")}</p>
                <input
                  type="number" min={1} max={1440}
                  value={followupInterval}
                  onChange={(e) => setFollowupInterval(Math.max(1, parseInt(e.target.value) || 30))}
                  className="w-24 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-sm font-medium">{t("settings.followupMaxReminders")}</label>
                <p className="text-xs text-muted-foreground leading-relaxed">{t("settings.followupMaxRemindersHint")}</p>
                <input
                  type="number" min={0} max={100}
                  value={followupMaxReminders}
                  onChange={(e) => setFollowupMaxReminders(Math.max(0, parseInt(e.target.value) || 0))}
                  className="w-24 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                />
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
