import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Combobox } from "@/components/ui/combobox";
import { X, Save, Bell, ShieldAlert, Clock, FolderLock, FolderSync, Zap, Bot, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { CHANNEL_TYPES } from "@/constants/channels";
import { MultiUserPicker } from "@/components/shared/multi-user-picker";
import type { TeamData, TeamAccessSettings, TeamNotifyConfig, EscalationMode, EscalationAction } from "@/types/team";
import { useTeams } from "./hooks/use-teams";

interface TeamSettingsTabProps {
  teamId: string;
  team: TeamData;
  onSaved: () => void;
}

/** Generic multi-select with static options (used for channel type selection). */
function MultiSelect({
  options,
  selected,
  onChange,
  placeholder,
}: {
  options: { value: string; label?: string }[];
  selected: string[];
  onChange: (values: string[]) => void;
  placeholder: string;
}) {
  return (
    <div className="space-y-2">
      <Combobox
        value=""
        onChange={(val) => {
          if (val && !selected.includes(val)) {
            onChange([...selected, val]);
          }
        }}
        options={options.filter((o) => !selected.includes(o.value))}
        placeholder={placeholder}
      />
      {selected.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {selected.map((id) => (
            <Badge key={id} variant="secondary" className="gap-1 pr-1">
              {options.find((o) => o.value === id)?.label ?? id}
              <button
                type="button"
                onClick={() => onChange(selected.filter((s) => s !== id))}
                className="ml-0.5 cursor-pointer rounded-full p-0.5 hover:bg-muted"
              >
                <X className="h-3 w-3" />
              </button>
            </Badge>
          ))}
        </div>
      )}
    </div>
  );
}

