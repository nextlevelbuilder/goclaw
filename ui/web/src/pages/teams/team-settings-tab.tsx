import { useState, useEffect, useCallback } from "react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Combobox } from "@/components/ui/combobox";
import { X, Save, Check } from "lucide-react";
import { CHANNEL_TYPES } from "@/constants/channels";
import type { TeamData, TeamAccessSettings } from "@/types/team";
import { useTeams } from "./hooks/use-teams";

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
  const { updateTeamSettings, getKnownUsers } = useTeams();
  const [knownUsers, setKnownUsers] = useState<string[]>([]);

  // Parse initial settings
  const initial = (team.settings ?? {}) as TeamAccessSettings;
  const [allowUserIds, setAllowUserIds] = useState<string[]>(initial.allow_user_ids ?? []);
  const [denyUserIds, setDenyUserIds] = useState<string[]>(initial.deny_user_ids ?? []);
  const [allowChannels, setAllowChannels] = useState<string[]>(initial.allow_channels ?? []);
  const [denyChannels, setDenyChannels] = useState<string[]>(initial.deny_channels ?? []);
  const [progressNotifications, setProgressNotifications] = useState(initial.progress_notifications ?? false);

  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load known users for combobox
  useEffect(() => {
    getKnownUsers(teamId).then(setKnownUsers).catch(() => {});
  }, [teamId, getKnownUsers]);

  // Reset when team changes
  useEffect(() => {
    const s = (team.settings ?? {}) as TeamAccessSettings;
    setAllowUserIds(s.allow_user_ids ?? []);
    setDenyUserIds(s.deny_user_ids ?? []);
    setAllowChannels(s.allow_channels ?? []);
    setDenyChannels(s.deny_channels ?? []);
    setProgressNotifications(s.progress_notifications ?? false);
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
      await updateTeamSettings(teamId, settings);
      setSaved(true);
      onSaved();
      setTimeout(() => setSaved(false), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  }, [teamId, allowUserIds, denyUserIds, allowChannels, denyChannels, progressNotifications, updateTeamSettings, onSaved]);

  const userOptions = knownUsers.map((u) => ({ value: u, label: u }));
  const channelOptions = CHANNEL_TYPES.map((c) => ({ value: c.value, label: c.label }));

  return (
    <div className="space-y-6">
      {/* User Access Control */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">User Access Control</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Allowed Users</label>
            <p className="text-xs text-muted-foreground">
              If set, only these users can trigger team workflows. Empty = all allowed.
            </p>
            <MultiSelect
              options={userOptions}
              selected={allowUserIds}
              onChange={setAllowUserIds}
              placeholder="Search users..."
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Denied Users</label>
            <p className="text-xs text-muted-foreground">
              Always blocked, overrides allow list.
            </p>
            <MultiSelect
              options={userOptions}
              selected={denyUserIds}
              onChange={setDenyUserIds}
              placeholder="Search users..."
            />
          </div>
        </div>
      </div>

      {/* Channel Restrictions */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">Channel Restrictions</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Allowed Channels</label>
            <p className="text-xs text-muted-foreground">
              If set, only these channels can activate team workflows. Empty = all allowed.
            </p>
            <MultiSelect
              options={channelOptions}
              selected={allowChannels}
              onChange={setAllowChannels}
              placeholder="Select channel..."
            />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">Denied Channels</label>
            <p className="text-xs text-muted-foreground">
              Messages from these channels are always blocked.
            </p>
            <MultiSelect
              options={channelOptions}
              selected={denyChannels}
              onChange={setDenyChannels}
              placeholder="Select channel..."
            />
          </div>
        </div>
      </div>

      {/* Notifications */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">Notifications</h3>
        <div className="rounded-lg border p-4">
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={progressNotifications}
              onChange={(e) => setProgressNotifications(e.target.checked)}
              className="h-4 w-4 rounded border-input"
            />
            <div>
              <span className="text-sm font-medium">Progress notifications</span>
              <p className="text-xs text-muted-foreground">
                Send periodic "Your team is working on it..." messages to chat during async delegations.
              </p>
            </div>
          </label>
        </div>
      </div>

      {/* Save button */}
      <div className="flex items-center gap-3">
        <Button onClick={handleSave} disabled={saving} className="gap-2">
          {saving ? (
            "Saving..."
          ) : saved ? (
            <>
              <Check className="h-4 w-4" /> Saved
            </>
          ) : (
            <>
              <Save className="h-4 w-4" /> Save Settings
            </>
          )}
        </Button>
        {error && <span className="text-sm text-destructive">{error}</span>}
      </div>
    </div>
  );
}
