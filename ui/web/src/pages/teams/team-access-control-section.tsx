import { useTranslation } from "react-i18next";
import { MultiUserPicker } from "@/components/shared/multi-user-picker";
import { MultiSelect } from "./team-multi-select";

interface TeamAccessControlSectionProps {
  allowUserIds: string[];
  setAllowUserIds: (v: string[]) => void;
  denyUserIds: string[];
  setDenyUserIds: (v: string[]) => void;
  allowChannels: string[];
  setAllowChannels: (v: string[]) => void;
  denyChannels: string[];
  setDenyChannels: (v: string[]) => void;
  channelOptions: { value: string; label?: string }[];
}

/** User and channel allow/deny lists for team access control settings. */
export function TeamAccessControlSection({
  allowUserIds, setAllowUserIds,
  denyUserIds, setDenyUserIds,
  allowChannels, setAllowChannels,
  denyChannels, setDenyChannels,
  channelOptions,
}: TeamAccessControlSectionProps) {
  const { t } = useTranslation("teams");

  return (
    <>
      {/* User Access Control */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.userAccessControl")}</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.allowedUsers")}</label>
            <p className="text-xs text-muted-foreground">{t("settings.allowedUsersHint")}</p>
            <MultiUserPicker value={allowUserIds} onChange={setAllowUserIds} placeholder={t("settings.searchUsers")} />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.deniedUsers")}</label>
            <p className="text-xs text-muted-foreground">{t("settings.deniedUsersHint")}</p>
            <MultiUserPicker value={denyUserIds} onChange={setDenyUserIds} placeholder={t("settings.searchUsers")} />
          </div>
        </div>
      </div>

      {/* Channel Restrictions */}
      <div className="space-y-4">
        <h3 className="text-sm font-medium">{t("settings.channelRestrictions")}</h3>
        <div className="space-y-3 rounded-lg border p-4">
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.allowedChannels")}</label>
            <p className="text-xs text-muted-foreground">{t("settings.allowedChannelsHint")}</p>
            <MultiSelect options={channelOptions} selected={allowChannels} onChange={setAllowChannels} placeholder={t("settings.selectChannel")} />
          </div>
          <div className="space-y-1.5">
            <label className="text-sm font-medium">{t("settings.deniedChannels")}</label>
            <p className="text-xs text-muted-foreground">{t("settings.deniedChannelsHint")}</p>
            <MultiSelect options={channelOptions} selected={denyChannels} onChange={setDenyChannels} placeholder={t("settings.selectChannel")} />
          </div>
        </div>
      </div>
    </>
  );
}