export function TeamSettingsTab({ teamId, team, onSaved }: TeamSettingsTabProps) {
  const { t } = useTranslation("teams");
  const { updateTeamSettings } = useTeams();

  // Parse initial settings
  const initial = (team.settings ?? {}) as TeamAccessSettings;
  const [allowUserIds, setAllowUserIds] = useState<string[]>(initial.allow_user_ids ?? []);
  const [denyUserIds, setDenyUserIds] = useState<string[]>(initial.deny_user_ids ?? []);
  const [allowChannels, setAllowChannels] = useState<string[]>(initial.allow_channels ?? []);
  const [denyChannels, setDenyChannels] = useState<string[]>(initial.deny_channels ?? []);
  const initNotify = initial.notifications ?? {};
  const [notifyDispatched, setNotifyDispatched] = useState(initNotify.dispatched ?? true);
  const [notifyProgress, setNotifyProgress] = useState(initNotify.progress ?? false);
  const [notifyFailed, setNotifyFailed] = useState(initNotify.failed ?? false);
  const [notifyCompleted, setNotifyCompleted] = useState(initNotify.completed ?? true);
  const [notifyCommented, setNotifyCommented] = useState(initNotify.commented ?? false);
  const [notifyNewTask, setNotifyNewTask] = useState(initNotify.new_task ?? true);
  const [notifySlowTool, setNotifySlowTool] = useState(initNotify.slow_tool ?? false);
  const [notifyMode, setNotifyMode] = useState<"direct" | "leader">(initNotify.mode ?? "direct");
  const initMemberRequests = initial.member_requests ?? {};
  const [memberRequestsEnabled, setMemberRequestsEnabled] = useState(initMemberRequests.enabled ?? false);
  const [memberRequestsAutoDispatch, setMemberRequestsAutoDispatch] = useState(initMemberRequests.auto_dispatch ?? false);
  const [escalationMode, setEscalationMode] = useState<EscalationMode | "">(initial.escalation_mode ?? "");
  const [escalationActions, setEscalationActions] = useState<EscalationAction[]>(initial.escalation_actions ?? []);
  const initBlockerEscalation = initial.blocker_escalation ?? {};
  const [blockerEscalationEnabled, setBlockerEscalationEnabled] = useState(initBlockerEscalation.enabled ?? true);
  const [followupInterval, setFollowupInterval] = useState<number>(initial.followup_interval_minutes ?? 30);
  const [followupMaxReminders, setFollowupMaxReminders] = useState<number>(initial.followup_max_reminders ?? 0);
  const [workspaceScope, setWorkspaceScope] = useState<string>(initial.workspace_scope ?? "isolated");

  const [saving, setSaving] = useState(false);

  // Reset when team changes
  useEffect(() => {
    const s = (team.settings ?? {}) as TeamAccessSettings;
    setAllowUserIds(s.allow_user_ids ?? []);
    setDenyUserIds(s.deny_user_ids ?? []);
    setAllowChannels(s.allow_channels ?? []);
    setDenyChannels(s.deny_channels ?? []);
    const sn = s.notifications ?? {};
    setNotifyDispatched(sn.dispatched ?? true);
    setNotifyProgress(sn.progress ?? false);
    setNotifyFailed(sn.failed ?? false);
    setNotifyCompleted(sn.completed ?? true);
    setNotifyCommented(sn.commented ?? false);
    setNotifyNewTask(sn.new_task ?? true);
    setNotifySlowTool(sn.slow_tool ?? false);
    setNotifyMode(sn.mode ?? "direct");
    const smr = s.member_requests ?? {};
    setMemberRequestsEnabled(smr.enabled ?? false);
    setMemberRequestsAutoDispatch(smr.auto_dispatch ?? false);
    setEscalationMode(s.escalation_mode ?? "");
    setEscalationActions(s.escalation_actions ?? []);
    const sbe = s.blocker_escalation ?? {};
    setBlockerEscalationEnabled(sbe.enabled ?? true);
    setFollowupInterval(s.followup_interval_minutes ?? 30);
    setFollowupMaxReminders(s.followup_max_reminders ?? 0);
    setWorkspaceScope(s.workspace_scope ?? "isolated");
  }, [team]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const settings: TeamAccessSettings = {};
      if (allowUserIds.length > 0) settings.allow_user_ids = allowUserIds;
      if (denyUserIds.length > 0) settings.deny_user_ids = denyUserIds;
      if (allowChannels.length > 0) settings.allow_channels = allowChannels;
      if (denyChannels.length > 0) settings.deny_channels = denyChannels;
      const notifications: TeamNotifyConfig = {
        dispatched: notifyDispatched,
        progress: notifyProgress,
        failed: notifyFailed,
        slow_tool: notifySlowTool,
        mode: notifyMode,
      };
      notifications.completed = notifyCompleted;
      notifications.commented = notifyCommented;
      notifications.new_task = notifyNewTask;
      settings.notifications = notifications;
      if (memberRequestsEnabled) {
        settings.member_requests = {
          enabled: true,
          auto_dispatch: memberRequestsAutoDispatch,
        };
      }
      if (escalationMode) {
        settings.escalation_mode = escalationMode;
        if (escalationActions.length > 0) settings.escalation_actions = escalationActions;
      }
      settings.blocker_escalation = { enabled: blockerEscalationEnabled };
      if (followupInterval !== 30) settings.followup_interval_minutes = followupInterval;
      if (followupMaxReminders !== 0) settings.followup_max_reminders = followupMaxReminders;
      settings.workspace_scope = workspaceScope || "isolated";
      await updateTeamSettings(teamId, settings);
      onSaved();
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  }, [teamId, allowUserIds, denyUserIds, allowChannels, denyChannels, notifyDispatched, notifyProgress, notifyFailed, notifyCompleted, notifyCommented, notifyNewTask, notifySlowTool, notifyMode, memberRequestsEnabled, memberRequestsAutoDispatch, escalationMode, escalationActions, blockerEscalationEnabled, followupInterval, followupMaxReminders, workspaceScope, updateTeamSettings, onSaved]);

  const channelOptions = CHANNEL_TYPES.map((c) => ({ value: c.value, label: c.label }));

  return (
    <div className="space-y-6">
      {/* Notifications */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.notifications")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-blue-500/5 to-orange-500/5 p-4 space-y-3">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-blue-500/10 p-2.5 text-blue-600 dark:text-blue-400">
              <Bell className="h-5 w-5" />
            </div>
            <div className="flex-1 space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyDispatched")}</span>
                  <p className="text-xs text-muted-foreground">{t("settings.notifyDispatchedHint")}</p>
                </div>
                <Switch checked={notifyDispatched} onCheckedChange={setNotifyDispatched} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyProgress")}</span>
                  <p className="text-xs text-muted-foreground">{t("settings.notifyProgressHint")}</p>
                </div>
                <Switch checked={notifyProgress} onCheckedChange={setNotifyProgress} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyFailed")}</span>
                  <p className="text-xs text-muted-foreground">{t("settings.notifyFailedHint")}</p>
                </div>
                <Switch checked={notifyFailed} onCheckedChange={setNotifyFailed} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyCompleted")}</span>
                </div>
                <Switch checked={notifyCompleted} onCheckedChange={setNotifyCompleted} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyCommented")}</span>
                </div>
                <Switch checked={notifyCommented} onCheckedChange={setNotifyCommented} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifyNewTask")}</span>
                </div>
                <Switch checked={notifyNewTask} onCheckedChange={setNotifyNewTask} />
              </div>
              <div className="flex items-center justify-between">
                <div>
                  <span className="text-sm font-semibold">{t("settings.notifySlowTool")}</span>
                  <p className="text-xs text-muted-foreground">{t("settings.notifySlowToolHint")}</p>
                </div>
                <Switch checked={notifySlowTool} onCheckedChange={setNotifySlowTool} />
              </div>
              <div className="border-t pt-3 space-y-2">
                <span className="text-sm font-semibold">{t("settings.notifyMode")}</span>
                <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                  {([
                    { value: "direct" as const, Icon: Zap, labelKey: "notifyModeDirect", descKey: "notifyModeDirectDesc" },
                    { value: "leader" as const, Icon: Bot, labelKey: "notifyModeLeader", descKey: "notifyModeLeaderDesc" },
                  ]).map((opt) => (
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
                <p className="text-xs text-muted-foreground leading-relaxed">
                  {t("settings.workspaceScopeHint")}
                </p>
              </div>
              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {([
                  { value: "isolated", Icon: FolderLock, labelKey: "workspaceScopeIsolated", descKey: "workspaceScopeIsolatedDesc" },
                  { value: "shared", Icon: FolderSync, labelKey: "workspaceScopeShared", descKey: "workspaceScopeSharedDesc" },
                ] as const).map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setWorkspaceScope(opt.value)}
                    className={
                      "flex items-start gap-3 rounded-lg border p-3 text-left transition-colors cursor-pointer " +
                      (workspaceScope === opt.value
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
            <div className="flex-1 space-y-3">
              <div className="flex items-center justify-between">
                <div className="space-y-1">
                  <span className="text-sm font-semibold">{t("settings.blockerEscalationEnabled")}</span>
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    {t("settings.blockerEscalationHint")}
                  </p>
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
                <p className="text-xs text-muted-foreground leading-relaxed">
                  {t("settings.followupIntervalHint")}
                </p>
                <input
                  type="number"
                  min={1}
                  max={1440}
                  value={followupInterval}
                  onChange={(e) => setFollowupInterval(Math.max(1, parseInt(e.target.value) || 30))}
                  className="w-24 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-sm font-medium">{t("settings.followupMaxReminders")}</label>
                <p className="text-xs text-muted-foreground leading-relaxed">
                  {t("settings.followupMaxRemindersHint")}
                </p>
                <input
                  type="number"
                  min={0}
                  max={100}
                  value={followupMaxReminders}
                  onChange={(e) => setFollowupMaxReminders(Math.max(0, parseInt(e.target.value) || 0))}
                  className="w-24 rounded-md border bg-background px-3 py-1.5 text-base md:text-sm"
                />
              </div>
            </div>
          </div>
        </div>
      </div>

      {/* User Access Control */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.userAccessControl")}</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.allowedUsers")}</label>
            <p className="text-xs text-muted-foreground">
              {t("settings.allowedUsersHint")}
            </p>
            <MultiUserPicker
              value={allowUserIds}
              onChange={setAllowUserIds}
              placeholder={t("settings.searchUsers")}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.deniedUsers")}</label>
            <p className="text-xs text-muted-foreground">
              {t("settings.deniedUsersHint")}
            </p>
            <MultiUserPicker
              value={denyUserIds}
              onChange={setDenyUserIds}
              placeholder={t("settings.searchUsers")}
            />
          </div>
        </div>
      </div>

      {/* Channel Restrictions */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.channelRestrictions")}</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.allowedChannels")}</label>
            <p className="text-xs text-muted-foreground">
              {t("settings.allowedChannelsHint")}
            </p>
            <MultiSelect
              options={channelOptions}
              selected={allowChannels}
              onChange={setAllowChannels}
              placeholder={t("settings.selectChannel")}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.deniedChannels")}</label>
            <p className="text-xs text-muted-foreground">
              {t("settings.deniedChannelsHint")}
            </p>
            <MultiSelect
              options={channelOptions}
              selected={denyChannels}
              onChange={setDenyChannels}
              placeholder={t("settings.selectChannel")}
            />
          </div>
        </div>
      </div>

      {/* Save button */}
      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={saving} className="gap-2">
          {saving ? <Loader2 className="h-4 w-4 animate-spin" /> : <Save className="h-4 w-4" />}
          {saving ? t("settings.saving") : t("settings.save")}
        </Button>
      </div>

    </div>
  );
}
