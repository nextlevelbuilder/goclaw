import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Save, Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { CHANNEL_TYPES } from "@/constants/channels";
import type { TeamData, TeamAccessSettings, TeamNotifyConfig, EscalationMode, EscalationAction } from "@/types/team";
import { useTeams } from "./hooks/use-teams";
import { TeamNotificationsSection } from "./team-notifications-section";
import { TeamAccessControlSection } from "./team-access-control-section";
import { TeamOrchestrationSection } from "./team-orchestration-section";
import { teamSettingsSchema, type TeamSettingsFormData } from "@/schemas/team-settings.schema";

interface TeamSettingsTabProps {
  teamId: string;
  team: TeamData;
  onSaved: () => void;
}

function deriveDefaults(team: TeamData): TeamSettingsFormData {
  const s = (team.settings ?? {}) as TeamAccessSettings;
  const sn = s.notifications ?? {};
  const smr = s.member_requests ?? {};
  const sbe = s.blocker_escalation ?? {};
  return {
    notifyDispatched: sn.dispatched ?? true,
    notifyProgress: sn.progress ?? false,
    notifyFailed: sn.failed ?? false,
    notifyCompleted: sn.completed ?? true,
    notifyCommented: sn.commented ?? false,
    notifyNewTask: sn.new_task ?? true,
    notifySlowTool: sn.slow_tool ?? false,
    notifyMode: sn.mode ?? "direct",
    workspaceScope: s.workspace_scope ?? "isolated",
    memberRequestsEnabled: smr.enabled ?? false,
    memberRequestsAutoDispatch: smr.auto_dispatch ?? false,
    blockerEscalationEnabled: sbe.enabled ?? true,
    followupInterval: s.followup_interval_minutes ?? 30,
    followupMaxReminders: s.followup_max_reminders ?? 0,
    allowUserIds: s.allow_user_ids ?? [],
    denyUserIds: s.deny_user_ids ?? [],
    allowChannels: s.allow_channels ?? [],
    denyChannels: s.deny_channels ?? [],
  };
}

