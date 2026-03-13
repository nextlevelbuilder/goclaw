import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Switch } from "@/components/ui/switch";
import { Combobox } from "@/components/ui/combobox";
import { X, Save, Check, Bell, ShieldAlert, Clock, Info } from "lucide-react";
import { useTranslation } from "react-i18next";
import { CHANNEL_TYPES } from "@/constants/channels";
import type { TeamData, TeamAccessSettings, EscalationMode, EscalationAction } from "@/types/team";
import { ESCALATION_ACTIONS } from "@/types/team";
import { useTeams } from "./hooks/use-teams";
import { TeamVersionModal } from "./team-version-modal";

interface TeamSettingsTabProps {
  teamId: string;
  team: TeamData;
  onSaved: () => void;
}

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
                className="ml-0.5 rounded-full p-0.5 hover:bg-muted"
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
  const { updateTeamSettings, getKnownUsers } = useTeams();
  const [knownUsers, setKnownUsers] = useState<string[]>([]);

  // Parse initial settings
  const initial = (team.settings ?? {}) as TeamAccessSettings;
  const [version, setVersion] = useState<number>(initial.version ?? 1);
  const [allowUserIds, setAllowUserIds] = useState<string[]>(initial.allow_user_ids ?? []);
  const [denyUserIds, setDenyUserIds] = useState<string[]>(initial.deny_user_ids ?? []);
  const [allowChannels, setAllowChannels] = useState<string[]>(initial.allow_channels ?? []);
  const [denyChannels, setDenyChannels] = useState<string[]>(initial.deny_channels ?? []);
  const [progressNotifications, setProgressNotifications] = useState(initial.progress_notifications ?? false);
  const [escalationMode, setEscalationMode] = useState<EscalationMode | "">(initial.escalation_mode ?? "");
  const [escalationActions, setEscalationActions] = useState<EscalationAction[]>(initial.escalation_actions ?? []);
  const [followupInterval, setFollowupInterval] = useState<number>(initial.followup_interval_minutes ?? 30);
  const [followupMaxReminders, setFollowupMaxReminders] = useState<number>(initial.followup_max_reminders ?? 0);
  const isTeamV2 = version >= 2;

  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [versionModalOpen, setVersionModalOpen] = useState(false);

  // Load known users for combobox
  useEffect(() => {
    getKnownUsers(teamId).then(setKnownUsers).catch(() => {});
  }, [teamId, getKnownUsers]);

  // Reset when team changes
  useEffect(() => {
    const s = (team.settings ?? {}) as TeamAccessSettings;
    setVersion(s.version ?? 1);
    setAllowUserIds(s.allow_user_ids ?? []);
    setDenyUserIds(s.deny_user_ids ?? []);
    setAllowChannels(s.allow_channels ?? []);
    setDenyChannels(s.deny_channels ?? []);
    setProgressNotifications(s.progress_notifications ?? false);
    setEscalationMode(s.escalation_mode ?? "");
    setEscalationActions(s.escalation_actions ?? []);
    setFollowupInterval(s.followup_interval_minutes ?? 30);
    setFollowupMaxReminders(s.followup_max_reminders ?? 0);
    setSaved(false);
    setError(null);
  }, [team]);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    setSaved(false);
    try {
      const settings: TeamAccessSettings = {};
      if (allowUserIds.length > 0) settings.allow_user_ids = allowUserIds;
      if (denyUserIds.length > 0) settings.deny_user_ids = denyUserIds;
      if (allowChannels.length > 0) settings.allow_channels = allowChannels;
      if (denyChannels.length > 0) settings.deny_channels = denyChannels;
      if (progressNotifications) settings.progress_notifications = true;
      if (escalationMode) {
        settings.escalation_mode = escalationMode;
        if (escalationActions.length > 0) settings.escalation_actions = escalationActions;
      }
      if (followupInterval !== 30) settings.followup_interval_minutes = followupInterval;
      if (followupMaxReminders !== 0) settings.followup_max_reminders = followupMaxReminders;
      if (version >= 2) settings.version = version;
      await updateTeamSettings(teamId, settings);
      setSaved(true);
      onSaved();
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : t("settings.failedSave"));
    } finally {
      setSaving(false);
    }
  }, [teamId, version, allowUserIds, denyUserIds, allowChannels, denyChannels, progressNotifications, escalationMode, escalationActions, followupInterval, followupMaxReminders, updateTeamSettings, onSaved, t]);

  const userOptions = knownUsers.map((u) => ({ value: u, label: u }));
  const channelOptions = CHANNEL_TYPES.map((c) => ({ value: c.value, label: c.label }));

  return (
    <div className="space-y-6">
      {/* Team Version */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">{t("settings.teamVersion")}</h3>
          <button
            type="button"
            onClick={() => setVersionModalOpen(true)}
            className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <Info className="h-3.5 w-3.5" />
            {t("settings.whatsNew")}
          </button>
        </div>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <button
            type="button"
            onClick={() => setVersion(1)}
            className={
              "rounded-lg border p-3 text-left transition-colors " +
              (version === 1
                ? "border-primary bg-primary/5"
                : "border-border hover:border-primary/50")
            }
          >
            <div className="text-sm font-medium">V1 — {t("settings.versionBasic")}</div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {t("settings.versionBasicDesc")}
            </div>
          </button>
          <button
            type="button"
            onClick={() => setVersion(2)}
            className={
              "rounded-lg border p-3 text-left transition-colors " +
              (version >= 2
                ? "border-primary bg-primary/5"
                : "border-border hover:border-primary/50")
            }
          >
            <div className="flex items-center gap-1.5">
              <span className="text-sm font-medium">V2 — {t("settings.versionAdvanced")}</span>
              <Badge variant="secondary" className="text-[10px] px-1.5 py-0">Beta</Badge>
            </div>
            <div className="mt-0.5 text-xs text-muted-foreground">
              {t("settings.versionAdvancedDesc")}
            </div>
          </button>
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
            <MultiSelect
              options={userOptions}
              selected={allowUserIds}
              onChange={setAllowUserIds}
              placeholder={t("settings.searchUsers")}
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.deniedUsers")}</label>
            <p className="text-xs text-muted-foreground">
              {t("settings.deniedUsersHint")}
            </p>
            <MultiSelect
              options={userOptions}
              selected={denyUserIds}
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

      {/* Notifications */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.notifications")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-blue-500/5 to-purple-500/5 p-4">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-blue-500/10 p-2.5 text-blue-600 dark:text-blue-400">
              <Bell className="h-5 w-5" />
            </div>
            <div className="flex-1 space-y-1">
              <div className="flex items-center justify-between">
                <span className="text-sm font-semibold">{t("settings.progressNotifications")}</span>
                <Switch
                  checked={progressNotifications}
                  onCheckedChange={setProgressNotifications}
                />
              </div>
              <p className="text-xs text-muted-foreground leading-relaxed">
                {t("settings.progressNotificationsHint")}
              </p>
            </div>
          </div>
        </div>
      </div>

      {/* Escalation Policy (V2 only) */}
      {isTeamV2 && <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.escalationPolicy")}</h3>
        <div className="rounded-lg border bg-gradient-to-r from-orange-500/5 to-red-500/5 p-4">
          <div className="flex items-start gap-4">
            <div className="rounded-lg bg-orange-500/10 p-2.5 text-orange-600 dark:text-orange-400">
              <ShieldAlert className="h-5 w-5" />
            </div>
            <div className="flex-1 space-y-4">
              <div className="space-y-1">
                <span className="text-sm font-semibold">{t("settings.escalationMode")}</span>
                <p className="text-xs text-muted-foreground leading-relaxed">
                  {t("settings.escalationModeHint")}
                </p>
              </div>

              <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
                {([
                  { value: "", labelKey: "escalationModeNone", descKey: "escalationModeNoneDesc" },
                  { value: "auto", labelKey: "escalationModeAuto", descKey: "escalationModeAutoDesc" },
                  { value: "review", labelKey: "escalationModeReview", descKey: "escalationModeReviewDesc" },
                  { value: "reject", labelKey: "escalationModeReject", descKey: "escalationModeRejectDesc" },
                ] as const).map((opt) => (
                  <button
                    key={opt.value}
                    type="button"
                    onClick={() => setEscalationMode(opt.value as EscalationMode | "")}
                    className={
                      "rounded-lg border p-3 text-left transition-colors " +
                      (escalationMode === opt.value
                        ? "border-primary bg-primary/5"
                        : "border-border hover:border-primary/50")
                    }
                  >
                    <div className="text-sm font-medium">{t(`settings.${opt.labelKey}`)}</div>
                    <div className="mt-0.5 text-xs text-muted-foreground">
                      {t(`settings.${opt.descKey}`)}
                    </div>
                  </button>
                ))}
              </div>

              {escalationMode && (
                <div className="space-y-1.5">
                  <label className="text-sm font-medium">{t("settings.escalationActions")}</label>
                  <p className="text-xs text-muted-foreground">
                    {t("settings.escalationActionsHint")}
                  </p>
                  <div className="flex flex-wrap gap-2">
                    {ESCALATION_ACTIONS.map((action) => (
                      <button
                        key={action}
                        type="button"
                        onClick={() => {
                          setEscalationActions((prev) =>
                            prev.includes(action)
                              ? prev.filter((a) => a !== action)
                              : [...prev, action],
                          );
                        }}
                        className={
                          "rounded-md border px-3 py-1.5 text-xs font-medium transition-colors " +
                          (escalationActions.includes(action)
                            ? "border-primary bg-primary/10 text-primary"
                            : "border-border text-muted-foreground hover:border-primary/50")
                        }
                      >
                        {action}
                      </button>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>}

      {/* Follow-up Reminders (V2 only) */}
      {isTeamV2 && <div className="space-y-4">
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
      </div>}

      {/* Save button */}
      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={saving} className="gap-2">
          {saving ? (
            t("settings.saving")
          ) : saved ? (
            <>
              <Check className="h-4 w-4" /> {t("settings.saved")}
            </>
          ) : (
            <>
              <Save className="h-4 w-4" /> {t("settings.save")}
            </>
          )}
        </Button>
        {error && <span className="text-sm text-destructive">{error}</span>}
      </div>

      <TeamVersionModal open={versionModalOpen} onOpenChange={setVersionModalOpen} />
    </div>
  );
}