export function TeamSettingsTab({ teamId, team, onSaved }: TeamSettingsTabProps) {
  const { t } = useTranslation("teams");
  const { updateTeamSettings } = useTeams();

  // UI-only state
  const [saving, setSaving] = useState(false);

  const form = useForm<TeamSettingsFormData>({
    resolver: zodResolver(teamSettingsSchema),
    mode: "onChange",
    defaultValues: deriveDefaults(team),
  });

  const { watch, setValue, reset } = form;

  // Sync when team prop changes (e.g. after refetch)
  useEffect(() => {
    reset(deriveDefaults(team));
  }, [team, reset]);

  const notifyDispatched = watch("notifyDispatched");
  const notifyProgress = watch("notifyProgress");
  const notifyFailed = watch("notifyFailed");
  const notifyCompleted = watch("notifyCompleted");
  const notifyCommented = watch("notifyCommented");
  const notifyNewTask = watch("notifyNewTask");
  const notifySlowTool = watch("notifySlowTool");
  const notifyMode = watch("notifyMode");
  const workspaceScope = watch("workspaceScope");
  const memberRequestsEnabled = watch("memberRequestsEnabled");
  const memberRequestsAutoDispatch = watch("memberRequestsAutoDispatch");
  const blockerEscalationEnabled = watch("blockerEscalationEnabled");
  const followupInterval = watch("followupInterval");
  const followupMaxReminders = watch("followupMaxReminders");
  const allowUserIds = watch("allowUserIds");
  const denyUserIds = watch("denyUserIds");
  const allowChannels = watch("allowChannels");
  const denyChannels = watch("denyChannels");

  // escalationMode / escalationActions remain uncontrolled (not in schema) — read from team settings directly
  const initial = (team.settings ?? {}) as TeamAccessSettings;
  const escalationMode: EscalationMode | "" = initial.escalation_mode ?? "";
  const escalationActions: EscalationAction[] = initial.escalation_actions ?? [];

  const handleSave = useCallback(async () => {
    setSaving(true);
    try {
      const data = form.getValues();
      const settings: TeamAccessSettings = {};
      // Preserve version from existing settings to prevent v2→v1 downgrade
      if (initial.version !== undefined) settings.version = initial.version;
      if (data.allowUserIds.length > 0) settings.allow_user_ids = data.allowUserIds;
      if (data.denyUserIds.length > 0) settings.deny_user_ids = data.denyUserIds;
      if (data.allowChannels.length > 0) settings.allow_channels = data.allowChannels;
      if (data.denyChannels.length > 0) settings.deny_channels = data.denyChannels;
      const notifications: TeamNotifyConfig = {
        dispatched: data.notifyDispatched,
        progress: data.notifyProgress,
        failed: data.notifyFailed,
        slow_tool: data.notifySlowTool,
        mode: data.notifyMode,
        completed: data.notifyCompleted,
        commented: data.notifyCommented,
        new_task: data.notifyNewTask,
      };
      settings.notifications = notifications;
      settings.member_requests = { enabled: data.memberRequestsEnabled, auto_dispatch: data.memberRequestsAutoDispatch };
      if (escalationMode) {
        settings.escalation_mode = escalationMode;
        if (escalationActions.length > 0) settings.escalation_actions = escalationActions;
      }
      settings.blocker_escalation = { enabled: data.blockerEscalationEnabled };
      settings.followup_interval_minutes = data.followupInterval;
      settings.followup_max_reminders = data.followupMaxReminders;
      settings.workspace_scope = data.workspaceScope || "isolated";
      await updateTeamSettings(teamId, settings);
      onSaved();
    } catch { // toast shown by hook
    } finally {
      setSaving(false);
    }
  }, [teamId, form, initial, escalationMode, escalationActions, updateTeamSettings, onSaved]);

  const channelOptions = CHANNEL_TYPES.map((c) => ({ value: c.value, label: c.label }));

  return (
    <div className="space-y-6">
      <TeamNotificationsSection
        notifyDispatched={notifyDispatched} setNotifyDispatched={(v) => setValue("notifyDispatched", v)}
        notifyProgress={notifyProgress} setNotifyProgress={(v) => setValue("notifyProgress", v)}
        notifyFailed={notifyFailed} setNotifyFailed={(v) => setValue("notifyFailed", v)}
        notifyCompleted={notifyCompleted} setNotifyCompleted={(v) => setValue("notifyCompleted", v)}
        notifyCommented={notifyCommented} setNotifyCommented={(v) => setValue("notifyCommented", v)}
        notifyNewTask={notifyNewTask} setNotifyNewTask={(v) => setValue("notifyNewTask", v)}
        notifySlowTool={notifySlowTool} setNotifySlowTool={(v) => setValue("notifySlowTool", v)}
        notifyMode={notifyMode} setNotifyMode={(v) => setValue("notifyMode", v)}
      />

      <TeamOrchestrationSection
        workspaceScope={workspaceScope} setWorkspaceScope={(v) => setValue("workspaceScope", v)}
        memberRequestsEnabled={memberRequestsEnabled} setMemberRequestsEnabled={(v) => setValue("memberRequestsEnabled", v)}
        memberRequestsAutoDispatch={memberRequestsAutoDispatch} setMemberRequestsAutoDispatch={(v) => setValue("memberRequestsAutoDispatch", v)}
        blockerEscalationEnabled={blockerEscalationEnabled} setBlockerEscalationEnabled={(v) => setValue("blockerEscalationEnabled", v)}
        followupInterval={followupInterval} setFollowupInterval={(v) => setValue("followupInterval", v)}
        followupMaxReminders={followupMaxReminders} setFollowupMaxReminders={(v) => setValue("followupMaxReminders", v)}
      />

      <TeamAccessControlSection
        allowUserIds={allowUserIds} setAllowUserIds={(v) => setValue("allowUserIds", v)}
        denyUserIds={denyUserIds} setDenyUserIds={(v) => setValue("denyUserIds", v)}
        allowChannels={allowChannels} setAllowChannels={(v) => setValue("allowChannels", v)}
        denyChannels={denyChannels} setDenyChannels={(v) => setValue("denyChannels", v)}
        channelOptions={channelOptions}
      />

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
